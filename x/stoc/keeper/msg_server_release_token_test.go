package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"stoc/x/stoc/types"
)

func TestReleaseTokens_Success(t *testing.T) {
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

	// SA-2026-06-02 HIGH-2: release recipient MUST equal creator. Two-step
	// flow (release → creator, then MsgSend → distribution wallet) is what
	// keeps the tax PostHandler on the path. Asserting creator as recipient
	// here covers the happy-path; rejection of non-creator is asserted in
	// TestReleaseTokens_NonCreator (already in this file).
	_, err := ms.ReleaseTokens(ctx, &types.MsgReleaseTokens{
		Creator:   creator,
		Symbol:    denom,
		Amount:    math.NewInt(100),
		Recipient: creator,
	})
	require.NoError(t, err)

	// Verify RemainingSupply decreased
	updated, found := k.GetToken(ctx, denom)
	require.True(t, found)
	require.Equal(t, math.NewInt(400), updated.RemainingSupply)
}

func TestReleaseTokens_BySymbol(t *testing.T) {
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

	// Release using symbol instead of minimalDenom. SA-2026-06-02 HIGH-2:
	// recipient must equal creator.
	_, err := ms.ReleaseTokens(ctx, &types.MsgReleaseTokens{
		Creator:   creator,
		Symbol:    "MYTOKEN",
		Amount:    math.NewInt(100),
		Recipient: creator,
	})
	require.NoError(t, err)

	// Verify RemainingSupply decreased
	updated, found := k.GetToken(ctx, denom)
	require.True(t, found)
	require.Equal(t, math.NewInt(400), updated.RemainingSupply)
}

func TestReleaseTokens_NonCreator(t *testing.T) {
	k, ms, ctx, _ := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	recipientAddr := sdk.AccAddress([]byte("recipient_addr_456"))
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

	// Different address tries to release
	otherAddr := sdk.AccAddress([]byte("other_address_99999"))
	_, err := ms.ReleaseTokens(ctx, &types.MsgReleaseTokens{
		Creator:   otherAddr.String(),
		Symbol:    denom,
		Amount:    math.NewInt(100),
		Recipient: recipientAddr.String(),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrUnauthorized)
}

func TestReleaseTokens_ExceedsRemaining(t *testing.T) {
	k, ms, ctx, _ := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	recipientAddr := sdk.AccAddress([]byte("recipient_addr_456"))
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

	// Try to release more than remaining supply
	_, err := ms.ReleaseTokens(ctx, &types.MsgReleaseTokens{
		Creator:   creator,
		Symbol:    denom,
		Amount:    math.NewInt(600),
		Recipient: recipientAddr.String(),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInsufficientTokens)
}

func TestReleaseTokens_ZeroAmount(t *testing.T) {
	k, ms, ctx, _ := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	recipientAddr := sdk.AccAddress([]byte("recipient_addr_456"))
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

	_, err := ms.ReleaseTokens(ctx, &types.MsgReleaseTokens{
		Creator:   creator,
		Symbol:    denom,
		Amount:    math.NewInt(0),
		Recipient: recipientAddr.String(),
	})
	require.Error(t, err)
}

func TestReleaseTokens_TokenNotFound(t *testing.T) {
	_, ms, ctx, _ := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	recipientAddr := sdk.AccAddress([]byte("recipient_addr_456"))

	_, err := ms.ReleaseTokens(ctx, &types.MsgReleaseTokens{
		Creator:   creatorAddr.String(),
		Symbol:    "NONEXISTENT",
		Amount:    math.NewInt(100),
		Recipient: recipientAddr.String(),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrTokenNotFound)
}
