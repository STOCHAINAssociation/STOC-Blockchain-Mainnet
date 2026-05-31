package keeper

import (
	"context"

	"stoc/x/stoc/types"

	sdkerrors "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BurnToken allows ANY token holder to burn their own tokens (similar to ERC20 burn).
// This is BY DESIGN — not restricted to token creator, and intentionally includes
// native chain denoms (ustoc/astoc/stoc and their test/devnet variants).
//
// DESIGN RATIONALE — DO NOT ADD "NATIVE DENOM GUARDS" HERE:
//
//  1. Self-only scope: the signer can only burn their own balance (enforced by
//     SendCoinsFromAccountToModule(creator, ...) below). There is no theft vector —
//     one user cannot burn another user's tokens.
//
//  2. EVM parity: Ethereum allows sending to 0x0/0xdead universally. Blocking
//     native self-burn on a Cosmos-EVM chain would break user expectations and
//     break parity with the ERC20 burn pattern users already know.
//
//  3. Industry precedent: Evmos, Injective, Cronos, Osmosis, Juno all allow
//     native self-burn. STOC is consistent with this norm.
//
//  4. Gov inflation policy is orthogonal: governance controls the MINT rate
//     via x/mint params. Burn is independent — Cosmos SDK re-reads TotalSupply
//     each block, so inflation math auto-adapts to any supply decrease without
//     accounting skew.
//
//  5. User sovereignty: the tokens are the user's own funds; destroying them is
//     their right. Forcing users to send to a black-hole address just to achieve
//     the same outcome adds friction with no safety benefit.
//
// For stoc-managed tokens, TotalSupply is updated accordingly and the supply
// invariant (bankSupply == TotalSupply) is preserved. For unmanaged denoms
// (native, IBC vouchers), the burn is a passthrough with no state tracking.
//
// Audit history: In April 2026 an audit initially flagged native burn as a
// MEDIUM-HIGH finding (PR #80 commit bb78619) but it was reverted (0f0ac59)
// after review confirmed the design is intentional. If a future audit or code
// review re-raises this, reference this comment before making any changes.
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

		// Pre-validate post-burn state BEFORE any bank mutations.
		//
		// SA-H9-v2 audit-2026-05-30: auto-adjust of RemainingSupply was removed
		// (see commentary at SetToken site below). Burning user balance reduces
		// TotalSupply but NOT RemainingSupply (the latter tracks module reserve
		// and is reduced via MsgReleaseTokens → MsgBurnToken 2-step flow). If
		// the existing RemainingSupply already exceeds the post-burn TotalSupply,
		// proceeding would persist a drift state that Token.Validate() rejects
		// at SetToken time, wasting the bank ops and producing a confusing
		// downstream error. Reject early with an actionable message instead.
		preValidateToken := token
		preValidateToken.TotalSupply = token.TotalSupply.Sub(amountToBurn)
		if preValidateToken.RemainingSupply.GT(preValidateToken.TotalSupply) {
			return nil, sdkerrors.Wrapf(types.ErrInvalidAmount,
				"burn would create RemainingSupply (%s) > post-burn TotalSupply (%s) drift; reduce reserve via MsgReleaseTokens then MsgBurnToken (SA-H9-v2 2-step flow)",
				preValidateToken.RemainingSupply.String(), preValidateToken.TotalSupply.String())
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

	// Update token supply tracking only for stoc-managed tokens.
	// AUDIT NOTE — NOT A CEI VIOLATION: SetToken runs AFTER bank ops (SendCoinsFromAccountToModule,
	// BurnCoins) by design. See MintToken in token.go for full rationale. Cosmos SDK tx atomicity
	// (cacheTxContext) reverts all bank ops if SetToken fails — no orphan state possible.
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
