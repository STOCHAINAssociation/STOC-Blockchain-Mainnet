package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"stoc/x/stoc/types"
)

func validToken() types.Token {
	return types.Token{
		Id:              "MYTOKEN_0",
		Name:            "My Token",
		Symbol:          "MYTOKEN",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(1000),
		TotalSupply:     math.NewInt(1000),
		RemainingSupply: math.ZeroInt(),
		MinimalDenom:    "MYTOKEN_0",
		Creator:         sdk.AccAddress([]byte("creator_address_123")).String(),
		Unlimited:       false,
		Tax:             types.TokenTax{Percent: math.LegacyZeroDec(), RecipientAddress: ""},
	}
}

func TestSetToken_GetToken(t *testing.T) {
	k, _, ctx, _ := setupMsgServerWithMock(t)

	token := validToken()
	err := k.SetToken(ctx, token)
	require.NoError(t, err)

	got, found := k.GetToken(ctx, token.MinimalDenom)
	require.True(t, found)
	require.Equal(t, token.Name, got.Name)
	require.Equal(t, token.Symbol, got.Symbol)
	require.Equal(t, token.MinimalDenom, got.MinimalDenom)
	require.Equal(t, token.MinimalDenom, got.Id) // Id is enforced to equal MinimalDenom
	require.Equal(t, token.Decimals, got.Decimals)
	require.Equal(t, token.Logo, got.Logo)
	require.True(t, token.InitialSupply.Equal(got.InitialSupply))
	require.True(t, token.TotalSupply.Equal(got.TotalSupply))
	require.True(t, token.RemainingSupply.Equal(got.RemainingSupply))
	require.Equal(t, token.Creator, got.Creator)
	require.Equal(t, token.Unlimited, got.Unlimited)
}

func TestGetToken_NotFound(t *testing.T) {
	k, _, ctx, _ := setupMsgServerWithMock(t)

	_, found := k.GetToken(ctx, "NONEXISTENT_0")
	require.False(t, found)
}

func TestHasToken(t *testing.T) {
	k, _, ctx, _ := setupMsgServerWithMock(t)

	token := validToken()
	require.NoError(t, k.SetToken(ctx, token))

	require.True(t, k.HasToken(ctx, token.MinimalDenom))
	require.False(t, k.HasToken(ctx, "NONEXISTENT_0"))
}

func TestDeleteToken(t *testing.T) {
	k, _, ctx, _ := setupMsgServerWithMock(t)

	token := validToken()
	require.NoError(t, k.SetToken(ctx, token))

	// Verify token exists before delete
	require.True(t, k.HasToken(ctx, token.MinimalDenom))

	// Delete and verify it's gone
	err := k.DeleteToken(ctx, token.MinimalDenom)
	require.NoError(t, err)
	require.False(t, k.HasToken(ctx, token.MinimalDenom))

	// Symbol index should also be cleaned — FindToken by symbol should fail
	_, err = k.FindToken(ctx, token.Symbol)
	require.Error(t, err)
}

func TestGetAllTokens(t *testing.T) {
	k, _, ctx, _ := setupMsgServerWithMock(t)

	creator := sdk.AccAddress([]byte("creator_address_123")).String()

	tokens := []types.Token{
		{
			Id: "ALPHA_0", Name: "Alpha", Symbol: "ALPHA", Decimals: 6,
			Logo: "https://example.com/a.png", InitialSupply: math.NewInt(100),
			TotalSupply: math.NewInt(100), RemainingSupply: math.ZeroInt(),
			MinimalDenom: "ALPHA_0", Creator: creator, Unlimited: false,
			Tax: types.TokenTax{Percent: math.LegacyZeroDec(), RecipientAddress: ""},
		},
		{
			Id: "BETA_0", Name: "Beta", Symbol: "BETA", Decimals: 6,
			Logo: "https://example.com/b.png", InitialSupply: math.NewInt(200),
			TotalSupply: math.NewInt(200), RemainingSupply: math.ZeroInt(),
			MinimalDenom: "BETA_0", Creator: creator, Unlimited: false,
			Tax: types.TokenTax{Percent: math.LegacyZeroDec(), RecipientAddress: ""},
		},
		{
			Id: "GAMMA_0", Name: "Gamma", Symbol: "GAMMA", Decimals: 6,
			Logo: "https://example.com/g.png", InitialSupply: math.NewInt(300),
			TotalSupply: math.NewInt(300), RemainingSupply: math.ZeroInt(),
			MinimalDenom: "GAMMA_0", Creator: creator, Unlimited: false,
			Tax: types.TokenTax{Percent: math.LegacyZeroDec(), RecipientAddress: ""},
		},
	}

	for _, tk := range tokens {
		require.NoError(t, k.SetToken(ctx, tk))
	}

	all := k.GetAllTokens(ctx)
	require.Len(t, all, 3)
}

