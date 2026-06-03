package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"stoc/x/stoc/types"
)

// SA-2026-06-03 (multi-recipient release): MsgReleaseTokens now accepts a
// distributions list of (address, absolute amount) entries. Tests below
// exercise the validator paths (cumulative cap, dedup, zero/negative
// amount), the happy path (single recipient creator-self, multi-recipient
// fan-out), and the unauthorized creator path.

func TestReleaseTokens_SingleRecipientCreatorSelf(t *testing.T) {
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

	_, err := ms.ReleaseTokens(ctx, &types.MsgReleaseTokens{
		Creator: creator,
		Symbol:  denom,
		Distributions: []types.ReleaseRecipient{
			{Address: creator, Amount: math.NewInt(100)},
		},
	})
	require.NoError(t, err)

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

	_, err := ms.ReleaseTokens(ctx, &types.MsgReleaseTokens{
		Creator: creator,
		Symbol:  "MYTOKEN",
		Distributions: []types.ReleaseRecipient{
			{Address: creator, Amount: math.NewInt(100)},
		},
	})
	require.NoError(t, err)

	updated, found := k.GetToken(ctx, denom)
	require.True(t, found)
	require.Equal(t, math.NewInt(400), updated.RemainingSupply)
}

func TestReleaseTokens_MultiRecipientFanOut(t *testing.T) {
	k, ms, ctx, _ := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "MYTOKEN_0"

	founderA := sdk.AccAddress([]byte("founder_a___________"))
	founderB := sdk.AccAddress([]byte("founder_b___________"))
	founderC := sdk.AccAddress([]byte("founder_c___________"))

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
		Creator: creator,
		Symbol:  denom,
		Distributions: []types.ReleaseRecipient{
			{Address: founderA.String(), Amount: math.NewInt(100)},
			{Address: founderB.String(), Amount: math.NewInt(150)},
			{Address: founderC.String(), Amount: math.NewInt(50)},
		},
	})
	require.NoError(t, err)

	// 500 - (100 + 150 + 50) = 200
	updated, found := k.GetToken(ctx, denom)
	require.True(t, found)
	require.Equal(t, math.NewInt(200), updated.RemainingSupply)
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

	otherAddr := sdk.AccAddress([]byte("other_address_99999"))
	_, err := ms.ReleaseTokens(ctx, &types.MsgReleaseTokens{
		Creator: otherAddr.String(),
		Symbol:  denom,
		Distributions: []types.ReleaseRecipient{
			{Address: recipientAddr.String(), Amount: math.NewInt(100)},
		},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrUnauthorized)
}

func TestReleaseTokens_ExceedsRemainingCumulative(t *testing.T) {
	k, ms, ctx, _ := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "MYTOKEN_0"

	founderA := sdk.AccAddress([]byte("founder_a___________"))
	founderB := sdk.AccAddress([]byte("founder_b___________"))

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

	// Cumulative 300 + 300 = 600 > 500 → reject
	_, err := ms.ReleaseTokens(ctx, &types.MsgReleaseTokens{
		Creator: creator,
		Symbol:  denom,
		Distributions: []types.ReleaseRecipient{
			{Address: founderA.String(), Amount: math.NewInt(300)},
			{Address: founderB.String(), Amount: math.NewInt(300)},
		},
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
		Creator: creator,
		Symbol:  denom,
		Distributions: []types.ReleaseRecipient{
			{Address: recipientAddr.String(), Amount: math.NewInt(0)},
		},
	})
	require.Error(t, err)
}

func TestReleaseTokens_TokenNotFound(t *testing.T) {
	_, ms, ctx, _ := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	recipientAddr := sdk.AccAddress([]byte("recipient_addr_456"))

	_, err := ms.ReleaseTokens(ctx, &types.MsgReleaseTokens{
		Creator: creatorAddr.String(),
		Symbol:  "NONEXISTENT",
		Distributions: []types.ReleaseRecipient{
			{Address: recipientAddr.String(), Amount: math.NewInt(100)},
		},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrTokenNotFound)
}
