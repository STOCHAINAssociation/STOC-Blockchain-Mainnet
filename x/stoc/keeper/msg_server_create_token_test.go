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

// SA-AUDIT-2026-06-08 fix15-5 regression coverage (R3 fix14-regression-2 +
// tax-postdecorator-fresh-2 + token-keeper-fresh-2): when the caller submits
// an Unlimited token with InitialSupply=0 AND a non-empty Distributions
// slice, the two pieces of input contradict each other (zero supply but
// upfront-distribution intent). Pre-fix15-5, the handler silently dropped
// the Distributions list because the mint loop short-circuited on the
// skipDistribution flag, leaving on-chain balances out of sync with the
// persisted Token.Distributions metadata. fix15-5 rejects the
// contradiction loudly so the issuer can fix the input.
func TestCreateToken_UnlimitedZeroInitial_WithDistributions_Rejected(t *testing.T) {
	_, ms, ctx, mockBank := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	otherAddr := sdk.AccAddress([]byte("other_addr_______123"))

	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(200_000_000)))

	msg := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "Conflicting",
		Symbol:        "CONFLICT",
		InitialSupply: math.ZeroInt(),
		TotalSupply:   math.ZeroInt(),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
		Unlimited:     true,
		Distributions: []types.WalletDistribution{
			{Address: creator, Percent: 60},
			{Address: otherAddr.String(), Percent: 40},
		},
	}

	_, err := ms.CreateToken(ctx, msg)
	require.Error(t, err, "fix15-5: contradictory (Unlimited+InitialSupply=0) + non-empty Distributions must be rejected")
	require.Contains(t, err.Error(), "cannot also carry")
	require.Contains(t, err.Error(), "Distributions")
}

// SA-AUDIT-2026-06-08 fix15-5: the empty-Distributions canonical "mint
// later" shape must persist with Distributions=nil (not the auto-injected
// default [{creator, 100%}] that pre-fix15-5 would leave on the token).
// Indexers reading token.Distributions then correctly see "no upfront
// distribution intent", matching the actual on-chain zero balance.
func TestCreateToken_UnlimitedZeroInitial_PersistsEmptyDistributions(t *testing.T) {
	k, ms, ctx, mockBank := setupMsgServerWithMock(t)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(200_000_000)))

	msg := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "Mint Later",
		Symbol:        "ML",
		InitialSupply: math.ZeroInt(),
		TotalSupply:   math.ZeroInt(),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
		Unlimited:     true,
	}

	_, err := ms.CreateToken(ctx, msg)
	require.NoError(t, err)

	stored, found := k.GetToken(ctx, "ML_0")
	require.True(t, found)
	require.True(t, stored.Unlimited)
	require.True(t, stored.InitialSupply.IsZero())
	require.True(t, stored.TotalSupply.IsZero())
	require.Empty(t, stored.Distributions,
		"fix15-5: Unlimited+InitialSupply=0 token must NOT auto-inject default [{creator, 100%}] — persisted Distributions must mirror actual on-chain balances (empty)")
}

// SA-AUDIT-2026-06-07 LOW-3 regression coverage (R2-LOW3): unlimited tokens
// with InitialSupply=0 + TotalSupply=0 must be creatable. Pre-fix14, the
// distribution loop hit the SA-2026-06-02 LOW-3 zero-rounded reject with a
// misleading "increase InitialSupply or merge low-percent recipients" error
// — meaningless for the canonical "mint later via MsgMintTokens" pattern.
// fix14 skips the distribution loop entirely when Unlimited && InitialSupply
// is zero so the token persists with zero circulating + zero reserve, and
// MsgMintTokens can grow supply on demand.
func TestCreateToken_UnlimitedZeroInitialSupply_Creatable(t *testing.T) {
	k, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()

	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(200_000_000)))

	msg := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "Mint Later Token",
		Symbol:        "MLT",
		InitialSupply: math.ZeroInt(),
		TotalSupply:   math.ZeroInt(),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
		Unlimited:     true,
	}

	_, err := ms.CreateToken(ctx, msg)
	require.NoError(t, err, "LOW-3: unlimited token with InitialSupply=0 must be creatable; fix14 skips the distribution loop")

	stored, found := k.GetToken(ctx, "MLT_0")
	require.True(t, found)
	require.True(t, stored.Unlimited)
	require.True(t, stored.InitialSupply.IsZero())
	require.True(t, stored.TotalSupply.IsZero())
	require.True(t, stored.RemainingSupply.IsZero(), "RemainingSupply must be zero since TotalSupply.GT(InitialSupply) is false")
}

