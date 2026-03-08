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

	// Deduct token creation fee (burned to prevent spam/storage bloat)
	creationFee := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, types.TokenCreationFee))
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creator, types.ModuleName, creationFee); err != nil {
		return nil, sdkerrors.Wrapf(types.ErrInsufficientFunds, "insufficient funds for token creation fee (%s): %v", creationFee.String(), err)
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, creationFee); err != nil {
		return nil, sdkerrors.Wrap(err, "failed to burn creation fee")
	}

	// InitialSupply and TotalSupply are in raw minimal units (decimals field is metadata only)
	initialSupply := token.InitialSupply

	// Determine if we should mint remaining tokens to module account
	remainingSupply := math.NewInt(0)
	if token.TotalSupply.GT(token.InitialSupply) {
		remainingSupply = token.TotalSupply.Sub(token.InitialSupply)
	}

	token.RemainingSupply = remainingSupply

	// NOTE: SetToken is called AFTER all minting succeeds (see below).
	// This follows CEI pattern — if MintCoins/SendCoins fails, no orphan token is left in state.

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

		if amount.IsZero() {
			continue
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