func TestFindToken_ByMinimalDenom(t *testing.T) {
	k, _, ctx, _ := setupMsgServerWithMock(t)

	token := validToken()
	require.NoError(t, k.SetToken(ctx, token))

	got, err := k.FindToken(ctx, "MYTOKEN_0")
	require.NoError(t, err)
	require.Equal(t, token.MinimalDenom, got.MinimalDenom)
	require.Equal(t, token.Symbol, got.Symbol)
}

func TestFindToken_BySymbol(t *testing.T) {
	k, _, ctx, _ := setupMsgServerWithMock(t)

	token := validToken()
	require.NoError(t, k.SetToken(ctx, token))

	// FindToken with symbol should resolve via symbol index
	got, err := k.FindToken(ctx, "MYTOKEN")
	require.NoError(t, err)
	require.Equal(t, token.MinimalDenom, got.MinimalDenom)
	require.Equal(t, token.Symbol, got.Symbol)
}

func TestFindToken_NotFound(t *testing.T) {
	k, _, ctx, _ := setupMsgServerWithMock(t)

	_, err := k.FindToken(ctx, "UNKNOWN")
	require.Error(t, err)
	require.ErrorContains(t, err, "not found")
}

func TestFindToken_AmbiguousSymbol(t *testing.T) {
	k, _, ctx, _ := setupMsgServerWithMock(t)

	creator := sdk.AccAddress([]byte("creator_address_123")).String()

	// Two tokens with the same symbol but different minimalDenom
	token1 := types.Token{
		Id: "SHARE_0", Name: "Share V1", Symbol: "SHARE", Decimals: 6,
		Logo: "https://example.com/s1.png", InitialSupply: math.NewInt(100),
		TotalSupply: math.NewInt(100), RemainingSupply: math.ZeroInt(),
		MinimalDenom: "SHARE_0", Creator: creator, Unlimited: false,
		Tax: types.TokenTax{Percent: math.LegacyZeroDec(), RecipientAddress: ""},
	}
	token2 := types.Token{
		Id: "SHARE_1", Name: "Share V2", Symbol: "SHARE", Decimals: 6,
		Logo: "https://example.com/s2.png", InitialSupply: math.NewInt(200),
		TotalSupply: math.NewInt(200), RemainingSupply: math.ZeroInt(),
		MinimalDenom: "SHARE_1", Creator: creator, Unlimited: false,
		Tax: types.TokenTax{Percent: math.LegacyZeroDec(), RecipientAddress: ""},
	}

	require.NoError(t, k.SetToken(ctx, token1))
	require.NoError(t, k.SetToken(ctx, token2))

	// FindToken by symbol should return error for ambiguous symbol
	_, err := k.FindToken(ctx, "SHARE")
	require.Error(t, err)
	require.ErrorContains(t, err, "matches 2 tokens")
	require.ErrorContains(t, err, "minimalDenom")
}

func TestGetTokensBySymbol(t *testing.T) {
	k, _, ctx, _ := setupMsgServerWithMock(t)

	creator := sdk.AccAddress([]byte("creator_address_123")).String()

	// Create multiple tokens with the same symbol
	for i, denom := range []string{"DUP_0", "DUP_1", "DUP_2"} {
		tk := types.Token{
			Id: denom, Name: "Dup Token", Symbol: "DUP", Decimals: 6,
			Logo:            "https://example.com/d.png",
			InitialSupply:   math.NewInt(int64((i + 1) * 100)),
			TotalSupply:     math.NewInt(int64((i + 1) * 100)),
			RemainingSupply: math.ZeroInt(),
			MinimalDenom:    denom, Creator: creator, Unlimited: false,
			Tax: types.TokenTax{Percent: math.LegacyZeroDec(), RecipientAddress: ""},
		}
		require.NoError(t, k.SetToken(ctx, tk))
	}

	tokens := k.GetTokensBySymbol(ctx, "DUP")
	require.Len(t, tokens, 3)

	// Verify all returned tokens have the correct symbol
	for _, tk := range tokens {
		require.Equal(t, "DUP", tk.Symbol)
	}
}

func TestGetTokensBySymbol_NotFound(t *testing.T) {
	k, _, ctx, _ := setupMsgServerWithMock(t)

	tokens := k.GetTokensBySymbol(ctx, "NOSUCHSYMBOL")
	require.Empty(t, tokens)
}

func TestSetToken_InvalidToken(t *testing.T) {
	k, _, ctx, _ := setupMsgServerWithMock(t)

	// Token with empty name should fail ValidateState
	invalid := types.Token{
		Id:              "BAD_0",
		Name:            "", // invalid: empty name
		Symbol:          "BAD",
		Decimals:        6,
		Logo:            "https://example.com/bad.png",
		InitialSupply:   math.NewInt(100),
		TotalSupply:     math.NewInt(100),
		RemainingSupply: math.ZeroInt(),
		MinimalDenom:    "BAD_0",
		Creator:         sdk.AccAddress([]byte("creator_address_123")).String(),
		Unlimited:       false,
		Tax:             types.TokenTax{Percent: math.LegacyZeroDec(), RecipientAddress: ""},
	}

	err := k.SetToken(ctx, invalid)
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid token")
}
