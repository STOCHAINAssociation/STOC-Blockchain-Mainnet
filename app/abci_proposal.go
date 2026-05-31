package app

import (
	"errors"
	"runtime/debug"
	"strings"

	abci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/mempool"

	evmmempool "github.com/cosmos/evm/mempool"
)

// STOCProposalHandler wraps the default ABCI PrepareProposal handler with the
// STOChain "Bug B" cascade-skip wire-up. The upstream cosmos/evm v0.6.0 fork
// added PopCurrentAccount + CurrentIsEVM helpers to EVMMempoolIterator
// (forks/cosmos-evm-v0.6.0/mempool/iterator.go), but no caller invokes them.
// As a result, when an EVM tx fails PrepareProposalVerifyTx the default
// handler advances via iter.Next() which Shifts to the next nonce of the
// same account — those subsequent txs then also fail with nonce-too-high,
// wasting block-building iterations and risking missed block deadlines under
// mempool pressure.
//
// This handler bypasses mempool.SelectBy so it can cast the iterator to
// *evmmempool.EVMMempoolIterator and call PopCurrentAccount() to drop the
// entire sender bucket from the price-and-nonce heap. Falls back cleanly
// to default behaviour when the iterator is a Cosmos-only one.
//
// Logic mirrors cosmos-sdk v0.53.6 baseapp.DefaultProposalHandler.
// PrepareProposalHandler line-for-line so signer sequence dedup, tx
// selector limits, and invalid-tx removal stay identical.
type STOCProposalHandler struct {
	inner            *baseapp.DefaultProposalHandler
	mp               mempool.Mempool
	txVerifier       baseapp.ProposalTxVerifier
	signerExtAdapter mempool.SignerExtractionAdapter
	txSelector       baseapp.TxSelector
	logger           log.Logger
}

// NewSTOCProposalHandler builds the wrapped handler.
func NewSTOCProposalHandler(
	mp mempool.Mempool,
	txVerifier baseapp.ProposalTxVerifier,
	signerExt mempool.SignerExtractionAdapter,
	logger log.Logger,
) *STOCProposalHandler {
	inner := baseapp.NewDefaultProposalHandler(mp, txVerifier)
	inner.SetSignerExtractionAdapter(signerExt)
	return &STOCProposalHandler{
		inner:            inner,
		mp:               mp,
		txVerifier:       txVerifier,
		signerExtAdapter: signerExt,
		txSelector:       baseapp.NewDefaultTxSelector(),
		logger:           logger.With(log.ModuleKey, "stoc-proposal"),
	}
}

