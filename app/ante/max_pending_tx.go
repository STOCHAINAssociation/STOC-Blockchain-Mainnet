package ante

import (
	"fmt"
	"math/big"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	evmmempool "github.com/cosmos/evm/mempool"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// DefaultMaxPendingTxPerWallet is the cap on simultaneously pending EVM transactions
// per sender address in the ExperimentalEVMMempool. Beyond this, the wallet is
// considered to be self-DoSing the mempool: a single sender filling the pool head
// would starve all other users' transactions from inclusion.
//
// The cap is conservative: legitimate users rarely have more than ten pending
// transactions concurrently. Bots and stuck-nonce situations are the primary
// producers of large per-address pending counts.
const DefaultMaxPendingTxPerWallet = 50

// MempoolGetter is a deferred getter for the EVM mempool. The mempool is
// constructed AFTER the ante handler in app.go (chicken-and-egg), so the
// decorator must look it up lazily at AnteHandle time.
type MempoolGetter func() *evmmempool.ExperimentalEVMMempool

// MaxPendingTxPerWalletDecorator rejects EVM transactions when the sender has
// already accumulated `max` pending entries in the EVM txpool. This is the
// "rate limit ante" / Option D in step-13: stop a single wallet from filling
// the experimental EVM mempool head and starving other users.
//
// Behaviour:
//   - Cosmos SDK txs (non-EVM): pass through, decorator does not apply.
//   - EVM txs (MsgEthereumTx): count `pending + queued` for the sender via
//     `txPool.ContentFrom(addr)`. Reject if count >= max.
//
// Important: this is enforced at CheckTx time. It does NOT prevent a re-org
// or post-mining accumulation, but it does stop the broadcast-spam flood that
// took down mainnet EVM tx layer.
type MaxPendingTxPerWalletDecorator struct {
	getMempool MempoolGetter
	max        int
}

// NewMaxPendingTxPerWalletDecorator builds the decorator. `max <= 0` falls back
// to DefaultMaxPendingTxPerWallet so misconfiguration cannot accidentally
// disable the protection.
func NewMaxPendingTxPerWalletDecorator(getMempool MempoolGetter, max int) MaxPendingTxPerWalletDecorator {
	if max <= 0 {
		max = DefaultMaxPendingTxPerWallet
	}
	return MaxPendingTxPerWalletDecorator{
		getMempool: getMempool,
		max:        max,
	}
}

// AnteHandle implements sdk.AnteDecorator.
func (d MaxPendingTxPerWalletDecorator) AnteHandle(
	ctx sdk.Context,
	tx sdk.Tx,
	simulate bool,
	next sdk.AnteHandler,
) (sdk.Context, error) {
	// SA-H5 audit-2026-05-29: enforce on both CheckTx AND RecheckTx (was: only
	// CheckTx). Previous gate allowed sender to maintain 50 pending forever via
	// Recheck bypass — Recheck fires after every block, never re-counted toward
	// cap. Now Recheck also enforces.
	if simulate || !(ctx.IsCheckTx() || ctx.IsReCheckTx()) {
		return next(ctx, tx, simulate)
	}

	// Single-message EVM txs only. Multi-msg or non-EVM bypass cleanly.
	msgs := tx.GetMsgs()
	if len(msgs) != 1 {
		return next(ctx, tx, simulate)
	}
	ethMsg, ok := msgs[0].(*evmtypes.MsgEthereumTx)
	if !ok {
		return next(ctx, tx, simulate)
	}

	// SA-AUDIT-2026-06-06 HIGH-1 (deep re-audit H1, audit batch B): bind the
	// per-wallet quota to the SIGNATURE-VERIFIED sender, not the unverified
	// MsgEthereumTx.From wire field.
	//
	// MsgEthereumTx.GetSender() returns common.BytesToAddress(msg.From) — a
	// protobuf field that ValidateBasic only checks for length != 0
	// (cosmos-evm v0.6.0 x/vm/types/msg.go:90). The signature check that
	// binds From to the recovered signer runs LATER in the ante chain
	// (EVMMonoDecorator step 5, mono_decorator.go:166). Without this fix,
	// an attacker could rotate a fake From per transaction so each tx's
	// quota slot was counted against a fresh, unused address, defeating
	// the per-wallet rate limit and forcing validators to spend CPU on
	// ValidateTx / SetupContext / NewMonoDecoratorUtils /
	// txpool.ValidateTransaction / fee math before the eventual
	// VerifySender rejection. The whole point of the rate limiter (SA-Báu
	// audit-2026-05-25 mempool DoS finding) is to STOP CPU spend before
	// the mono preamble, so the quota must be bound to a verified address.
	//
	// Inline the MakeSigner + VerifySender pattern used by
	// EthSigVerificationDecorator (cosmos-evm v0.6.0
	// ante/evm/05_signature_verification.go). A signed-but-tampered tx
	// (mismatched From) gets rejected here at decorator step 0, before any
	// mempool quota work, and the quota count operates on the recovered
	// sender bytes. The mono decorator still runs its own VerifySender at
	// step 5 — the ~50 µs of duplicated ecrecover is the cost of making
	// the rate limit actually enforceable.
	ethCfg := evmtypes.GetEthChainConfig()
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix())) //#nosec G115 -- block time fits uint64
	if err := ethMsg.VerifySender(signer); err != nil {
		return ctx, errorsmod.Wrapf(
			errortypes.ErrorInvalidSigner,
			"EVM signature verification failed at quota decorator: %s",
			err.Error(),
		)
	}

	if d.getMempool == nil {
		// Defensive: getter not wired (e.g., tests). Skip rather than panic.
		return next(ctx, tx, simulate)
	}
	pool := d.getMempool()
	if pool == nil {
		// EVMMempool not initialized yet (e.g., very early CheckTx during startup).
		return next(ctx, tx, simulate)
	}
	txPool := pool.GetTxPool()
	if txPool == nil {
		// Legacy pool not yet wired (startup race). Skip cap rather than panic.
		return next(ctx, tx, simulate)
	}

	sender := ethMsg.GetSender()
	pending, queued := txPool.ContentFrom(sender)
	count := len(pending) + len(queued)
	if count >= d.max {
		return ctx, errorsmod.Wrap(
			errortypes.ErrMempoolIsFull,
			fmt.Sprintf(
				"wallet %s already has %d pending EVM transactions (max %d). "+
					"Wait for existing txs to confirm or cancel them before submitting more.",
				sender.Hex(), count, d.max,
			),
		)
	}
	return next(ctx, tx, simulate)
}
