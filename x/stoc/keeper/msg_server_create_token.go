package keeper

import (
	"context"
	"fmt"

	"stoc/x/stoc/types"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

func (k msgServer) CreateToken(goCtx context.Context, msg *types.MsgCreateToken) (*types.MsgCreateTokenResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	k.Logger().Info("Starting token creation",
		"symbol", msg.Symbol,
		"name", msg.Name,
		"creator", msg.Creator)
	// Create a token object

	distributions := msg.Distributions
	if len(distributions) == 0 {
		distributions = []types.WalletDistribution{
			{
				Address: msg.Creator,
				Percent: 100,
			},
		}
	}
	taxToUse := msg.Tax
	if taxToUse.Percent.IsNil() {
		taxToUse = types.TokenTax{
			Percent:          math.LegacyZeroDec(),
			RecipientAddress: "",
		}
	}
	// Build token for validation BEFORE incrementing counter
	counter, err := k.GetTokenCounter(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "failed to get token counter")
	}
	// Fail-fast: check counter overflow BEFORE any bank operations to prevent orphan coins
	if counter == ^uint64(0) {
		return nil, sdkerrors.Wrap(types.ErrInvalidTokenAmount, "token counter overflow — maximum number of tokens reached")
	}
	minimalDenom := fmt.Sprintf("%s_%d", msg.Symbol, counter)
	tokenId := minimalDenom
	token := types.Token{
		Id:            tokenId,
		Name:          msg.Name,
		Symbol:        msg.Symbol,
		InitialSupply: msg.InitialSupply,
		TotalSupply:   msg.TotalSupply,
		Decimals:      msg.Decimals,
		Logo:          msg.Logo,
		Distributions: distributions,
		Tax:           taxToUse,
		Creator:       msg.Creator,
		Unlimited:     msg.Unlimited,
		MinimalDenom:  minimalDenom,
	}

	// Validate token BEFORE incrementing counter (prevents counter pollution on invalid tokens)
	if err := types.Validate(token); err != nil {
		k.Logger().Error("Token validation failed", "error", err)
		return nil, sdkerrors.Wrap(err, "invalid token")
	}

	// SA-H11 audit-2026-05-29: reject Tax.RecipientAddress that is bank-blocked.
	// Bank-blocked recipient (module account, etc.) makes every taxable transfer
	// fail at the PostHandler's SendCoins call → token is soft-rugged: all
	// holders permanently unable to transfer it. Creator cannot self-remediate
	// because Tax fields require gov via MsgUpdateParams.
	if token.Tax.RecipientAddress != "" {
		taxRecipient, err := sdk.AccAddressFromBech32(token.Tax.RecipientAddress)
		if err == nil && k.bankKeeper.BlockedAddr(taxRecipient) {
			return nil, sdkerrors.Wrapf(types.ErrInvalidTokenAmount,
				"tax recipient %s is a blocked address; choose a non-module/non-precompile address",
				token.Tax.RecipientAddress)
		}
	}

	// SA-H13 REVERTED rc2g (user policy 2026-05-30):
	// Name / Symbol / Display CAN collide on-chain. Multiple creators with same
	// Symbol "USDC" → each gets unique MinimalDenom via global counter
	// ("USDC_3", "USDC_47", etc).
	//
	// BUSINESS RULE — Trust verification lives at BE indexer layer
	// (`stochain-explorer/stoc-backend-sync-chain`), like Etherscan verified-
	// contract pattern. BE maintains `token_verification` registry; explorer +
	// wallet UI consult BE API to render "✓ Verified" / "⚠️ Unverified" /
	// "🚫 Scam" badges per token. On-chain layer stays permissionless.
	//
	// MinimalDenom uniqueness preserved via counter (see line 41 + 178 below).
	// SafeIsNativeDenom (SA-H8) still blocks ustoc/astoc symbol collision.
	// Tax recipient blocklist (SA-H11) still blocks soft-rug.

	// Validate generated denom against SDK rules
	if err := sdk.ValidateDenom(minimalDenom); err != nil {
		return nil, sdkerrors.Wrapf(types.ErrInvalidTokenSymbol, "generated denom %s is invalid: %v", minimalDenom, err)
	}

	// Check if token already exists
	if k.HasToken(ctx, token.MinimalDenom) {
		k.Logger().Error("Token symbol already exists", "symbol", token.MinimalDenom)
		return nil, sdkerrors.Wrapf(types.ErrTokenExists, "token with symbol %s already exists", token.MinimalDenom)
	}

	// Mint initial supply and distribute according to distribution list
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "invalid creator address")
	}

	// InitialSupply and TotalSupply are in raw minimal units (decimals field is metadata only)
	initialSupply := token.InitialSupply

	// Determine if we should mint remaining tokens to module account
	remainingSupply := math.NewInt(0)
	if token.TotalSupply.GT(token.InitialSupply) {
		remainingSupply = token.TotalSupply.Sub(token.InitialSupply)
	}

	token.RemainingSupply = remainingSupply

	// NOTE: SetToken + SetTokenCounter are called AFTER all minting succeeds (see below).
	// AUDIT NOTE — NOT A CEI VIOLATION (reviewed April 2026, PR #80):
	// In Cosmos SDK, the entire msg handler runs inside BaseApp.cacheTxContext.
	// If any MintCoins/SendCoins call fails mid-loop, the handler returns error,
	// BaseApp discards the cache, and ALL bank mutations revert atomically.
	// No orphan coins, no counter pollution, no partial token state.
	// See MintToken in token.go for detailed rationale on why Solidity CEI patterns
	// do not apply to Cosmos SDK (no re-entrancy, no external calls).

	// Distribute initial supply according to distribution list
	// (distributions always has >= 1 entry: defaults to [{Creator, 100%}] when msg.Distributions is empty)
	totalMinted := math.ZeroInt()
	for i, dist := range token.Distributions {
		recipient, err := sdk.AccAddressFromBech32(dist.Address)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "invalid distribution address")
		}

		var amount math.Int
		if i == len(token.Distributions)-1 {
			// Last recipient gets the remainder to avoid rounding loss
			amount = initialSupply.Sub(totalMinted)
		} else {
			// Calculate: amount = initialSupply * percent / 100
			amount = initialSupply.MulRaw(int64(dist.Percent)).QuoRaw(100)
		}

		if amount.IsNegative() {
			// Should not happen with valid percent values, but guard against
			// chain halt from sdk.NewCoin panic.
			return nil, sdkerrors.Wrapf(types.ErrInvalidAmount,
				"distribution entry %d (%s) computed negative amount %s — totalMinted exceeded initialSupply",
				i, dist.Address, amount.String())
		}
		if amount.IsZero() {
			// SA-2026-06-02 LOW-3 (senior-skeptic audit): the prior version
			// silently `continue`-d on zero-rounded entries WITHOUT
			// incrementing totalMinted, so the last-entry remainder branch
			// at line 150 would push ALL unallocated supply to the last
			// recipient — silently concentrating the entire initial supply
			// in one address even though the creator listed many. For a
			// security-token primary distribution this is a material
			// misallocation that the indexer cannot detect after the fact.
			// Reject loudly so the creator notices the InitialSupply is too
			// small (or the percent slices too thin) for the requested
			// recipient set, and can fix the input before submitting.
			return nil, sdkerrors.Wrapf(types.ErrInvalidAmount,
				"distribution entry %d (address %s, percent %d) rounds to 0 tokens at initial supply %s — increase InitialSupply or merge low-percent recipients",
				i, dist.Address, dist.Percent, initialSupply.String())
		}
		totalMinted = totalMinted.Add(amount)

		// Mint tokens to the recipient
		coin := sdk.NewCoin(token.MinimalDenom, amount)
		if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(coin)); err != nil {
			return nil, err
		}

		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, sdk.NewCoins(coin)); err != nil {
			return nil, err
		}
	}

	// If there are remaining tokens (totals > initial), mint them to module account

	if remainingSupply.GT(math.ZeroInt()) {
		coin := sdk.NewCoin(token.MinimalDenom, remainingSupply)
		if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(coin)); err != nil {
			return nil, err
		}

		k.Logger().Info("Minting remaining tokens to module account", "symbol", token.Symbol, "amount", remainingSupply.String())

	}

	// Persist state AFTER all bank operations succeeded (CEI pattern)
	if err := k.SetTokenCounter(ctx, counter+1); err != nil {
		return nil, sdkerrors.Wrap(err, "failed to set token counter")
	}
	if err := k.SetToken(ctx, token); err != nil {
		return nil, err
	}

	// register metadata for token so that the wallet can display it correctly

	// Use minimalDenom as display to avoid metadata collision when multiple tokens share the same symbol
	denomMetadata := banktypes.Metadata{
		Description: fmt.Sprintf("Token %s (%s) created on Stoc chain", token.Name, token.Symbol),
		DenomUnits: []*banktypes.DenomUnit{
			{
				Denom:    minimalDenom,
				Exponent: 0,
				Aliases:  []string{token.Symbol},
			},
		},
		Base:    minimalDenom,
		Display: minimalDenom,
		Name:    token.Name,
		Symbol:  token.Symbol,
		URI:     token.Logo,
		URIHash: "",
	}

	// if token has decimals, add DenomUnit with exponent = decimals
	if token.Decimals > 0 {
		denomMetadata.DenomUnits = []*banktypes.DenomUnit{
			{
				Denom:    minimalDenom,
				Exponent: 0,
			},
			{
				Denom:    fmt.Sprintf("%s_display", minimalDenom),
				Exponent: uint32(token.Decimals),
				Aliases:  []string{token.Symbol},
			},
		}
	}

	k.bankKeeper.SetDenomMetaData(ctx, denomMetadata)

	// Emit token creation event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeCreateToken,
			sdk.NewAttribute(types.AttributeKeyTokenSymbol, token.Symbol),
			sdk.NewAttribute(types.AttributeKeyTokenName, token.Name),
			sdk.NewAttribute(types.AttributeKeyTokenCreator, token.Creator),
			sdk.NewAttribute(types.AttributeKeyInitialSupply, token.InitialSupply.String()),
			sdk.NewAttribute(types.AttributeKeyMinimalDenom, token.MinimalDenom),
		),
	)

	// After minting:
	k.Logger().Info("Token minting complete",
		"symbol", token.Symbol,
		"amount", initialSupply.String(),
		"recipient", creator.String())

	// Final success log
	k.Logger().Info("Token creation successful", "symbol", token.Symbol)

	return &types.MsgCreateTokenResponse{
		Symbol:  token.Symbol,
		Creator: token.Creator,
		Success: true,
		Message: token.MinimalDenom,
	}, nil

}
