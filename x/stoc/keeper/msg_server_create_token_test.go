package keeper_test

import (
	"strings"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"stoc/x/stoc/types"
)

func TestCreateToken_Success(t *testing.T) {
	k, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()

	// Set up balance for creation fee
	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(200_000_000)))

	msg := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "Test Token",
		Symbol:        "TEST",
		InitialSupply: math.NewInt(1_000_000),
		TotalSupply:   math.NewInt(1_000_000),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
	}

	resp, err := ms.CreateToken(ctx, msg)
	require.NoError(t, err)
	require.Equal(t, "TEST", resp.Symbol)
	require.True(t, resp.Success)

	// Verify token is stored
	token, found := k.GetToken(ctx, "TEST_0")
	require.True(t, found)
	require.Equal(t, "Test Token", token.Name)
	require.Equal(t, "TEST", token.Symbol)
	require.Equal(t, "TEST_0", token.MinimalDenom)
	require.Equal(t, creator, token.Creator)

	// Verify counter incremented
	counter, err := k.GetTokenCounter(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), counter)
}

func TestCreateToken_WithDistributions(t *testing.T) {
	k, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	recipientAddr := sdk.AccAddress([]byte("recipient_addr_1234"))

	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(200_000_000)))

	msg := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "Dist Token",
		Symbol:        "DIST",
		InitialSupply: math.NewInt(1_000_000),
		TotalSupply:   math.NewInt(1_000_000),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
		Distributions: []types.WalletDistribution{
			{Address: creator, Percent: 60},
			{Address: recipientAddr.String(), Percent: 40},
		},
	}

	resp, err := ms.CreateToken(ctx, msg)
	require.NoError(t, err)
	require.True(t, resp.Success)

	// Verify token is stored with distributions
	token, found := k.GetToken(ctx, "DIST_0")
	require.True(t, found)
	require.Equal(t, "Dist Token", token.Name)
	require.Len(t, token.Distributions, 2)
	require.Equal(t, uint32(60), token.Distributions[0].Percent)
	require.Equal(t, uint32(40), token.Distributions[1].Percent)
}

func TestCreateToken_ZeroBalanceCreator(t *testing.T) {
	// STOChain does not charge a creation fee for custom tokens. A wallet
	// with a zero native balance can successfully create a token provided
	// it can still pay the standard transaction gas (gas is handled by the
	// ante chain, not this message handler). This test pins the behavior
	// so a future change that accidentally reintroduces a fee against the
	// creator balance will fail loudly here.
	_, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()

	mockBank.Balances[creator] = sdk.NewCoins()

	msg := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "Zero Balance Token",
		Symbol:        "ZBT",
		InitialSupply: math.NewInt(1_000_000),
		TotalSupply:   math.NewInt(1_000_000),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
	}

	_, err := ms.CreateToken(ctx, msg)
	require.NoError(t, err)
}

func TestCreateToken_InvalidCreator(t *testing.T) {
	_, ms, ctx, _ := setupMsgServerWithMock(t)

	msg := &types.MsgCreateToken{
		Creator:       "",
		Name:          "Bad Token",
		Symbol:        "BAD",
		InitialSupply: math.NewInt(1_000_000),
		TotalSupply:   math.NewInt(1_000_000),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
	}

	_, err := ms.CreateToken(ctx, msg)
	require.Error(t, err)
}

func TestCreateToken_EmptyName(t *testing.T) {
	_, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(200_000_000)))

	msg := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "",
		Symbol:        "NONAME",
		InitialSupply: math.NewInt(1_000_000),
		TotalSupply:   math.NewInt(1_000_000),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
	}

	_, err := ms.CreateToken(ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "name")
}

func TestCreateToken_EmptySymbol(t *testing.T) {
	_, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(200_000_000)))

	msg := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "No Symbol",
		Symbol:        "",
		InitialSupply: math.NewInt(1_000_000),
		TotalSupply:   math.NewInt(1_000_000),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
	}

	_, err := ms.CreateToken(ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "symbol")
}