// SA-AUDIT-2026-06-07 LOW-3: ensure the existing LOW-3 zero-rounded reject is
// preserved for non-unlimited tokens whose distribution percent rounds to 0.
// The fix14 skip path is gated on token.Unlimited && InitialSupply.IsZero(),
// so a fixed-supply token with InitialSupply>0 and a distribution entry
// that rounds to 0 must still fail loudly (no silent misallocation).
func TestCreateToken_FixedSupplyZeroRounded_StillRejected(t *testing.T) {
	_, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	otherAddr := sdk.AccAddress([]byte("other_addr_______123"))

	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(200_000_000)))

	// InitialSupply=99, three recipients at 33/33/34 — first two compute
	// floor(99*33/100)=32 each, last gets remainder=35. No zero-rounded
	// entry, so this should pass. Use 100/3 split with InitialSupply=2 to
	// guarantee zero-rounded entries.
	msg := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "Tiny Token",
		Symbol:        "TIN",
		InitialSupply: math.NewInt(2),
		TotalSupply:   math.NewInt(2),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
		Distributions: []types.WalletDistribution{
			{Address: creator, Percent: 1},
			{Address: otherAddr.String(), Percent: 99},
		},
	}

	_, err := ms.CreateToken(ctx, msg)
	require.Error(t, err, "LOW-3: non-unlimited token with first-entry zero-rounded distribution must still fail loudly")
	require.Contains(t, err.Error(), "rounds to 0 tokens at initial supply")
}

// SA-AUDIT-2026-06-07 LOW-2 regression coverage (R2-LOW2): fix14 extends the
// MED-4 canonicalization to token.Distributions[i].Address and
// token.Tax.RecipientAddress. Verify that uppercase entries on either field
// are persisted in canonical lowercase form.
func TestCreateToken_UppercaseDistributions_StoredAsCanonical(t *testing.T) {
	k, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	canonicalCreator := creatorAddr.String()
	upperCreator := strings.ToUpper(canonicalCreator)
	recipientAddr := sdk.AccAddress([]byte("recipient_addr_1234"))
	canonicalRecipient := recipientAddr.String()
	upperRecipient := strings.ToUpper(canonicalRecipient)

	mockBank.Balances[canonicalCreator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(200_000_000)))

	msg := &types.MsgCreateToken{
		Creator:       canonicalCreator,
		Name:          "Dist Uppercase",
		Symbol:        "DUP",
		InitialSupply: math.NewInt(1_000_000),
		TotalSupply:   math.NewInt(1_000_000),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
		Distributions: []types.WalletDistribution{
			{Address: upperCreator, Percent: 60},
			{Address: upperRecipient, Percent: 40},
		},
	}

	_, err := ms.CreateToken(ctx, msg)
	require.NoError(t, err)

	stored, found := k.GetToken(ctx, "DUP_0")
	require.True(t, found)
	require.Len(t, stored.Distributions, 2)
	require.Equal(t, canonicalCreator, stored.Distributions[0].Address,
		"LOW-2: distributions[0].Address must persist canonical lowercase")
	require.Equal(t, canonicalRecipient, stored.Distributions[1].Address,
		"LOW-2: distributions[1].Address must persist canonical lowercase")
}

func TestCreateToken_UppercaseTaxRecipient_StoredAsCanonical(t *testing.T) {
	k, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	recipientAddr := sdk.AccAddress([]byte("tax_recipient_______"))
	canonicalRecipient := recipientAddr.String()
	upperRecipient := strings.ToUpper(canonicalRecipient)

	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(200_000_000)))

	msg := &types.MsgCreateToken{
		Creator:       creator,
		Name:          "Tax Uppercase",
		Symbol:        "TUP",
		InitialSupply: math.NewInt(1_000_000),
		TotalSupply:   math.NewInt(1_000_000),
		Decimals:      6,
		Logo:          "https://example.com/logo.png",
		Tax: types.TokenTax{
			Percent:          math.LegacyMustNewDecFromStr("0.05"),
			RecipientAddress: upperRecipient,
		},
	}

	_, err := ms.CreateToken(ctx, msg)
	require.NoError(t, err)

	stored, found := k.GetToken(ctx, "TUP_0")
	require.True(t, found)
	require.Equal(t, canonicalRecipient, stored.Tax.RecipientAddress,
		"LOW-2: token.Tax.RecipientAddress must persist canonical lowercase")
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
