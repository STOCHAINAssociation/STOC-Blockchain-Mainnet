package keeper

import (
	"context"

	"stoc/x/stoc/types"

	sdkerrors "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MintTokens delegates to Keeper.MintToken after creator + token resolution.
// Authorization (creator-only), unlimited-flag check, totalSupply cap, and
// RemainingSupply update all live in Keeper.MintToken — this handler is the
// thin msg-server boundary. The token is resolved by symbol or minimalDenom,
// and minting is always delegated by minimalDenom, never the user-supplied
// symbol.
func (k msgServer) MintTokens(goCtx context.Context, msg *types.MsgMintTokens) (*types.MsgMintTokensResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Bech32 creator validation — any decoding
	// failure surfaces as ErrInvalidCreatorAddress so callers can distinguish
	// "you sent garbage" from "token not found" downstream.
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, sdkerrors.Wrapf(types.ErrInvalidCreatorAddress, "invalid creator address (%s)", err)
	}

	// Resolve via FindToken which accepts BOTH user-typed symbol ("MYTOK")
	// AND minimalDenom ("MYTOK_0"). Looking up by symbol alone would miss
	// every CreateToken'd token because they are stored under minimalDenom.
	token, findErr := k.FindToken(ctx, msg.Symbol)
	if findErr != nil {
		return nil, findErr
	}

	// Always delegate using the resolved minimalDenom,
	// never msg.Symbol. minimalDenom is unique per token (sym + counter); symbol
	// can collide across tokens, so passing it raw would mint into the wrong
	// supply bucket on collision.
	err = k.Keeper.MintToken(ctx, creator, token.MinimalDenom, msg.Amount)
	if err != nil {
		return nil, err
	}

	return &types.MsgMintTokensResponse{}, nil
}
