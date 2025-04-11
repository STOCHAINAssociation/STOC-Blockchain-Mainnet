package keeper

import (
	"context"

	"stoc/x/stoc/types"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) MintTokens(goCtx context.Context, msg *types.MsgMintTokens) (*types.MsgMintTokensResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Get creator address
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, sdkerrors.Wrapf(types.ErrInvalidCreatorAddress, "invalid creator address (%s)", err)
	}

	// Convert string amount to int
	if !msg.Amount.IsInt64() {
		return nil, sdkerrors.Wrap(types.ErrInvalidAmount, "amount too large")
	}
	amount := msg.Amount.Int64()

	// Call MintToken function to perform mint
	err = types.ValidateMintToken(msg.Symbol, math.NewInt(amount))
	if err != nil {
		return nil, err
	}

	err = k.Keeper.MintToken(ctx, creator, msg.Symbol, math.NewInt(amount))
	if err != nil {
		return nil, err
	}

	return &types.MsgMintTokensResponse{}, nil
}