func TestCreateToken_InvalidLogo(t *testing.T) {
	_, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(200_000_000)))

	msg := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "Bad Logo",
		Symbol:        "BADLOGO",
		InitialSupply: math.NewInt(1_000_000),
		TotalSupply:   math.NewInt(1_000_000),
		Decimals:      6,
		Logo:          "http://example.com/logo.png",
	}

	_, err := ms.CreateToken(ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "logo")
}

func TestCreateToken_TotalLessThanInitial(t *testing.T) {
	_, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(200_000_000)))

	msg := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "Bad Supply",
		Symbol:        "BADSUP",
		InitialSupply: math.NewInt(2_000_000),
		TotalSupply:   math.NewInt(1_000_000),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
	}

	_, err := ms.CreateToken(ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "total supply")
}

func TestCreateToken_ZeroTotalNonUnlimited(t *testing.T) {
	_, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(200_000_000)))

	msg := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "Dead Token",
		Symbol:        "DEAD",
		InitialSupply: math.NewInt(0),
		TotalSupply:   math.NewInt(0),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
		Unlimited:     false,
	}

	_, err := ms.CreateToken(ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "zero")
}

func TestCreateToken_WithRemainingSupply(t *testing.T) {
	k, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(200_000_000)))

	msg := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "Remaining Token",
		Symbol:        "REM",
		InitialSupply: math.NewInt(400_000),
		TotalSupply:   math.NewInt(1_000_000),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
	}

	resp, err := ms.CreateToken(ctx, msg)
	require.NoError(t, err)
	require.True(t, resp.Success)

	token, found := k.GetToken(ctx, "REM_0")
	require.True(t, found)
	require.Equal(t, math.NewInt(600_000), token.RemainingSupply)
}

func TestCreateToken_WithTax(t *testing.T) {
	k, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	recipientAddr := sdk.AccAddress([]byte("tax_recipient_12345"))

	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(200_000_000)))

	msg := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "Tax Token",
		Symbol:        "TAXT",
		InitialSupply: math.NewInt(1_000_000),
		TotalSupply:   math.NewInt(1_000_000),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
		Tax: types.TokenTax{
			Percent:          math.LegacyNewDecWithPrec(1, 1), // 10%
			RecipientAddress: recipientAddr.String(),
		},
	}

	resp, err := ms.CreateToken(ctx, msg)
	require.NoError(t, err)
	require.True(t, resp.Success)

	token, found := k.GetToken(ctx, "TAXT_0")
	require.True(t, found)
	require.True(t, token.Tax.Percent.Equal(math.LegacyNewDecWithPrec(1, 1)))
	require.Equal(t, recipientAddr.String(), token.Tax.RecipientAddress)
}

func TestCreateToken_CounterIncrement(t *testing.T) {
	k, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(500_000_000)))

	// Verify initial counter is 0
	counter, err := k.GetTokenCounter(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(0), counter)

	// Create first token
	msg1 := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "First Token",
		Symbol:        "FIRST",
		InitialSupply: math.NewInt(1_000_000),
		TotalSupply:   math.NewInt(1_000_000),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
	}
	resp1, err := ms.CreateToken(ctx, msg1)
	require.NoError(t, err)
	require.True(t, resp1.Success)

	// Create second token
	msg2 := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "Second Token",
		Symbol:        "SECOND",
		InitialSupply: math.NewInt(2_000_000),
		TotalSupply:   math.NewInt(2_000_000),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
	}
	resp2, err := ms.CreateToken(ctx, msg2)
	require.NoError(t, err)
	require.True(t, resp2.Success)

	// Verify counter is now 2
	counter, err = k.GetTokenCounter(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), counter)

	// Verify minimal denoms
	token1, found := k.GetToken(ctx, "FIRST_0")
	require.True(t, found)
	require.Equal(t, "FIRST_0", token1.MinimalDenom)

	token2, found := k.GetToken(ctx, "SECOND_1")
	require.True(t, found)
	require.Equal(t, "SECOND_1", token2.MinimalDenom)
}

