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
	// Tạo tokenId sử dụng counter từ state
	counter := k.GetTokenCounter(ctx)
	tokenId := fmt.Sprintf("%s-%d", msg.Symbol, counter)
	k.SetTokenCounter(ctx, counter+1)
	minimalDenom := fmt.Sprintf("%s-%s", msg.Symbol, tokenId)
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

	// Validate token
	if err := types.Validate(token); err != nil {
		k.Logger().Error("Token validation failed", "error", err)
		return nil, sdkerrors.Wrap(err, "invalid token")
	}

	// Check if token symbol already exists
	if k.HasToken(ctx, token.MinimalDenom) {
		k.Logger().Error("Token symbol already exists", "symbol", token.MinimalDenom)
		return nil, sdkerrors.Wrapf(types.ErrTokenExists, "token with symbol %s already exists", token.MinimalDenom)
	}

	// Mint initial supply and distribute according to distribution list
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "invalid creator address")
	}

	// Calculate the initial supply as tokens (adjusted for decimals)
	initialSupply := token.InitialSupply

	// Determine if we should mint remaining tokens to module account
	remainingSupply := math.NewInt(0)
	if token.TotalSupply.GT(token.InitialSupply) {
		remainingSupply = token.TotalSupply.Sub(token.InitialSupply)
	}

	token.RemainingSupply = remainingSupply

	k.SetToken(ctx, token)
	// If distributions specified, distribute according to percentages
	if len(token.Distributions) > 0 {
		for _, dist := range token.Distributions {
			recipient, err := sdk.AccAddressFromBech32(dist.Address)
			if err != nil {
				return nil, sdkerrors.Wrap(err, "invalid distribution address")
			}

			// Calculate amount using simple percentage math (40 means 40%)
			// Calculate: amount = initialSupply * percent / 100
			amount := initialSupply.MulRaw(int64(dist.Percent)).QuoRaw(100)

			// Mint tokens to the recipient
			coin := sdk.NewCoin(token.MinimalDenom, amount)
			if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(coin)); err != nil {
				return nil, err
			}

			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, sdk.NewCoins(coin)); err != nil {
				return nil, err
			}
		}
	} else {
		// If no distribution specified, mint everything to creator
		coin := sdk.NewCoin(token.MinimalDenom, initialSupply)
		if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(coin)); err != nil {
			return nil, err
		}

		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, creator, sdk.NewCoins(coin)); err != nil {
			return nil, err
		}
	}

	//If there are remaining tokens (totals > initial), mint them to module account

	if remainingSupply.GT(math.ZeroInt()) {
		coin := sdk.NewCoin(token.MinimalDenom, remainingSupply)
		if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(coin)); err != nil {
			return nil, err
		}

		k.Logger().Info("Minting remaining tokens to module account", "symbol", token.Symbol, "amount", remainingSupply.String())

	}

	// register metadata for token so that the wallet can display it correctly

	denomMetadata := banktypes.Metadata{
		Description: fmt.Sprintf("Token %s created on Stoc chain", token.Name),
		DenomUnits: []*banktypes.DenomUnit{
			{
				Denom:    minimalDenom,
				Exponent: 0,
				Aliases:  []string{token.Symbol},
			},
		},
		Base:    minimalDenom,
		Display: token.Symbol,
		Name:    token.Name,
		Symbol:  token.Symbol,
		URI:     token.Logo,
		URIHash: "",
	}

	// if token has decimals, add DenomUnit with exponent = decimals
	if token.Decimals > 0 {
		// smallestDenom := token.Symbol
		displayDenom := token.Symbol

		denomMetadata.DenomUnits = []*banktypes.DenomUnit{
			{
				Denom:    minimalDenom,
				Exponent: 0,
			},
			{
				Denom:    displayDenom,
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
		Symbol: token.Symbol,
	}, nil

}
