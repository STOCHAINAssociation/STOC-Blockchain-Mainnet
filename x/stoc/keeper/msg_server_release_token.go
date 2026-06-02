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

	// SA-2026-06-02 HIGH-2 (senior-skeptic audit):
	//
	// MsgReleaseTokens moves reserve out of the x/stoc module account via
	// SendCoinsFromModuleToAccount, which is NOT intercepted by the tax
	// PostHandler (the PostHandler only matches bank.MsgSend and
	// bank.MsgMultiSend at the wire level). Allowing the creator to release
	// the reserve directly to an arbitrary recipient would let them bypass
	// the tax entirely on the primary distribution path — for a taxable
	// security token whose entire compliance premise depends on tax being
	// applied at every value-bearing hop, that is a hard NO.
	//
	// Business rule (locked 2026-06-02): MsgReleaseTokens is restricted to
	// the creator address only. Reserve flows into the creator's balance
	// first, then any onward transfer to a real recipient goes through the
	// taxed MsgSend path. Creators who want to fund a distribution wallet
	// must do so via a TWO-STEP flow:
	//
	//   1) MsgReleaseTokens (creator-only, reserve → creator balance)
	//   2) MsgSend (creator → distribution wallet, tax applies)
	//
	// This matches the SA-H9-v2 MsgBurnToken business rule (see
	// msg_server_burn_token.go) — both ops are reserve-side actions and both
	// require the creator to first take custody before any third-party
	// movement. Audit trail: 2 explicit events (Release + Send) instead of
	// 1 implicit cascade. Regulators get the same clarity as in burn.
	//
	// If the future regulator-driven design needs taxable release directly
	// to a non-creator address, replicate the tax math from
	// x/stoc/ante/tax_post.go applyTaxForRecipient inline below rather than
	// relaxing this guard.
	creatorAddr, err := sdk.AccAddressFromBech32(token.Creator)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "token creator address is malformed")
	}
	if !recipient.Equals(creatorAddr) {
		return nil, sdkerrors.Wrapf(types.ErrUnauthorized,
			"MsgReleaseTokens recipient %s must equal token creator %s — release the reserve to the creator first, then transfer via MsgSend (taxed)",
			msg.Recipient, token.Creator,
		)
	}

	coin := sdk.NewCoin(token.MinimalDenom, msg.Amount)
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, sdk.NewCoins(coin)); err != nil {
		return nil, err
	}

	// Persist state AFTER bank op succeeds.
	// AUDIT NOTE — NOT A CEI VIOLATION: See MintToken in token.go for full rationale.
	// Cosmos SDK tx atomicity (cacheTxContext) reverts all bank ops if SetToken fails.
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
