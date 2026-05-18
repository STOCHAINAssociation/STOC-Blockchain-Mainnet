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
	decorators := []sdk.AnteDecorator{
		// Reject EVM txs from wallets that already have >= max pending entries
		// in the EVM mempool. Placed BEFORE EVMMonoDecorator so spammers don't
		// waste cycles on fee/gas validation. CheckTx-only.
		NewMaxPendingTxPerWalletDecorator(options.GetEVMMempool, options.MaxPendingTxPerWallet),
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
