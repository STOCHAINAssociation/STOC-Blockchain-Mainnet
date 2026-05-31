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

		// Pre-validate: ensure post-burn TotalSupply will pass basic validation BEFORE any bank mutations.
		// Note: RemainingSupply adjustment depends on module balance (computed post-burn),
		// so full state validation is deferred to SetToken after all mutations.
		preValidateToken := token
		preValidateToken.TotalSupply = token.TotalSupply.Sub(amountToBurn)
		// Use conservative estimate: if remaining > new total, clamp to new total
		// (actual clamp uses min of excess and module balance, which may be less)
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

		// SA-H9-v2 audit-2026-05-30 (user policy clarification):
		// REMOVED the auto module-reserve burn entirely.
		//
		// BUSINESS RULE (user explicit 2026-05-30):
		//   MsgBurnToken = burn token holder ĐANG SỞ HỮU (universal user action).
		//   Creator KHÔNG có privilege đặc biệt — phải sở hữu mới burn được.
		//   Để giảm RemainingSupply (reserve), creator MUST use 2-step flow:
		//     1) MsgReleaseTokens (creator-only) → move reserve to creator balance
		//     2) MsgBurnToken → burn the released balance
		//
		// Why this is better than auto-trigger reserve burn:
		//   - Clean audit trail: 2 explicit events (Release + Burn) instead of
		//     1 implicit cascade.
		//   - Compliance: regulated security token requires authorized intent
		//     declaration per reserve change.
		//   - State drift safety: auto-reconcile MASKS drift. Manual reconcile
		//     FORCES creator notice + investigate via SupplyInvariant warning
		//     (SA-H14 soft fail emits drift event).
		//   - Separation of concerns: MsgBurnToken = balance action,
		//     MsgReleaseTokens = reserve action. Don't collapse.
		//
		// If RemainingSupply > TotalSupply drift happens (shouldn't in normal
		// flows), SupplyInvariant logs warning + emits stoc_supply_drift event
		// (see x/stoc/keeper/invariants.go). Creator reconciles via Release+Burn.

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
