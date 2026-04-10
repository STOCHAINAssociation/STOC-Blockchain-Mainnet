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
