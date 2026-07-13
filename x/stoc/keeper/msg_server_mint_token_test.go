package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"stoc/x/stoc/types"
)

func TestMintTokens_Success(t *testing.T) {
	k, ms, ctx, _ := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "MYTOKEN_0"

	token := types.Token{
		Id:              denom,
		Name:            "My Token",
		Symbol:          "MYTOKEN",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(1000),
		TotalSupply:     math.NewInt(1000),
		RemainingSupply: math.NewInt(500),
		MinimalDenom:    denom,
		Creator:         creator,
		Unlimited:       true,
	}
	require.NoError(t, k.SetToken(ctx, token))

	// Mint 100 tokens using minimalDenom
	_, err := ms.MintTokens(ctx, &types.MsgMintTokens{
		Creator: creator,
		Symbol:  denom,
		Amount:  math.NewInt(100),
	})
	require.NoError(t, err)

	// Verify TotalSupply increased
	updated, found := k.GetToken(ctx, denom)
	require.True(t, found)
	require.Equal(t, math.NewInt(1100), updated.TotalSupply)
}

func TestMintTokens_BySymbol(t *testing.T) {
	k, ms, ctx, _ := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "MYTOKEN_0"

	token := types.Token{
		Id:              denom,
		Name:            "My Token",
		Symbol:          "MYTOKEN",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(1000),
		TotalSupply:     math.NewInt(1000),
		RemainingSupply: math.NewInt(500),
		MinimalDenom:    denom,
		Creator:         creator,
		Unlimited:       true,
	}
	require.NoError(t, k.SetToken(ctx, token))

	// Mint using symbol instead of minimalDenom
	_, err := ms.MintTokens(ctx, &types.MsgMintTokens{
		Creator: creator,
		Symbol:  "MYTOKEN",
		Amount:  math.NewInt(100),
	})
	require.NoError(t, err)

	// Verify TotalSupply increased
	updated, found := k.GetToken(ctx, denom)
	require.True(t, found)
	require.Equal(t, math.NewInt(1100), updated.TotalSupply)
}

func TestMintTokens_NotUnlimited(t *testing.T) {
	k, ms, ctx, _ := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "MYTOKEN_0"

	token := types.Token{
		Id:              denom,
		Name:            "My Token",
		Symbol:          "MYTOKEN",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(1000),
		TotalSupply:     math.NewInt(1000),
		RemainingSupply: math.NewInt(500),
		MinimalDenom:    denom,
		Creator:         creator,
		Unlimited:       false,
	}
	require.NoError(t, k.SetToken(ctx, token))

	_, err := ms.MintTokens(ctx, &types.MsgMintTokens{
		Creator: creator,
		Symbol:  denom,
		Amount:  math.NewInt(100),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrCannotMint)
}

func TestMintTokens_NonCreator(t *testing.T) {
	k, ms, ctx, _ := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "MYTOKEN_0"

	token := types.Token{
		Id:              denom,
		Name:            "My Token",
		Symbol:          "MYTOKEN",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(1000),
		TotalSupply:     math.NewInt(1000),
		RemainingSupply: math.NewInt(500),
		MinimalDenom:    denom,
		Creator:         creator,
		Unlimited:       true,
	}
	require.NoError(t, k.SetToken(ctx, token))

	// Different address tries to mint
	otherAddr := sdk.AccAddress([]byte("other_address_99999"))
	_, err := ms.MintTokens(ctx, &types.MsgMintTokens{
		Creator: otherAddr.String(),
		Symbol:  denom,
		Amount:  math.NewInt(100),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrUnauthorized)
}

func TestMintTokens_TokenNotFound(t *testing.T) {
	_, ms, ctx, _ := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))

	_, err := ms.MintTokens(ctx, &types.MsgMintTokens{
		Creator: creatorAddr.String(),
		Symbol:  "NONEXISTENT",
		Amount:  math.NewInt(100),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrTokenNotFound)
}

func TestMintTokens_ZeroAmount(t *testing.T) {
	k, ms, ctx, _ := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "MYTOKEN_0"

	token := types.Token{
		Id:              denom,
		Name:            "My Token",
		Symbol:          "MYTOKEN",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(1000),
		TotalSupply:     math.NewInt(1000),
		RemainingSupply: math.NewInt(500),
		MinimalDenom:    denom,
		Creator:         creator,
		Unlimited:       true,
	}
	require.NoError(t, k.SetToken(ctx, token))

	_, err := ms.MintTokens(ctx, &types.MsgMintTokens{
		Creator: creator,
		Symbol:  denom,
		Amount:  math.NewInt(0),
	})
	require.Error(t, err)
}

func TestMintTokens_ExceedsMaxSupply(t *testing.T) {
	k, ms, ctx, _ := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "MYTOKEN_0"

	// Set TotalSupply close to MaxTokenSupply
	nearMax := types.MaxTokenSupply.Sub(math.NewInt(10))
	token := types.Token{
		Id:              denom,
		Name:            "My Token",
		Symbol:          "MYTOKEN",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   nearMax,
		TotalSupply:     nearMax,
		RemainingSupply: math.NewInt(500),
		MinimalDenom:    denom,
		Creator:         creator,
		Unlimited:       true,
	}
	require.NoError(t, k.SetToken(ctx, token))

	// Attempt to mint amount that pushes past MaxTokenSupply
	_, err := ms.MintTokens(ctx, &types.MsgMintTokens{
		Creator: creator,
		Symbol:  denom,
		Amount:  math.NewInt(100),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidTokenAmount)
}
