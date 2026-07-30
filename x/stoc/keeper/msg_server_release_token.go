package keeper

import (
	"context"

	"cosmossdk.io/math"
	"stoc/x/stoc/types"

	sdkerrors "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ReleaseTokens drips part of the on-chain reserve (RemainingSupply) to one
// or more recipients in a single atomic transaction.
//
// Rules:
//
//   - Only the token creator can submit this message (ErrUnauthorized
//     otherwise).
//   - Release is a primary-issuance event analogous to the CreateToken
//     initial distribution and is intentionally TAX-FREE regardless of
//     recipient address. Subsequent secondary-market transfers via the bank
//     module's MsgSend go through the bank tax wrapper and ARE taxed per
//     token.Tax configuration.
//   - The distributions list is fully validated before any bank mutation:
//     the cumulative amount must not exceed RemainingSupply, no individual
//     amount may be non-positive, no address may appear twice (caller MUST
//     pre-aggregate), and the per-recipient minimum-mint check from
//     CreateToken is enforced here too (no zero-rounded entries; for
//     ReleaseRecipient the amount is already absolute so the rule
//     reduces to "amount > 0").
//   - Bank mutations happen in a single loop after the cumulative-supply
//     check. If any individual SendCoinsFromModuleToAccount fails partway,
//     Cosmos SDK tx atomicity (cacheTxContext) reverts the whole tx — same
//     guarantee CreateToken's distributions rely on.
//   - Token.RemainingSupply is decremented by the total cumulative amount
//     once at the end. SetToken's ValidateState invariant
//     (RemainingSupply >= 0 etc.) is checked there.
func (k msgServer) ReleaseTokens(goCtx context.Context, msg *types.MsgReleaseTokens) (*types.MsgReleaseTokensResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	token, err := k.FindToken(ctx, msg.Symbol)
	if err != nil {
		return nil, err
	}

	// Canonicalize msg.Creator before comparing against token.Creator.
	// cosmos-sdk bech32 Normalize accepts ALL-UPPERCASE bech32 input, so a
	// creator who signs from an uppercasing wallet could otherwise only match
	// a same-case msg.Creator. Canonicalize here so the compare is
	// canonical-vs-canonical regardless of caller input case. CreateToken
	// (msg_server_create_token.go) already canonicalizes at write time; this
	// is the read-side counterpart.
	creatorAddr, parseErr := sdk.AccAddressFromBech32(msg.Creator)
	if parseErr != nil {
		return nil, sdkerrors.Wrapf(types.ErrInvalidCreatorAddress, "invalid creator address (%s)", parseErr)
	}
	if creatorAddr.String() != token.Creator {
		return nil, sdkerrors.Wrap(types.ErrUnauthorized, "only token creator can release tokens")
	}

	// Sum cumulative amount and bail before any bank mutation.
	totalAmount := math.ZeroInt()
	for i, dist := range msg.Distributions {
		// authz MsgExec bypasses ValidateBasic; a nil
		// dist.Amount would nil-deref on IsPositive(). Guard IsNil first.
		if dist.Amount.IsNil() || !dist.Amount.IsPositive() {
			return nil, sdkerrors.Wrapf(types.ErrInvalidAmount,
				"distributions[%d] (%s): release amount must be positive (got %s)",
				i, dist.Address, dist.Amount.String())
		}
		totalAmount = totalAmount.Add(dist.Amount)
	}

	// A nil RemainingSupply (tolerated by Token.ValidateState for genesis
	// import) cannot panic the GT comparison below: cosmos-sdk math.Int
	// marshals a nil Int as "0" and Unmarshal of "0" produces a non-nil
	// Int(0). Therefore
	// SetToken → store → GetToken normalizes nil RemainingSupply to Int(0)
	// at write time, and the release handler reads token via FindToken →
	// GetToken which always returns a non-nil RemainingSupply. The
	// totalAmount.GT(token.RemainingSupply) path below is reached with a
	// well-formed Int and produces a clean "exceeds remaining supply 0"
	// error, no panic. No defensive IsNil() guard is needed here; the burn
	// handler's symmetric RemainingSupply IsNil guard is likewise unreachable
	// after storage normalization and is kept only as defense-in-depth.
	if totalAmount.GT(token.RemainingSupply) {
		return nil, sdkerrors.Wrapf(types.ErrInsufficientTokens,
			"cumulative release amount %s exceeds remaining supply %s (split into multiple MsgReleaseTokens or reduce per-recipient amounts)",
			totalAmount.String(), token.RemainingSupply.String())
	}

	// Pre-validate the post-release token state would still be invariant-clean.
	preValidateToken := token
	preValidateToken.RemainingSupply = token.RemainingSupply.Sub(totalAmount)
	if err := types.ValidateState(preValidateToken); err != nil {
		return nil, sdkerrors.Wrap(err, "release would produce invalid token state")
	}

	// Single-loop bank mutation. tx-atomicity protects partial failure: if any
	// SendCoinsFromModuleToAccount errors, the whole tx reverts including any
	// prior successful sends in this loop.
	//
	// Resolve dist.Address to AccAddress once and use .String() for both the
	// bank op and the per-recipient event so the
	// EventTypeReleaseTokens.AttributeKeyRecipient attribute always carries
	// the canonical lowercase form, matching the invariants for
	// token.Creator and token.Distributions[].Address.
	for i, dist := range msg.Distributions {
		recipientAddr, err := sdk.AccAddressFromBech32(dist.Address)
		if err != nil {
			return nil, sdkerrors.Wrapf(err, "distributions[%d]: invalid address %s", i, dist.Address)
		}
		canonicalRecipient := recipientAddr.String()
		// Reject non-canonical (e.g. ALL-UPPERCASE) bech32 in the release
		// manifest instead of silently canonicalizing. Events emit the
		// canonical form, so a forensic replay reading raw msg bytes would
		// otherwise see dist.Address diverge from the emitted attribute — for
		// an STO chain the on-chain tx bytes and the event stream must agree
		// byte-for-byte. Mirrors the canonical-form check in ValidateState so
		// both issuance gates enforce the same rule.
		if dist.Address != canonicalRecipient {
			return nil, sdkerrors.Wrapf(types.ErrInvalidAmount,
				"distributions[%d]: address %q is not canonical bech32 (expected %s)",
				i, dist.Address, canonicalRecipient)
		}
		// Reject module-account
		// recipients before bank ops. `SendCoinsFromModuleToAccount` will
		// revert if recipient is blocked, but mid-loop the error reads as
		// a generic bank error ("address is blocked: stoc...") instead of
		// pointing at the failing distribution index. Pre-check at the
		// creator gate surfaces the rejection with a precise per-index
		// message so the issuer can fix the release manifest without
		// spelunking bank errors. Mirrors the MsgCreateToken
		// Tax.RecipientAddress blocked-address pre-check pattern.
		if k.bankKeeper.BlockedAddr(recipientAddr) {
			return nil, sdkerrors.Wrapf(types.ErrInvalidAmount,
				"distributions[%d]: recipient %s is a blocked module address; use a non-module account",
				i, canonicalRecipient)
		}
		coin := sdk.NewCoin(token.MinimalDenom, dist.Amount)
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipientAddr, sdk.NewCoins(coin)); err != nil {
			return nil, sdkerrors.Wrapf(err, "distributions[%d]: SendCoinsFromModuleToAccount failed for recipient %s", i, canonicalRecipient)
		}

		// Per-recipient event for indexer + audit trail. Same attribute names
		// as the prior single-recipient form so existing consumers keep
		// working when they index per-recipient flows. AttributeKeyRecipient
		// emits canonicalRecipient (not raw dist.Address) to match the
		// persisted-form invariant.
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeReleaseTokens,
				sdk.NewAttribute(types.AttributeKeyTokenSymbol, token.Symbol),
				sdk.NewAttribute(types.AttributeKeyMinimalDenom, token.MinimalDenom),
				sdk.NewAttribute(types.AttributeKeyAmount, dist.Amount.String()),
				sdk.NewAttribute(types.AttributeKeyRecipient, canonicalRecipient),
				sdk.NewAttribute(types.AttributeKeyTokenCreator, token.Creator),
			),
		)
	}

	// Persist state AFTER all bank ops succeed.
	// NOTE — NOT A CEI VIOLATION: See MintToken in token.go for full rationale.
	// Cosmos SDK tx atomicity reverts all bank ops if SetToken fails below.
	token.RemainingSupply = token.RemainingSupply.Sub(totalAmount)
	if err := k.SetToken(ctx, token); err != nil {
		return nil, err
	}

	ctx.Logger().Info("Tokens released (multi-recipient)",
		"symbol", token.Symbol,
		"minimal_denom", token.MinimalDenom,
		"total_amount", totalAmount.String(),
		"recipient_count", len(msg.Distributions),
		"remaining_supply", token.RemainingSupply.String(),
	)

	return &types.MsgReleaseTokensResponse{
		Symbol:         token.Symbol,
		TotalAmount:    totalAmount.String(),
		RecipientCount: uint32(len(msg.Distributions)),
		Success:        true,
		Message:        "tokens released",
	}, nil
}
