package keeper

import (
	"context"

	"stoc/x/stoc/types"

	sdkerrors "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) ReleaseTokens(goCtx context.Context, msg *types.MsgReleaseTokens) (*types.MsgReleaseTokensResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Get token info
	token, found := k.GetToken(ctx, msg.Symbol)
	if !found {
		return nil, sdkerrors.Wrapf(types.ErrTokenNotFound, "token %s does not exist", msg.Symbol)
	}

	// Check if caller is creator
	if msg.Creator != token.Creator {
		return nil, sdkerrors.Wrap(types.ErrUnauthorized, "only token creator can release tokens")
	}

	// Check if requested amount exceeds remaining supply
	if msg.Amount.GT(token.RemainingSupply) {
		return nil, sdkerrors.Wrapf(types.ErrInsufficientTokens,
			"requested amount %s exceeds remaining supply %s",
			msg.Amount, token.RemainingSupply)
	}

	// Transfer tokens from module account to recipient
	recipient, err := sdk.AccAddressFromBech32(msg.Recipient)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "invalid recipient address")
	}

	coin := sdk.NewCoin(token.MinimalDenom, msg.Amount)
	if err := k.BankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, sdk.NewCoins(coin)); err != nil {
		return nil, err
	}

	// Update remaining supply
	token.RemainingSupply = token.RemainingSupply.Sub(msg.Amount)
	k.SetToken(ctx, token)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeReleaseTokens,
			sdk.NewAttribute(types.AttributeKeyTokenSymbol, token.Symbol),
			sdk.NewAttribute(types.AttributeKeyAmount, msg.Amount.String()),
			sdk.NewAttribute(types.AttributeKeyRecipient, msg.Recipient),
			sdk.NewAttribute(types.AttributeKeyRemainingSupply, token.RemainingSupply.String()),
		),
	)

	return &types.MsgReleaseTokensResponse{}, nil
}
