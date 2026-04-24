package keeper

import (
	"context"

	"stoc/x/stoc/types"

	sdkerrors "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) MintTokens(goCtx context.Context, msg *types.MsgMintTokens) (*types.MsgMintTokensResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Get creator address
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, sdkerrors.Wrapf(types.ErrInvalidCreatorAddress, "invalid creator address (%s)", err)
	}

	// Resolve token — supports both minimalDenom ("SYMBOL_0") and symbol ("SYMBOL")
	token, findErr := k.FindToken(ctx, msg.Symbol)
	if findErr != nil {
		return nil, findErr
	}

	err = k.Keeper.MintToken(ctx, creator, token.MinimalDenom, msg.Amount)
	if err != nil {
		return nil, err
	}

	return &types.MsgMintTokensResponse{}, nil
}
