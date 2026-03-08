package keeper

import (
	"context"

	"stoc/x/stoc/types"

	sdkerrors "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BurnToken allows ANY token holder to burn their own tokens (similar to ERC20 burn).
// This is BY DESIGN — not restricted to token creator. TotalSupply is updated accordingly.
func (k msgServer) BurnToken(goCtx context.Context, msg *types.MsgBurnToken) (*types.MsgBurnTokenResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "invalid creator address")
	}

	// Only allow burning stoc-managed tokens (not native ustoc/astoc)
	token, found := k.GetToken(ctx, msg.Denom)
	if !found {
		return nil, sdkerrors.Wrapf(types.ErrTokenNotFound, "can only burn stoc-managed tokens, denom %s not found", msg.Denom)
	}

	// Determine amount to burn
	amountToBurn := msg.Amount
	if msg.BurnAll {
		// Get current balance after gas deduction
		// Note: Cosmos SDK deducts gas BEFORE handler execution,
		// so GetBalance() returns balance AFTER gas has been paid
		balance := k.bankKeeper.GetBalance(ctx, creator, msg.Denom)
		amountToBurn = balance.Amount
	}

	// Validate amount
	if amountToBurn.IsZero() {
		if msg.BurnAll {
			return nil, sdkerrors.Wrap(types.ErrInvalidAmount, "no tokens remaining after gas deduction")
		}
		return nil, sdkerrors.Wrap(types.ErrInvalidAmount, "amount to burn must be positive")
	}

	// Validate supply tracking BEFORE any bank mutations (fail-fast on corrupted state)
	if token.TotalSupply.LT(amountToBurn) {
		return nil, sdkerrors.Wrapf(types.ErrInvalidAmount, "burn amount %s exceeds tracked total supply %s — state may be corrupted", amountToBurn.String(), token.TotalSupply.String())
	}

	// Transfer coins from user to module
	coins := sdk.NewCoins(sdk.NewCoin(msg.Denom, amountToBurn))
	err = k.bankKeeper.SendCoinsFromAccountToModule(ctx, creator, types.ModuleName, coins)
	if err != nil {
		return nil, err
	}

	// Burn coins from module
	err = k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins)
	if err != nil {
		return nil, err
	}

	// Update token supply tracking
	token.TotalSupply = token.TotalSupply.Sub(amountToBurn)

	// Cap RemainingSupply if TotalSupply dropped below it (burned tokens reduce total
	// but module-held remaining tokens can't exceed total — maintain invariant)
	if token.RemainingSupply.GT(token.TotalSupply) {
		ctx.Logger().Error("RemainingSupply exceeds TotalSupply after burn — possible state corruption",
			"denom", token.MinimalDenom,
			"remaining", token.RemainingSupply.String(),
			"total", token.TotalSupply.String(),
		)
		token.RemainingSupply = token.TotalSupply
	}

	k.SetToken(ctx, token)

	// Emit event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeBurnToken,
			sdk.NewAttribute(types.AttributeKeyTokenCreator, msg.Creator),
			sdk.NewAttribute(types.AttributeKeyMinimalDenom, msg.Denom),
			sdk.NewAttribute(types.AttributeKeyBurnAmount, amountToBurn.String()),
		),
	)

	return &types.MsgBurnTokenResponse{
		Success: true,
		Message: "Tokens burned successfully",
	}, nil
}
