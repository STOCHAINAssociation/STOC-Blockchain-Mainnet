package app

import (
	"testing"

	"cosmossdk.io/math"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	evmutiltypes "stoc/x/evmutil/types"
	stoctypes "stoc/x/stoc/types"
)

func v504Setup(t *testing.T, bondDenom string) (*App, sdk.Context) {
	t.Helper()
	a := v503App(t)
	ctx := v503Finalize(a, 100)
	p := stakingtypes.DefaultParams()
	p.BondDenom = bondDenom
	require.NoError(t, a.StakingKeeper.SetParams(ctx, p))
	return a, ctx
}

func v504Fund(t *testing.T, a *App, ctx sdk.Context, addr string, denom string, amt math.Int, createAccount bool) {
	t.Helper()
	acc := sdk.MustAccAddressFromBech32(addr)
	if createAccount {
		a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, acc))
	}
	coins := sdk.NewCoins(sdk.NewCoin(denom, amt))
	require.NoError(t, a.BankKeeper.MintCoins(ctx, stoctypes.ModuleName, coins))
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, stoctypes.ModuleName, acc, coins))
}

func v504Run(t *testing.T, a *App, ctx sdk.Context) {
	t.Helper()
	_, err := a.upgradeHandlerV504(ctx, upgradetypes.Plan{Name: UpgradeNameV504, Height: ctx.BlockHeight()}, a.ModuleManager.GetVersionMap())
	require.NoError(t, err)
}

