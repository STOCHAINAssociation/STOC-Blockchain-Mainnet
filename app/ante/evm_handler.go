package ante

import (
	"github.com/cosmos/evm/ante"
	evmante "github.com/cosmos/evm/ante/evm"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// newMonoEVMAnteHandler creates the sdk.AnteHandler implementation for the EVM transactions.
// v0.6.0: now takes ctx to fetch params from keepers
func newMonoEVMAnteHandler(ctx sdk.Context, options StocAnteOptions) sdk.AnteHandler {
	evmParams := options.EvmKeeper.GetParams(ctx)
	feemarketParams := options.FeeMarketKeeper.GetParams(ctx)

	maxPending := options.MaxPendingTxPerWallet
	if maxPending <= 0 {
		maxPending = DefaultMaxPendingTxPerWallet
	}

	decorators := []sdk.AnteDecorator{
		// v4-final fix: per-wallet pending-tx cap. Prevents a single sender
		// from filling the EVM mempool head and starving other users.
		NewMaxPendingTxPerWalletDecorator(options.GetEVMMempool, maxPending),
		evmante.NewEVMMonoDecorator(
			options.AccountKeeper,
			options.FeeMarketKeeper,
			options.EvmKeeper,
			options.MaxTxGasWanted,
			&evmParams,
			&feemarketParams,
		),
		ante.NewTxListenerDecorator(options.PendingTxListener),
	}

	return sdk.ChainAnteDecorators(decorators...)
}
