package app

import (
	"errors"
	"runtime/debug"

	abci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/mempool"

	evmmempool "github.com/cosmos/evm/mempool"
)

// STOCProposalHandler wraps the default ABCI PrepareProposal handler with the
// EVM sender-bucket cascade-skip wire-up. The upstream cosmos/evm v0.6.0 fork
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
		// CometBFT does NOT wrap PrepareProposal in panic-recover. A malformed
		// signer extension, corrupt iterator, or stale evmIter.PopCurrentAccount
		// call can crash the proposer. Every proposer hitting the same adversarial
		// tx → chain liveness halt. Fall back to inner default handler on panic
		// so the block still proposes (loses the cascade-skip benefit for THIS
		// block only).
		// The recover path must also evict the poison tx that caused the panic;
		// otherwise EVERY subsequent block re-panics on the same tx → empty-block
		// DoS forever. We track `currentMemTx` as the iterator advances so the
		// panic-recovery path can identify and remove the poison tx from the
		// mempool, allowing future blocks to make progress.
		var currentMemTx sdk.Tx
		defer func() {
			if r := recover(); r != nil {
				h.logger.Error("PrepareProposal panic — attempting poison-tx eviction + fallback",
					"panic", r, "stack", string(debug.Stack()))
				// Best-effort evict the in-flight tx that triggered the panic.
				// Ignore errors (Remove may report ErrTxNotFound if the tx was
				// already invalidated elsewhere). This breaks the empty-block
				// DoS loop by ensuring the next PrepareProposal cycle sees a
				// clean mempool head.
				if currentMemTx != nil && h.mp != nil {
					if remErr := h.mp.Remove(currentMemTx); remErr != nil {
						// Panic-path eviction failures are operationally
						// significant — they mean the empty-block DoS guard
						// could not confirm removal of the poison tx. Warn so
						// production log levels surface it (Debug is filtered
						// out). ErrTxNotFound included is benign but rare
						// enough to be worth a signal.
						h.logger.Warn("PrepareProposal panic: poison-tx evict failed",
							"err", remErr)
					}
				}
				// Inner default handler also iterates the same mempool. After
				// eviction the poison is gone; if a SECOND independent panic
				// still fires (unrelated tx), the nested recover returns an
				// empty block so chain liveness is preserved.
				func() {
					defer func() {
						if r2 := recover(); r2 != nil {
							h.logger.Error("PrepareProposal inner-fallback panic — returning empty block",
								"panic", r2, "stack", string(debug.Stack()))
							resp = &abci.ResponsePrepareProposal{Txs: nil}
							err = nil
						}
					}()
					resp, err = h.inner.PrepareProposalHandler()(ctx, req)
				}()
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

		// Bound proposer memory under adversarial mempool spam. Thousands of
		// txs all failing ante can push transient heap >50MB per block (invalid
		// sdk.Tx ptr retains the full decoded tree, ~10KB each). Cap invalidTxs
		// at 1024 — excess remain in mempool one extra block and evict on next
		// cycle. Iteration cap (8× invalidTxs cap) is defense-in-depth against
		// a pathological mempool failing to terminate.
		const maxInvalidTxsPerBlock = 1024
		const maxIterations = 8 * maxInvalidTxsPerBlock
		iterCount := 0

		// Direct iterator — bypass SelectBy so we keep concrete type access.
		iter := h.mp.Select(ctx, req.Txs)

		// Defense against potential infinite loop where PopCurrentAccount
		// no-ops (e.g. shouldUseEVM flips between caller's CurrentIsEVM check and
		// the actual Pop), causing the loop to re-fetch the same memTx. Force
		// iter.Next() if we see same Tx pointer twice consecutively.
		var prevMemTx sdk.Tx
		var sameTxCount int

		for iter != nil {
			memTx := iter.Tx()
			if memTx == nil {
				break
			}
			// Hard cap iteration count. Truncate proposal building rather than
			// risk an unterminated loop on a misbehaving mempool.
			iterCount++
			if iterCount > maxIterations {
				h.logger.Warn("PrepareProposal: maxIterations cap reached — truncating",
					"iter_count", iterCount, "invalid_so_far", len(invalidTxs))
				break
			}
			// Detect non-advancing iterator and force-Next to escape loop.
			if memTx == prevMemTx {
				sameTxCount++
				if sameTxCount >= 2 {
					h.logger.Warn("PrepareProposal: iter not advancing on same memTx, forcing Next()",
						"iter_stuck_count", sameTxCount)
					// Clear the eviction target before the forced Next(). A
					// panic here originates in iterator/mempool internal
					// state, not in a specific tx — the tx currently tracked
					// was already processed (possibly selected into this
					// proposal), so evicting it would remove an innocent tx
					// while leaving the real poison in place, defeating the
					// empty-block DoS guard. With nil the recover path skips
					// eviction and falls back to the inner handler.
					currentMemTx = nil
					iter = iter.Next()
					sameTxCount = 0
					continue
				}
			} else {
				sameTxCount = 0
			}
			prevMemTx = memTx

			// Track current head so panic-recover can evict the poison tx
			// from mempool. The assignment MUST stay AFTER the force-Next
			// branch above — assigning before it would make the
			// panic-recovery evict the wrong tx on a forced-Next panic (see
			// comment in that branch).
			currentMemTx = memTx

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
				// Cap the invalidTxs slice. Beyond the cap the iterator still
				// advances so block-building finishes, but the slice stops
				// growing — remaining invalid txs persist one extra block and get
				// cleaned on the next PrepareProposal cycle.
				// This cap bounds MEMORY only. It must NEVER gate the cascade-skip
				// below — PopCurrentAccount advances the iterator past a poisoned
				// sender bucket regardless of whether the tx was recorded in
				// invalidTxs. Gating Pop on the cap would let an attacker fill the
				// cap with 1024 cheap invalid txs and then park
				// permanent-nonce-error EVM txs that never get skipped.
				if len(invalidTxs) < maxInvalidTxsPerBlock {
					invalidTxs = append(invalidTxs, memTx)
				}

				// Only pop the sender bucket on PERMANENT nonce errors. Transient
				// errors (insufficient funds mid-block, IBC redundant relay, fee
				// market fluctuation) should fall through to iter.Next() — popping
				// wide on transient = censoring legitimate downstream tx of the
				// same sender.
				// The cosmos-evm v0.6.0 fork emits "tx nonce is higher/lower than
				// account nonce" (NOT the go-ethereum "nonce too high/low"
				// wording). Without these fork strings the most common nonce-gap
				// path would never trigger PopCurrentAccount and the cascade-skip
				// would be silently disabled. Match BOTH legacy (go-ethereum core)
				// AND fork phrasings.
				errMsg := err.Error()
				isPermanentNonceErr := IsPermanentNonceErr(errMsg)

				// Cascade-skip: if current head is an EVM tx AND the err is a
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

		// Per-block summary so operators can spot adversarial mempool
		// conditions (high invalid_ratio = spam, high iter_count = pressure
		// on the iteration cap). Promote to Info when invalid > selected
		// (canonical "spam in progress" signal); Debug otherwise.
		// Materialize SelectedTxs ONCE and reuse for both the summary log and
		// the response below — each call allocates a fresh copy of the
		// selected-tx slice.
		selectedTxs := h.txSelector.SelectedTxs(ctx)
		selectedCount := len(selectedTxs)
		invalidCount := len(invalidTxs)
		if invalidCount > 0 && invalidCount > selectedCount {
			h.logger.Info("PrepareProposal: high invalid-tx ratio",
				"selected", selectedCount, "invalid", invalidCount, "iter_count", iterCount)
		} else {
			h.logger.Debug("PrepareProposal: block built",
				"selected", selectedCount, "invalid", invalidCount, "iter_count", iterCount)
		}

		// Best-effort cleanup. Aborting the proposal on any Remove error
		// other than ErrTxNotFound would let a transient mempool error
		// (iterator lock contention, codec drift on one tx) force fallback via
		// the outer recover, replacing a valid built block with an empty one.
		// Log warn + continue so liveness is preserved; un-removed tx retries
		// cleanup on the next PrepareProposal cycle. Aligned with the relaxed
		// semantics of the panic-recover branch.
		for _, tx := range invalidTxs {
			if err := h.mp.Remove(tx); err != nil && !errors.Is(err, mempool.ErrTxNotFound) {
				h.logger.Warn("PrepareProposal: failed to evict invalid tx, will retry next block",
					"err", err)
			}
		}

		return &abci.ResponsePrepareProposal{Txs: selectedTxs}, nil
	}
}

// ProcessProposalHandler delegates to the upstream default. The cascade-skip is a
// PrepareProposal-time optimization; ProcessProposal validates an already
// built block byte-by-byte and has no iterator to cascade through.
func (h *STOCProposalHandler) ProcessProposalHandler() sdk.ProcessProposalHandler {
	return h.inner.ProcessProposalHandler()
}
