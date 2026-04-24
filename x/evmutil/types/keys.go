package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// ModuleName defines the module name
	ModuleName = "evmutil"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// RouterKey defines the module's message routing key
	RouterKey = ModuleName
)

var (
	// ConversionMultiplier is the conversion factor between Cosmos (6 decimals) and EVM (18 decimals)
	// 1 ustoc (10^6) = 10^12 astoc (10^18)
	// 1 utstoc (10^6) = 10^12 atstoc (10^18)
	ConversionMultiplier = math.NewInt(1_000_000_000_000) // 10^12
)

// GetCosmosDenom returns the Cosmos token denom with 6 decimals
// This is dynamically determined from sdk.DefaultBondDenom which is set from genesis staking params
// Examples:
//   - Mainnet: sdk.DefaultBondDenom = "ustoc" → returns "ustoc"
//   - Testnet: sdk.DefaultBondDenom = "utstoc" → returns "utstoc"
func GetCosmosDenom() string {
	return sdk.DefaultBondDenom
}

// SafeGetEvmDenom returns the EVM token denom with 18 decimals, or an error.
// Use this in runtime code (queries, tx processing) to avoid panics.
func SafeGetEvmDenom() (string, error) {
	cosmosDenom := sdk.DefaultBondDenom
	if len(cosmosDenom) < 2 || cosmosDenom[0] != 'u' {
		return "", fmt.Errorf("evmutil: DefaultBondDenom must start with 'u' (e.g. 'ustoc'), got: %q", cosmosDenom)
	}
	return "a" + cosmosDenom[1:], nil
}

// GetEvmDenom returns the EVM token denom with 18 decimals.
// Panics if DefaultBondDenom is misconfigured. Use only during init/startup.
// For runtime code, use SafeGetEvmDenom() instead.
// Examples:
//   - Mainnet: sdk.DefaultBondDenom = "ustoc" → returns "astoc"
//   - Testnet: sdk.DefaultBondDenom = "utstoc" → returns "atstoc"
func GetEvmDenom() string {
	denom, err := SafeGetEvmDenom()
	if err != nil {
		panic(err)
	}
	return denom
}
