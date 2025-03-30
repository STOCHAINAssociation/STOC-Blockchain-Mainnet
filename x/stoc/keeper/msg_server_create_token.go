package keeper

import (
	"context"

	"stoc/x/stoc/types"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
	token := types.Token{
		Name:          msg.Name,
		Symbol:        msg.Symbol,
		InitialSupply: msg.InitialSupply,
		TotalSupply:   msg.TotalSupply,
		Decimals:      msg.Decimals,
		Logo:          msg.Logo,
		Distributions: distributions,
		Tax: types.TokenTax{
			Percent: math.LegacyZeroDec(),
			RecipientAddress: "",
		},
		Creator:       msg.Creator,
	}
	
	// Validate token
	if err := types.Validate(token); err != nil {
		k.Logger().Error("Token validation failed", "error", err)
		return nil, sdkerrors.Wrap(err, "invalid token")
	}
	
	// Check if token symbol already exists
	if k.HasToken(ctx, token.Symbol) {
		k.Logger().Error("Token symbol already exists", "symbol", token.Symbol)
		return nil, sdkerrors.Wrapf(types.ErrTokenExists, "token with symbol %s already exists", token.Symbol)
	}
	
	// Save the token
	k.SetToken(ctx, token)
	k.Logger().Info("Token saved to store", "symbol", token.Symbol)
	
	// Mint initial supply and distribute according to distribution list
	creator, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "invalid creator address")
	}
	
	// Calculate the initial supply as tokens (adjusted for decimals)
	initialSupply := token.InitialSupply
	
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
			coin := sdk.NewCoin(token.Symbol, amount)
			if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(coin)); err != nil {
				return nil, err
			}
			
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, sdk.NewCoins(coin)); err != nil {
				return nil, err
			}
		}
	} else {
		// If no distribution specified, mint everything to creator
		coin := sdk.NewCoin(token.Symbol, initialSupply)
		if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(coin)); err != nil {
			return nil, err
		}
		
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, creator, sdk.NewCoins(coin)); err != nil {
			return nil, err
		}
	}
	
	// Emit token creation event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeCreateToken,
			sdk.NewAttribute(types.AttributeKeyTokenSymbol, token.Symbol),
			sdk.NewAttribute(types.AttributeKeyTokenName, token.Name),
			sdk.NewAttribute(types.AttributeKeyTokenCreator, token.Creator),
			sdk.NewAttribute(types.AttributeKeyInitialSupply, token.InitialSupply.String()),
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
	// return &types.MsgCreateTokenResponse{
	// 	Symbol: "test",
	// }, nil
}