// PrepareProposalHandler returns a custom PrepareProposal that:
//  1. Falls back to default no-op path when mempool is nil/NoOp.
//  2. Iterates the mempool via Select() directly (NOT SelectBy) so the
//     EVMMempoolIterator interface methods PopCurrentAccount + CurrentIsEVM
//     are reachable.
//  3. When a tx fails PrepareProposalVerifyTx AND the iterator's current head
//     is an EVM tx, pops the entire sender bucket instead of shifting.
//
// All other invariants (signer sequence dedup, gas/byte cap via TxSelector,
// invalid-tx removal after iteration) match the upstream default handler.
func (h *STOCProposalHandler) PrepareProposalHandler() sdk.PrepareProposalHandler {
	return func(ctx sdk.Context, req *abci.RequestPrepareProposal) (resp *abci.ResponsePrepareProposal, err error) {
		// SA-C4 audit-2026-05-29: CometBFT does NOT wrap PrepareProposal in
		// panic-recover. A malformed signer extension, corrupt iterator, or
		// stale evmIter.PopCurrentAccount call can crash the proposer. Every
		// proposer hitting the same adversarial tx → chain liveness halt.
		// Fall back to inner default handler on panic so the block still
		// proposes (loses Bug B cascade-skip benefit for THIS block only).
		defer func() {
			if r := recover(); r != nil {
				h.logger.Error("PrepareProposal panic — falling back to default handler",
					"panic", r, "stack", string(debug.Stack()))
				resp, err = h.inner.PrepareProposalHandler()(ctx, req)
			}
		}()

		var maxBlockGas uint64
		if b := ctx.ConsensusParams().Block; b != nil {
			maxBlockGas = uint64(b.MaxGas)
		}

		defer h.txSelector.Clear()

		// No-op mempool fast path — identical to default handler.
		_, isNoOp := h.mp.(mempool.NoOpMempool)
		if h.mp == nil || isNoOp {
			for _, txBz := range req.Txs {
				tx, err := h.txVerifier.TxDecode(txBz)
				if err != nil {
					return nil, err
				}
				if h.txSelector.SelectTxForProposal(ctx, uint64(req.MaxTxBytes), maxBlockGas, tx, txBz) {
					break
				}
			}
			return &abci.ResponsePrepareProposal{Txs: h.txSelector.SelectedTxs(ctx)}, nil
		}

		selectedTxsSignersSeqs := make(map[string]uint64)
		var (
			selectedTxsNums int
			invalidTxs      []sdk.Tx
		)

		// Direct iterator — bypass SelectBy so we keep concrete type access.
		iter := h.mp.Select(ctx, req.Txs)

		// SA-H4 audit-2026-05-29: defense against potential infinite loop where
		// PopCurrentAccount no-ops (e.g. shouldUseEVM flips between caller's
		// CurrentIsEVM check and the actual Pop), causing the loop to re-fetch
		// the same memTx. Force iter.Next() if we see same Tx pointer twice
		// consecutively.
		var prevMemTx sdk.Tx
		var sameTxCount int

		for iter != nil {
			memTx := iter.Tx()
			if memTx == nil {
				break
			}

			// SA-H4: detect non-advancing iterator and force-Next to escape loop.
			if memTx == prevMemTx {
				sameTxCount++
				if sameTxCount >= 2 {
					h.logger.Warn("PrepareProposal: iter not advancing on same memTx, forcing Next()",
						"iter_stuck_count", sameTxCount)
					iter = iter.Next()
					sameTxCount = 0
					continue
				}
			} else {
				sameTxCount = 0
			}
			prevMemTx = memTx

			evmIter, _ := iter.(*evmmempool.EVMMempoolIterator)

			unorderedTx, unordOK := memTx.(sdk.TxWithUnordered)
			isUnordered := unordOK && unorderedTx.GetUnordered()
			txSignersSeqs := make(map[string]uint64)

			shouldAdd := true
			if !isUnordered {
				signerData, err := h.signerExtAdapter.GetSigners(memTx)
				if err != nil {
					return nil, err
				}
				for _, signer := range signerData {
					seq, ok := selectedTxsSignersSeqs[signer.Signer.String()]
					if !ok {
						txSignersSeqs[signer.Signer.String()] = signer.Sequence
						continue
					}
					if seq+1 != signer.Sequence {
						shouldAdd = false
						break
					}
					txSignersSeqs[signer.Signer.String()] = signer.Sequence
				}
			}
			if !shouldAdd {
				iter = iter.Next()
				continue
			}

			txBz, err := h.txVerifier.PrepareProposalVerifyTx(memTx)
			if err != nil {
				invalidTxs = append(invalidTxs, memTx)

				// SA-H3 audit-2026-05-29: only pop sender bucket on PERMANENT
				// nonce errors. Transient errors (insufficient funds mid-block,
				// IBC redundant relay, fee market fluctuation) should fall through
				// to iter.Next() — popping wide on transient = censoring legitimate
				// downstream tx of the same sender.
				errMsg := err.Error()
				isPermanentNonceErr :=
					strings.Contains(errMsg, "nonce too high") ||
						strings.Contains(errMsg, "nonce too low") ||
						strings.Contains(errMsg, "intrinsic gas") ||
						strings.Contains(errMsg, "invalid nonce")

				// Bug B wire: if current head is an EVM tx AND the err is a
				// permanent nonce-class error, pop the entire sender bucket so we
				// don't cascade through its (now nonce-too-high) subsequent txs.
				// PopCurrentAccount already advances the underlying heap, so do
				// NOT also call iter.Next() — the next loop iteration's iter.Tx()
				// will surface the new head.
				if evmIter != nil && evmIter.CurrentIsEVM() && isPermanentNonceErr {
					h.logger.Debug(
						"Bug B cascade-skip: dropping sender bucket after permanent ante fail",
						"err", errMsg,
					)
					evmIter.PopCurrentAccount()
					continue
				}

				iter = iter.Next()
				continue
			}

			if stop := h.txSelector.SelectTxForProposal(ctx, uint64(req.MaxTxBytes), maxBlockGas, memTx, txBz); stop {
				break
			}

			txsLen := len(h.txSelector.SelectedTxs(ctx))
			if !isUnordered {
				for sender, seq := range txSignersSeqs {
					if txsLen != selectedTxsNums {
						selectedTxsSignersSeqs[sender] = seq
					} else if _, ok := selectedTxsSignersSeqs[sender]; !ok {
						selectedTxsSignersSeqs[sender] = seq - 1
					}
				}
			}
			selectedTxsNums = txsLen

			iter = iter.Next()
		}

		for _, tx := range invalidTxs {
			if err := h.mp.Remove(tx); err != nil && !errors.Is(err, mempool.ErrTxNotFound) {
				return nil, err
			}
		}

		return &abci.ResponsePrepareProposal{Txs: h.txSelector.SelectedTxs(ctx)}, nil
	}
}

// ProcessProposalHandler delegates to the upstream default. Bug B is a
// PrepareProposal-time optimization; ProcessProposal validates an already
// built block byte-by-byte and has no iterator to cascade through.
func (h *STOCProposalHandler) ProcessProposalHandler() sdk.ProcessProposalHandler {
	return h.inner.ProcessProposalHandler()
}
