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
// Business rules (locked 2026-06-03):
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
//     CreateToken's LOW-3 fix is enforced here too (no zero-rounded
//     entries, which the LegacyDec percent path could produce historically;
//     for ReleaseRecipient the amount is already absolute so the rule
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

	// SA-AUDIT-2026-06-06 MED-4 (deep re-audit M4): canonicalize msg.Creator
	// before comparing against token.Creator. cosmos-sdk bech32 Normalize
	// accepts ALL-UPPERCASE bech32 input, so a creator who signed CreateToken
	// from an uppercasing wallet (or any pre-fix token that persists
	// uppercase) can only match a same-case msg.Creator. Canonicalize both
	// sides here so the compare is canonical-vs-canonical regardless of
	// caller input case. CreateToken (msg_server_create_token.go) already
	// canonicalizes at write time per the same MED-4 fix; this is the read-
	// side counterpart that also covers any pre-fix uppercase-persisted
	// tokens.
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
		if !dist.Amount.IsPositive() {
			return nil, sdkerrors.Wrapf(types.ErrInvalidAmount,
				"distributions[%d] (%s): release amount must be positive (got %s)",
				i, dist.Address, dist.Amount.String())
		}
		totalAmount = totalAmount.Add(dist.Amount)
	}

	// SA-AUDIT-2026-06-06 MED-1 (deep re-audit M1, audit batch B):
	// audit hypothesis was "nil RemainingSupply persisted via
	// genesis-import (Token.ValidateState tolerates nil per token.go:189-194)
	// would panic the GT comparison below via big.Int.Cmp on a nil pointer".
	// Empirical refutation (2026-06-06): cosmos-sdk math.Int marshals a nil
	// Int as "0" and Unmarshal of "0" produces a non-nil Int(0). Therefore
	// SetToken → store → GetToken normalizes nil RemainingSupply to Int(0)
	// at write time, and the release handler reads token via FindToken →
	// GetToken which always returns a non-nil RemainingSupply. The
	// totalAmount.GT(token.RemainingSupply) path below is reached with a
	// well-formed Int and produces a clean "exceeds remaining supply 0"
	// error, no panic. No defensive IsNil() guard added here; the burn
	// handler's symmetric SA-2026-06-04 LOW-1 guard at
	// msg_server_burn_token.go:94-103 is itself dead code post-storage-
	// normalization and is kept only for in-place consistency with
	// prior audit acceptance.
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
	for i, dist := range msg.Distributions {
		recipientAddr, err := sdk.AccAddressFromBech32(dist.Address)
		if err != nil {
			return nil, sdkerrors.Wrapf(err, "distributions[%d]: invalid address %s", i, dist.Address)
		}
		coin := sdk.NewCoin(token.MinimalDenom, dist.Amount)
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipientAddr, sdk.NewCoins(coin)); err != nil {
			return nil, sdkerrors.Wrapf(err, "distributions[%d]: SendCoinsFromModuleToAccount failed for recipient %s", i, dist.Address)
		}

		// Per-recipient event for indexer + audit trail. Same attribute names
		// as the prior single-recipient form so existing consumers keep
		// working when they index per-recipient flows.
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				types.EventTypeReleaseTokens,
				sdk.NewAttribute(types.AttributeKeyTokenSymbol, token.Symbol),
				sdk.NewAttribute(types.AttributeKeyMinimalDenom, token.MinimalDenom),
				sdk.NewAttribute(types.AttributeKeyAmount, dist.Amount.String()),
				sdk.NewAttribute(types.AttributeKeyRecipient, dist.Address),
				sdk.NewAttribute(types.AttributeKeyTokenCreator, token.Creator),
			),
		)
	}

	// Persist state AFTER all bank ops succeed.
	// AUDIT NOTE — NOT A CEI VIOLATION: See MintToken in token.go for full rationale.
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
