package keeper

import (
	"context"

	"stoc/x/stoc/types"

	sdkerrors "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) ReleaseTokens(goCtx context.Context, msg *types.MsgReleaseTokens) (*types.MsgReleaseTokensResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Get token info — supports both minimalDenom ("SYMBOL_0") and symbol ("SYMBOL")
	token, err := k.FindToken(ctx, msg.Symbol)
	if err != nil {
		return nil, err
	}

	// Check if caller is creator
	if msg.Creator != token.Creator {
		return nil, sdkerrors.Wrap(types.ErrUnauthorized, "only token creator can release tokens")
	}

	// Defensive check: amount must be positive
	if !msg.Amount.IsPositive() {
		return nil, sdkerrors.Wrap(types.ErrInvalidAmount, "release amount must be positive")
	}

	// Check if requested amount exceeds remaining supply
	if msg.Amount.GT(token.RemainingSupply) {
		return nil, sdkerrors.Wrapf(types.ErrInsufficientTokens,
			"requested amount %s exceeds remaining supply %s",
			msg.Amount, token.RemainingSupply)
	}

	// Pre-validate: ensure post-release state will pass SetToken validation BEFORE bank mutations.
	preValidateToken := token
	preValidateToken.RemainingSupply = token.RemainingSupply.Sub(msg.Amount)
	if err := types.ValidateState(preValidateToken); err != nil {
		return nil, sdkerrors.Wrap(err, "release would produce invalid token state")
	}

	// Transfer tokens from module account to recipient
	recipient, err := sdk.AccAddressFromBech32(msg.Recipient)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "invalid recipient address")
	}

	coin := sdk.NewCoin(token.MinimalDenom, msg.Amount)
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, sdk.NewCoins(coin)); err != nil {
		return nil, err
	}

	// Update remaining supply
	token.RemainingSupply = token.RemainingSupply.Sub(msg.Amount)
	if err := k.SetToken(ctx, token); err != nil {
		return nil, err
	}

	ctx.Logger().Info("Tokens released",
		"symbol", token.Symbol,
		"minimal_denom", token.MinimalDenom,
		"amount", msg.Amount.String(),
		"recipient", msg.Recipient,
		"remaining_supply", token.RemainingSupply.String(),
	)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeReleaseTokens,
			sdk.NewAttribute(types.AttributeKeyTokenSymbol, token.Symbol),
			sdk.NewAttribute(types.AttributeKeyMinimalDenom, token.MinimalDenom),
			sdk.NewAttribute(types.AttributeKeyAmount, msg.Amount.String()),
			sdk.NewAttribute(types.AttributeKeyRecipient, msg.Recipient),
			sdk.NewAttribute(types.AttributeKeyRemainingSupply, token.RemainingSupply.String()),
			sdk.NewAttribute(types.AttributeKeyTokenCreator, token.Creator),
		),
	)

	return &types.MsgReleaseTokensResponse{}, nil
}
