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

	// Check if this is a stoc-managed token (optional — native denom burns are allowed)
	token, isManaged := k.GetToken(ctx, msg.Denom)

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

	// Supply validation only for stoc-managed tokens
	if isManaged {
		// Validate supply tracking BEFORE any bank mutations (fail-fast on corrupted state)
		if token.TotalSupply.LT(amountToBurn) {
			return nil, sdkerrors.Wrapf(types.ErrInvalidAmount, "burn amount %s exceeds tracked total supply %s — state may be corrupted", amountToBurn.String(), token.TotalSupply.String())
		}

		// Pre-validate: ensure post-burn state will pass SetToken validation BEFORE any bank mutations.
		preValidateToken := token
		preValidateToken.TotalSupply = token.TotalSupply.Sub(amountToBurn)
		if preValidateToken.RemainingSupply.GT(preValidateToken.TotalSupply) {
			preValidateToken.RemainingSupply = preValidateToken.TotalSupply
		}
		if err := types.ValidateState(preValidateToken); err != nil {
			return nil, sdkerrors.Wrap(err, "burn would produce invalid token state")
		}
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

	// Update token supply tracking only for stoc-managed tokens
	if isManaged {
		token.TotalSupply = token.TotalSupply.Sub(amountToBurn)

		// If TotalSupply dropped below RemainingSupply, burn excess from module account
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
			// Track actual amount burned from module — prevents invariant violation
			// when actualBurnable < excess (e.g., stale state)
			token.RemainingSupply = token.RemainingSupply.Sub(actualBurnable)
		}

		if err := k.SetToken(ctx, token); err != nil {
			return nil, err
		}
	}

	// Emit event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeBurnToken,
			sdk.NewAttribute(types.AttributeKeyBurner, msg.Creator),
			sdk.NewAttribute(types.AttributeKeyMinimalDenom, msg.Denom),
			sdk.NewAttribute(types.AttributeKeyBurnAmount, amountToBurn.String()),
		),
	)

	return &types.MsgBurnTokenResponse{
		Success: true,
		Message: "Tokens burned successfully",
	}, nil
}
