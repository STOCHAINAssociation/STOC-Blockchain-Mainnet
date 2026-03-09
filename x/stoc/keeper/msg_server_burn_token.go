package keeper

import (
	"context"

	"stoc/x/stoc/types"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
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

	// Pre-validate: ensure post-burn state will pass SetToken validation BEFORE any bank mutations.
	// This follows the same CEI pattern as MintToken — prevents wasted gas on invalid burns.
	preValidateToken := token
	preValidateToken.TotalSupply = token.TotalSupply.Sub(amountToBurn)
	if preValidateToken.RemainingSupply.GT(preValidateToken.TotalSupply) {
		preValidateToken.RemainingSupply = preValidateToken.TotalSupply
	}
	if err := types.ValidateState(preValidateToken); err != nil {
		return nil, sdkerrors.Wrap(err, "burn would produce invalid token state")
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

	// If TotalSupply dropped below RemainingSupply, burn excess from module account
	// to maintain invariant: moduleBalance == token.RemainingSupply.
	// Use actual module balance to determine burnable excess (defensive against stale RemainingSupply).
	if token.RemainingSupply.GT(token.TotalSupply) {
		excess := token.RemainingSupply.Sub(token.TotalSupply)

		// Check actual module balance to avoid BurnCoins failure on stale state
		moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
		moduleBalance := k.bankKeeper.GetBalance(ctx, moduleAddr, msg.Denom)
		actualBurnable := math.MinInt(excess, moduleBalance.Amount)

		if actualBurnable.IsPositive() {
			ctx.Logger().Warn("Burning excess module tokens after user burn",
				"denom", token.MinimalDenom,
				"excess", excess.String(),
				"actual_burnable", actualBurnable.String(),
				"remaining_before", token.RemainingSupply.String(),
				"total_after", token.TotalSupply.String(),
			)
			excessCoins := sdk.NewCoins(sdk.NewCoin(msg.Denom, actualBurnable))
			if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, excessCoins); err != nil {
				return nil, sdkerrors.Wrap(err, "failed to burn excess module tokens")
			}
		}
		// Cap RemainingSupply at TotalSupply using tracked value only.
		// DO NOT derive from moduleBalance — direct transfers to module account would inflate RemainingSupply.
		token.RemainingSupply = token.TotalSupply
	}

	if err := k.SetToken(ctx, token); err != nil {
		return nil, err
	}

	// Emit event with post-burn state for supply tracking observability
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeBurnToken,
			sdk.NewAttribute(types.AttributeKeyBurner, msg.Creator),
			sdk.NewAttribute(types.AttributeKeyMinimalDenom, msg.Denom),
			sdk.NewAttribute(types.AttributeKeyBurnAmount, amountToBurn.String()),
			sdk.NewAttribute("total_supply_after", token.TotalSupply.String()),
			sdk.NewAttribute(types.AttributeKeyRemainingSupply, token.RemainingSupply.String()),
		),
	)

	return &types.MsgBurnTokenResponse{
		Success: true,
		Message: "Tokens burned successfully",
	}, nil
}
