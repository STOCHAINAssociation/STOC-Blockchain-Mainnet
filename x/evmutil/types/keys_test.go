package types_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"stoc/x/evmutil/types"
)

func TestGetCosmosDenom_ReturnsDefaultBondDenom(t *testing.T) {
	orig := sdk.DefaultBondDenom
	t.Cleanup(func() { sdk.DefaultBondDenom = orig })

	sdk.DefaultBondDenom = "ustoc"
	require.Equal(t, "ustoc", types.GetCosmosDenom())

	sdk.DefaultBondDenom = "utstoc"
	require.Equal(t, "utstoc", types.GetCosmosDenom())
}

func TestGetEvmDenom_MainnetConversion(t *testing.T) {
	orig := sdk.DefaultBondDenom
	t.Cleanup(func() { sdk.DefaultBondDenom = orig })

	sdk.DefaultBondDenom = "ustoc"
	require.Equal(t, "astoc", types.GetEvmDenom())
}

func TestGetEvmDenom_TestnetConversion(t *testing.T) {
	orig := sdk.DefaultBondDenom
	t.Cleanup(func() { sdk.DefaultBondDenom = orig })

	sdk.DefaultBondDenom = "utstoc"
	require.Equal(t, "atstoc", types.GetEvmDenom())
}

func TestGetEvmDenom_FallbackNoUPrefix_Panics(t *testing.T) {
	orig := sdk.DefaultBondDenom
	defer func() {
		sdk.DefaultBondDenom = orig
	}()
	sdk.DefaultBondDenom = "stoc"
	require.Panics(t, func() {
		types.GetEvmDenom()
	}, "GetEvmDenom should panic when DefaultBondDenom doesn't start with 'u'")
}

func TestConversionMultiplier(t *testing.T) {
	expected := math.NewInt(1_000_000_000_000) // 10^12
	require.Equal(t, expected, types.ConversionMultiplier)
}

func TestModuleConstants(t *testing.T) {
	require.Equal(t, "evmutil", types.ModuleName)
	require.Equal(t, "evmutil", types.StoreKey)
	require.Equal(t, "evmutil", types.RouterKey)
}