// SA-AUDIT-2026-06-06 MED-4 regression coverage: cosmos-sdk bech32 Normalize
// accepts ALL-UPPERCASE bech32 input. Pre-fix, CreateToken stored
// msg.Creator raw, so an uppercase submission persisted an uppercase
// token.Creator. The downstream MintToken / ReleaseTokens comparisons use
// canonical lowercase (owner.String() or canonicalized msg.Creator), so the
// token would self-DoS — Mint and Release would permanently fail with
// ErrUnauthorized until a gov state-edit. The fix canonicalizes
// msg.Creator at the persistence site so the stored field is always
// lowercase bech32, regardless of caller input case.
func TestCreateToken_UppercaseCreator_StoredAsCanonicalLowercase(t *testing.T) {
	k, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	canonicalCreator := creatorAddr.String()
	// Uppercase variant that cosmos-sdk's bech32 Normalize accepts (lower/mixed
	// is rejected, all-upper is normalized through Decode).
	uppercaseCreator := strings.ToUpper(canonicalCreator)
	require.NotEqual(t, canonicalCreator, uppercaseCreator,
		"test premise: canonical and uppercase forms must differ for the test to be meaningful")

	mockBank.Balances[canonicalCreator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(200_000_000)))

	msg := &types.MsgCreateToken{
		Creator:       uppercaseCreator,
		Name:          "Upper Token",
		Symbol:        "UPPER",
		InitialSupply: math.NewInt(1_000_000),
		TotalSupply:   math.NewInt(1_000_000),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
	}

	_, err := ms.CreateToken(ctx, msg)
	require.NoError(t, err, "MED-4: uppercase bech32 creator must be accepted (cosmos-sdk Normalize allows uppercase)")

	stored, found := k.GetToken(ctx, "UPPER_0")
	require.True(t, found)
	require.Equal(t, canonicalCreator, stored.Creator,
		"MED-4: token.Creator must persist canonical lowercase form, not the uppercase user input")

	// Default-distribution case also canonicalizes — the auto-injected
	// {Address: msg.Creator, Percent: 100} entry should refer to the
	// canonical creator, not the uppercase form.
	require.Len(t, stored.Distributions, 1)
	require.Equal(t, canonicalCreator, stored.Distributions[0].Address,
		"MED-4: default distribution address must use canonicalized creator")
}

// SA-AUDIT-2026-06-06 MED-4: the read-side counterpart in ReleaseTokens
// canonicalizes msg.Creator before comparing against token.Creator. Verify
// that a release call with an uppercase msg.Creator succeeds when the
// stored token.Creator is canonical lowercase. Without canonicalization on
// either side this would fail with ErrUnauthorized.
func TestReleaseTokens_UppercaseMsgCreatorMatchesCanonicalStored(t *testing.T) {
	k, ms, ctx, _ := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	canonicalCreator := creatorAddr.String()
	uppercaseCreator := strings.ToUpper(canonicalCreator)
	denom := "UPPER_0"

	// Seed a token with the canonical creator stored (post-fix shape).
	token := types.Token{
		Id:              denom,
		Name:            "Upper Release Token",
		Symbol:          "UPPER",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(1000),
		TotalSupply:     math.NewInt(1000),
		RemainingSupply: math.NewInt(500),
		MinimalDenom:    denom,
		Creator:         canonicalCreator,
		Unlimited:       false,
	}
	require.NoError(t, k.SetToken(ctx, token))

	// Release with the uppercase variant — must succeed because
	// ReleaseTokens canonicalizes msg.Creator before the compare.
	_, err := ms.ReleaseTokens(ctx, &types.MsgReleaseTokens{
		Creator: uppercaseCreator,
		Symbol:  denom,
		Distributions: []types.ReleaseRecipient{
			{Address: canonicalCreator, Amount: math.NewInt(100)},
		},
	})
	require.NoError(t, err, "MED-4: uppercase msg.Creator must canonicalize before compare against stored canonical token.Creator")
}
