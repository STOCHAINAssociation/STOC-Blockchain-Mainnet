package types

import (
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

// GetEvmDenom returns the EVM token denom with 18 decimals
// This is automatically derived by replacing 'u' prefix with 'a' prefix
// Examples:
//   - Mainnet: sdk.DefaultBondDenom = "ustoc" → returns "astoc"
//   - Testnet: sdk.DefaultBondDenom = "utstoc" → returns "atstoc"
func GetEvmDenom() string {
	cosmosDenom := sdk.DefaultBondDenom
	if len(cosmosDenom) > 1 && cosmosDenom[0] == 'u' {
		// Replace 'u' prefix with 'a' prefix
		// "ustoc" -> "astoc", "utstoc" -> "atstoc"
		return "a" + cosmosDenom[1:]
	}
	// Fail loudly: the EVM denom derivation requires a 'u'-prefixed cosmos denom.
	// A misconfigured DefaultBondDenom would silently produce wrong EVM denoms,
	// leading to fund loss. Panic so operators notice immediately.
	panic("evmutil: DefaultBondDenom must start with 'u' prefix (e.g. 'ustoc'), got: " + cosmosDenom)
}
