package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"stoc/x/stoc/keeper"
	"stoc/x/stoc/types"
)

func TestSupplyInvariant_Valid(t *testing.T) {
	k, _, ctx, mockBank := setupMsgServerWithMock(t)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	denom := "MYTOKEN_0"

	// Set up token: TotalSupply=1000, RemainingSupply=300
	token := types.Token{
		Id:              denom,
		Name:            "My Token",
		Symbol:          "MYTOKEN",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(1000),
		TotalSupply:     math.NewInt(1000),
		RemainingSupply: math.NewInt(300),
		MinimalDenom:    denom,
		Creator:         creatorAddr.String(),
		Unlimited:       false,
	}
	require.NoError(t, k.SetToken(ctx, token))

	// Bank: creator has 700, module has 300 -> total supply = 1000
	mockBank.Balances[creatorAddr.String()] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(700)))
	mockBank.Balances[moduleAddr.String()] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(300)))

	invariant := keeper.SupplyInvariant(k)
	msg, broken := invariant(ctx)

	require.False(t, broken, "invariant should hold: %s", msg)
}

func TestSupplyInvariant_SupplyMismatch(t *testing.T) {
	k, _, ctx, mockBank := setupMsgServerWithMock(t)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	denom := "MYTOKEN_0"

	token := types.Token{
		Id:              denom,
		Name:            "My Token",
		Symbol:          "MYTOKEN",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(1000),
		TotalSupply:     math.NewInt(1000),
		RemainingSupply: math.NewInt(300),
		MinimalDenom:    denom,
		Creator:         creatorAddr.String(),
		Unlimited:       false,
	}
	require.NoError(t, k.SetToken(ctx, token))

	// Bank total = 600 + 300 = 900, but token TotalSupply = 1000 -> mismatch
	mockBank.Balances[creatorAddr.String()] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(600)))
	mockBank.Balances[moduleAddr.String()] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(300)))

	invariant := keeper.SupplyInvariant(k)
	msg, broken := invariant(ctx)

	require.True(t, broken, "invariant should be broken due to supply mismatch: %s", msg)
	require.Contains(t, msg, "bank supply")
}

func TestSupplyInvariant_ModuleBalanceMismatch(t *testing.T) {
	k, _, ctx, mockBank := setupMsgServerWithMock(t)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	denom := "MYTOKEN_0"

	token := types.Token{
		Id:              denom,
		Name:            "My Token",
		Symbol:          "MYTOKEN",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(1000),
		TotalSupply:     math.NewInt(1000),
		RemainingSupply: math.NewInt(300),
		MinimalDenom:    denom,
		Creator:         creatorAddr.String(),
		Unlimited:       false,
	}
	require.NoError(t, k.SetToken(ctx, token))

	// Bank total = 800 + 200 = 1000 (matches TotalSupply),
	// but module has 200 while RemainingSupply = 300 -> module balance mismatch
	mockBank.Balances[creatorAddr.String()] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(800)))
	mockBank.Balances[moduleAddr.String()] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(200)))

	invariant := keeper.SupplyInvariant(k)
	msg, broken := invariant(ctx)

	require.True(t, broken, "invariant should be broken due to module balance mismatch: %s", msg)
	require.Contains(t, msg, "module balance")
}

func TestSupplyInvariant_MultipleMismatches(t *testing.T) {
	k, _, ctx, mockBank := setupMsgServerWithMock(t)

	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	denom := "MYTOKEN_0"

	token := types.Token{
		Id:              denom,
		Name:            "My Token",
		Symbol:          "MYTOKEN",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(1000),
		TotalSupply:     math.NewInt(1000),
		RemainingSupply: math.NewInt(300),
		MinimalDenom:    denom,
		Creator:         creatorAddr.String(),
		Unlimited:       false,
	}
	require.NoError(t, k.SetToken(ctx, token))

	// Bank state deliberately mismatched:
	// Check 1: bank supply (1500) != TotalSupply (1000) -> broken
	// Check 2: module balance (1500) != RemainingSupply (300) -> broken
	mockBank.Balances[creatorAddr.String()] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(0)))
	mockBank.Balances[moduleAddr.String()] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1500)))

	invariant := keeper.SupplyInvariant(k)
	msg, broken := invariant(ctx)

	require.True(t, broken, "invariant should be broken: %s", msg)
}

func TestSupplyInvariant_NoTokens(t *testing.T) {
	k, _, ctx, _ := setupMsgServerWithMock(t)

	// No tokens in store — invariant should hold
	invariant := keeper.SupplyInvariant(k)
	msg, broken := invariant(ctx)

	require.False(t, broken, "invariant should hold with no tokens: %s", msg)
}
