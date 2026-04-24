package ante

import (
	"testing"

	"cosmossdk.io/math"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	"github.com/stretchr/testify/require"
)

func TestNewCosmosMinGasPriceDecorator(t *testing.T) {
	params := feemarkettypes.DefaultParams()
	decorator := NewCosmosMinGasPriceDecorator(&params)
	require.NotNil(t, decorator)
	require.Equal(t, &params, decorator.feemarketParams)
}

func TestNewCosmosMinGasPriceDecorator_ZeroMinGasPrice(t *testing.T) {
	params := feemarkettypes.DefaultParams()
	params.MinGasPrice = math.LegacyZeroDec()
	decorator := NewCosmosMinGasPriceDecorator(&params)
	require.True(t, decorator.feemarketParams.MinGasPrice.IsZero())
}

func TestNewCosmosMinGasPriceDecorator_1Gwei(t *testing.T) {
	params := feemarkettypes.DefaultParams()
	params.MinGasPrice = math.LegacyNewDec(1_000_000_000)
	decorator := NewCosmosMinGasPriceDecorator(&params)
	require.Equal(t, math.LegacyNewDec(1_000_000_000), decorator.feemarketParams.MinGasPrice)
}
