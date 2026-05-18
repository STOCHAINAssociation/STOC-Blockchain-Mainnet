package ante

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	evmmempool "github.com/cosmos/evm/mempool"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// DefaultMaxPendingTxPerWallet is the cap on simultaneously pending EVM transactions
// per sender address in the ExperimentalEVMMempool. Beyond this, the wallet is
// considered to be self-DoSing the mempool (e.g., the Lê Minh 102-tx incident on
// mainnet 2026-05-12 where one wallet filled the pool head and starved all other
// users).
//
// The cap is conservative: legitimate users rarely have >10 pending txs. Bots /
// stuck nonces are the main producers of large per-address pending counts.
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
	// Only enforce during CheckTx (mempool admission). DeliverTx / ReCheck pass
	// through — by that point the tx is already past mempool admission and
	// re-counting would waste cycles. Simulation also skips.
	if simulate || !ctx.IsCheckTx() || ctx.IsReCheckTx() {
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
