package ante

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"

	"stoc/x/stoc/keeper"
	stoctypes "stoc/x/stoc/types"
)

// IBCCustomTokenRestriction is an AnteDecorator that blocks IBC transfers
// of custom tokens created via x/stoc. Custom tokens are Cosmos-only and
// should not be sent cross-chain because:
// 1. Tax enforcement cannot be applied on other chains
// 2. Token metadata/rules are lost when crossing chains
// 3. Returning tokens via IBC would bypass tax on the outgoing path
type IBCCustomTokenRestriction struct {
	k keeper.Keeper
}

func NewIBCCustomTokenRestriction(k keeper.Keeper) IBCCustomTokenRestriction {
	return IBCCustomTokenRestriction{k: k}
}

func (d IBCCustomTokenRestriction) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	for _, msg := range tx.GetMsgs() {
		transfer, ok := msg.(*ibctransfertypes.MsgTransfer)
		if !ok {
			continue
		}

		coin := transfer.Token

		// Skip native denoms — always allowed for IBC
		if stoctypes.IsNativeDenom(coin.Denom) {
			continue
		}

		// Check if this denom is a custom stoc token
		if d.k.HasToken(ctx, coin.Denom) {
			return ctx, fmt.Errorf(
				"IBC transfer of custom token %q is not allowed: custom tokens created via x/stoc are Cosmos-only and cannot be transferred cross-chain",
				coin.Denom,
			)
		}
	}

	return next(ctx, tx, simulate)
}
