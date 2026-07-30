package keeper_test

import (
	"strings"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"stoc/x/stoc/types"
)

// Multi-recipient release: MsgReleaseTokens accepts a
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

// Regression coverage: non-canonical (e.g. ALL-UPPERCASE) recipients in the
// release manifest are REJECTED at the handler gate instead of silently
// canonicalized, so the event stream can never diverge from the raw msg
// bytes — non-canonical input never reaches the bank op or the event
// emitter at all. Canonical input still succeeds and still emits the
// canonical form (second half of this test).
func TestReleaseTokens_UppercaseRecipient_RejectedNonCanonical(t *testing.T) {
	k, ms, ctx, _ := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	recipientAddr := sdk.AccAddress([]byte("recipient_addr_456"))
	canonicalRecipient := recipientAddr.String()
	upperRecipient := strings.ToUpper(canonicalRecipient)
	require.NotEqual(t, canonicalRecipient, upperRecipient,
		"test premise: canonical and uppercase forms must differ")
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

	// Uppercase manifest entry must be rejected with a
	// per-index canonical-mismatch error.
	_, err := ms.ReleaseTokens(ctx, &types.MsgReleaseTokens{
		Creator: creator,
		Symbol:  denom,
		Distributions: []types.ReleaseRecipient{
			{Address: upperRecipient, Amount: math.NewInt(100)},
		},
	})
	require.Error(t, err, "A16-STO-M2: non-canonical recipient must be rejected")
	require.Contains(t, err.Error(), "not canonical bech32")

	// Canonical input still succeeds and the event stream still carries
	// the canonical form.
	_, err = ms.ReleaseTokens(ctx, &types.MsgReleaseTokens{
		Creator: creator,
		Symbol:  denom,
		Distributions: []types.ReleaseRecipient{
			{Address: canonicalRecipient, Amount: math.NewInt(100)},
		},
	})
	require.NoError(t, err, "canonical recipient must still succeed")

	events := ctx.EventManager().Events()
	var found bool
	for _, ev := range events {
		if ev.Type != types.EventTypeReleaseTokens {
			continue
		}
		for _, a := range ev.Attributes {
			if a.Key == types.AttributeKeyRecipient {
				require.Equal(t, canonicalRecipient, a.Value,
					"fix15-3: AttributeKeyRecipient must emit canonical lowercase form")
				found = true
			}
		}
	}
	require.True(t, found, "expected at least one %s event with AttributeKeyRecipient", types.EventTypeReleaseTokens)
}

// A genesis-imported token with nil RemainingSupply (permitted by
// ValidateState in types/token.go) must not panic the release handler at
// the totalAmount.GT(token.RemainingSupply) comparison. cosmos-sdk
// math.Int marshals nil as "0" and Unmarshal of "0"
// returns Int(0), so SetToken → store → GetToken normalizes nil to a
// non-nil Int(0). Release path always reads via FindToken → GetToken and
// therefore sees a well-formed Int, so a nil-pointer panic cannot occur.
// This test pins the normalization behavior so a future
// regression in math.Int marshal semantics surfaces here.
func TestReleaseTokens_NilRemainingSupplyNormalizedToZero(t *testing.T) {
	k, ms, ctx, _ := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "NILRES_0"

	// RemainingSupply intentionally omitted → math.Int zero value, IsNil()=true
	// in memory. ValidateState (types/token.go) tolerates this for
	// genesis-import.
	token := types.Token{
		Id:            denom,
		Name:          "Nil Reserve",
		Symbol:        "NILRES",
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
		InitialSupply: math.NewInt(1000),
		TotalSupply:   math.NewInt(1000),
		MinimalDenom:  denom,
		Creator:       creator,
		Unlimited:     false,
	}
	require.True(t, token.RemainingSupply.IsNil(),
		"in-memory token literal: omitted RemainingSupply is nil math.Int")
	require.NoError(t, k.SetToken(ctx, token),
		"SetToken must accept nil RemainingSupply per token.go:189-194")

	// Verify storage normalization: GetToken returns non-nil Int(0).
	stored, found := k.GetToken(ctx, denom)
	require.True(t, found)
	require.False(t, stored.RemainingSupply.IsNil(),
		"audit MED-1 refutation: cosmos-sdk math.Int marshals nil as \"0\" so GetToken always returns non-nil RemainingSupply")
	require.True(t, stored.RemainingSupply.IsZero(),
		"normalized RemainingSupply must equal 0 after round-trip")

	// Release on the normalized zero reserve must return the existing
	// "exceeds remaining supply 0" error cleanly (no panic via big.Int.Cmp
	// on nil pointer).
	resp, err := ms.ReleaseTokens(ctx, &types.MsgReleaseTokens{
		Creator: creator,
		Symbol:  denom,
		Distributions: []types.ReleaseRecipient{
			{Address: creator, Amount: math.NewInt(100)},
		},
	})
	require.Error(t, err)
	require.Nil(t, resp)
	require.ErrorIs(t, err, types.ErrInsufficientTokens)
	require.Contains(t, err.Error(), "exceeds remaining supply 0",
		"release must surface the canonical 'exceeds remaining supply 0' error after storage normalization; if this message changes the audit refutation note in msg_server_release_token.go MED-1 may need to be re-verified")
}