func v504Events(ctx sdk.Context, typ string) []sdk.Event {
	var out []sdk.Event
	for _, e := range ctx.EventManager().Events() {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

func TestV504BurnListConstants(t *testing.T) {
	require.Len(t, v504FoundationBurns, 7)
	sum := math.ZeroInt()
	seen := map[string]bool{}
	for _, b := range v504FoundationBurns {
		addr, err := sdk.AccAddressFromBech32(b.Address)
		require.NoError(t, err, b.Category)
		require.Contains(t, b.Address, "stoc1")
		require.Equal(t, "stoc", b.Address[:4])
		require.False(t, seen[b.Address], "duplicate address")
		seen[b.Address] = true
		require.NotEmpty(t, addr)
		sum = sum.AddRaw(b.Amount)
	}
	require.Equal(t, "3000000000000000", sum.String(), "3,000,000,000 STOC in base units")
}

func TestV504HappyPathAndEvmutilUntouched(t *testing.T) {
	for _, denom := range []string{"ustoc", "udstoc"} { // bond denom is read from state
		t.Run(denom, func(t *testing.T) {
			a, ctx := v504Setup(t, denom)
			extra := math.NewInt(12345)
			for _, b := range v504FoundationBurns {
				v504Fund(t, a, ctx, b.Address, denom, math.NewInt(b.Amount).Add(extra), true)
			}
			// A custom-token balance on a burn wallet must stay untouched.
			v503Fund(t, a, ctx, sdk.MustAccAddressFromBech32(v504FoundationBurns[0].Address), 5)

			evmAddr := authtypes.NewModuleAddress(evmutiltypes.ModuleName)
			evmBefore := a.BankKeeper.GetAllBalances(ctx, evmAddr)
			astocBefore := a.BankKeeper.GetSupply(ctx, "astoc")
			supplyBefore := a.BankKeeper.GetSupply(ctx, denom).Amount

			v504Run(t, a, ctx)

			for _, b := range v504FoundationBurns {
				bal := a.BankKeeper.GetBalance(ctx, sdk.MustAccAddressFromBech32(b.Address), denom).Amount
				require.Equal(t, extra, bal, b.Category)
			}
			require.Equal(t, supplyBefore.SubRaw(3_000_000_000_000_000), a.BankKeeper.GetSupply(ctx, denom).Amount)
			require.Equal(t, evmBefore, a.BankKeeper.GetAllBalances(ctx, evmAddr))
			require.Equal(t, astocBefore, a.BankKeeper.GetSupply(ctx, "astoc"))
			// custom token untouched
			require.Equal(t, int64(5), a.BankKeeper.GetBalance(ctx, sdk.MustAccAddressFromBech32(v504FoundationBurns[0].Address), v503Denom).Amount.Int64())
			require.Len(t, v504Events(ctx, EventTypeV504FoundationBurn), 7)
			require.Empty(t, v504Events(ctx, EventTypeV504FoundationBurnSkipped))
			require.Len(t, v504Events(ctx, EventTypeV504FoundationBurnSummary), 1)
		})
	}
}

func TestV504ShortAndMissingWalletsSkipped(t *testing.T) {
	a, ctx := v504Setup(t, "ustoc")
	short := v504FoundationBurns[1]   // balance one ustoc below the amount
	missing := v504FoundationBurns[2] // no account, no balance
	for _, b := range v504FoundationBurns {
		switch b.Address {
		case missing.Address:
		case short.Address:
			v504Fund(t, a, ctx, b.Address, "ustoc", math.NewInt(b.Amount-1), true)
		default:
			v504Fund(t, a, ctx, b.Address, "ustoc", math.NewInt(b.Amount), true)
		}
	}
	supplyBefore := a.BankKeeper.GetSupply(ctx, "ustoc").Amount

	v504Run(t, a, ctx)

	require.Equal(t, math.NewInt(short.Amount-1), a.BankKeeper.GetBalance(ctx, sdk.MustAccAddressFromBech32(short.Address), "ustoc").Amount, "never partial-burn")
	require.True(t, a.BankKeeper.GetBalance(ctx, sdk.MustAccAddressFromBech32(missing.Address), "ustoc").Amount.IsZero())
	require.Nil(t, a.AccountKeeper.GetAccount(ctx, sdk.MustAccAddressFromBech32(missing.Address)))
	burned := int64(3_000_000_000_000_000) - short.Amount - missing.Amount
	require.Equal(t, supplyBefore.SubRaw(burned), a.BankKeeper.GetSupply(ctx, "ustoc").Amount)
	require.Len(t, v504Events(ctx, EventTypeV504FoundationBurn), 5)
	require.Len(t, v504Events(ctx, EventTypeV504FoundationBurnSkipped), 2)
}

// An unreadable (empty) bond denom skips every wallet and must not error.
func TestV504EmptyBondDenomSkipsAll(t *testing.T) {
	a, ctx := v504Setup(t, "ustoc")
	for _, b := range v504FoundationBurns {
		v504Fund(t, a, ctx, b.Address, "ustoc", math.NewInt(b.Amount), true)
	}
	p := stakingtypes.DefaultParams()
	p.BondDenom = ""
	require.NoError(t, a.StakingKeeper.SetParams(ctx, p))
	supplyBefore := a.BankKeeper.GetSupply(ctx, "ustoc").Amount

	v504Run(t, a, ctx)

	require.Equal(t, supplyBefore, a.BankKeeper.GetSupply(ctx, "ustoc").Amount)
	require.Len(t, v504Events(ctx, EventTypeV504FoundationBurnSkipped), 7)
}

// The bank send-restriction chain must not block ustoc account -> x/stoc
// module while a custom token exists (restrictions only target custom denoms).
func TestV504SendRestrictionAllowsNativeToStocModule(t *testing.T) {
	a, _ := v503Setup(t)
	ctx := v503Finalize(a, 100)
	w := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, w))
	coins := sdk.NewCoins(sdk.NewInt64Coin("ustoc", 10))
	require.NoError(t, a.BankKeeper.MintCoins(ctx, stoctypes.ModuleName, coins))
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, stoctypes.ModuleName, w, coins))
	require.NoError(t, a.BankKeeper.SendCoinsFromAccountToModule(ctx, w, stoctypes.ModuleName, coins))
	require.NoError(t, a.BankKeeper.BurnCoins(ctx, stoctypes.ModuleName, coins))
}
