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

	// Call MintToken function to perform mint

	err = k.Keeper.MintToken(ctx, creator, msg.Symbol, msg.Amount)
	if err != nil {
		return nil, err
	}

	return &types.MsgMintTokensResponse{}, nil
}
