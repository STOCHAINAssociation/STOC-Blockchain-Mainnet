package ante

import (
	errorsmod "cosmossdk.io/errors"
	"github.com/cosmos/evm/ante"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"

	stockeeper "stoc/x/stoc/keeper"
)

// StocAnteOptions wraps EVM HandlerOptions with stoc-specific keepers
type StocAnteOptions struct {
	ante.HandlerOptions
	StocKeeper stockeeper.Keeper
}

// NewAnteHandler returns an ante handler responsible for attempting to route an
// Ethereum or SDK transaction to an internal ante handler for performing
// transaction-level processing (e.g. fee payment, signature verification) before
// being passed onto its respective handler.
func NewAnteHandler(options StocAnteOptions) sdk.AnteHandler {
	return func(
		ctx sdk.Context, tx sdk.Tx, sim bool,
	) (newCtx sdk.Context, err error) {
		var anteHandler sdk.AnteHandler

		txWithExtensions, ok := tx.(authante.HasExtensionOptionsTx)
		if ok {
			opts := txWithExtensions.GetExtensionOptions()
			if len(opts) > 1 {
				return ctx, errorsmod.Wrap(
					errortypes.ErrUnknownExtensionOptions,
					"transactions with multiple extension options are not supported",
				)
			}
			if len(opts) > 0 {
				switch typeURL := opts[0].GetTypeUrl(); typeURL {
				case "/cosmos.evm.vm.v1.ExtensionOptionsEthereumTx":
					// handle as *evmtypes.MsgEthereumTx
					anteHandler = newMonoEVMAnteHandler(ctx, options)
				case "/cosmos.evm.ante.v1.ExtensionOptionDynamicFeeTx":
					// cosmos-sdk tx with dynamic fee extension — v0.6.0: path changed from types.v1 to ante.v1
					anteHandler = newCosmosAnteHandler(ctx, options)
				default:
					return ctx, errorsmod.Wrapf(
						errortypes.ErrUnknownExtensionOptions,
						"rejecting tx with unsupported extension option: %s", typeURL,
					)
				}

				return anteHandler(ctx, tx, sim)
			}
		}

		// handle as totally normal Cosmos SDK tx (no EVM extension options)
		anteHandler = newCosmosAnteHandler(ctx, options)
		return anteHandler(ctx, tx, sim)
	}
}
