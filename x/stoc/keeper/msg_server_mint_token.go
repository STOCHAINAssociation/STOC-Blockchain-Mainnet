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
// thin msg-server boundary.
//
// SA-AUDIT-2026-06-05-fix11 (audit pass A12 SA-coverage backfill): the entire
// file previously lacked SA-* provenance even though the underlying flow
// satisfies SA-C1 (symbol-vs-minimalDenom dual-resolution) and SA-H13
// (delegation by minimalDenom, never user-supplied symbol).
func (k msgServer) MintTokens(goCtx context.Context, msg *types.MsgMintTokens) (*types.MsgMintTokensResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// SA-AUDIT-2026-06-05-fix11: bech32 creator validation — any decoding
	// failure surfaces as ErrInvalidCreatorAddress so callers can distinguish
	// "you sent garbage" from "token not found" downstream.
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, sdkerrors.Wrapf(types.ErrInvalidCreatorAddress, "invalid creator address (%s)", err)
	}

	// SA-C1 audit-2026-03-19 + SA-H13 (CRITICAL fix): resolve via FindToken
	// which accepts BOTH user-typed symbol ("MYTOK") AND minimalDenom ("MYTOK_0").
	// The original code path looked up by symbol alone, which silently dropped
	// every CreateToken'd token because they are stored under minimalDenom.
	token, findErr := k.FindToken(ctx, msg.Symbol)
	if findErr != nil {
		return nil, findErr
	}

	// SA-H13 audit-2026-03-19: always delegate using the resolved minimalDenom,
	// never msg.Symbol. minimalDenom is unique per token (sym + counter); symbol
	// can collide across tokens, so passing it raw would mint into the wrong
	// supply bucket on collision.
	err = k.Keeper.MintToken(ctx, creator, token.MinimalDenom, msg.Amount)
	if err != nil {
		return nil, err
	}

	return &types.MsgMintTokensResponse{}, nil
}
