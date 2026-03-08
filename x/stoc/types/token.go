package types

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TokenSymbolRegex validates token symbol: alphanumeric, starts with letter, max 32 chars.
// Exported so it can be shared with ValidateBasic in msg_create_token.go.
var TokenSymbolRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]{0,31}$`)

// MaxTokenSupply is the maximum allowed supply for a single token (10^30).
// This prevents potential overflow/memory issues with extremely large supply values.
var MaxTokenSupply = math.NewIntFromBigInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil))

// MaxTaxPercent is the maximum tax percentage (50%)
var MaxTaxPercent = math.LegacyNewDecWithPrec(5, 1) // 0.5 = 50%

// MaxDistributions limits the number of distribution entries to prevent gas griefing
const MaxDistributions = 20

// IsNativeDenom dynamically checks if a denom is a native chain denom.
// Uses sdk.DefaultBondDenom (set from genesis staking params) to derive all native denoms:
// - Cosmos denom (e.g. "ustoc" or "utstoc")
// - EVM denom (e.g. "astoc" or "atstoc") — derived by replacing 'u' prefix with 'a'
// - Display denom (e.g. "stoc" or "tstoc") — derived by trimming 'u' prefix
func IsNativeDenom(denom string) bool {
	d := strings.ToLower(denom)
	cosmosDenom := strings.ToLower(sdk.DefaultBondDenom)
	if d == cosmosDenom {
		return true
	}
	// EVM denom: replace 'u' prefix with 'a'
	if len(cosmosDenom) > 0 && cosmosDenom[0] == 'u' {
		evmDenom := "a" + cosmosDenom[1:]
		if d == evmDenom {
			return true
		}
		// Display denom: trim 'u' prefix
		displayDenom := cosmosDenom[1:]
		if d == displayDenom {
			return true
		}
	}
	return false
}

// ValidateState validates a token for state persistence (post-creation mutations like burn/mint).
// Unlike Validate(), this skips creation-time invariants (e.g., TotalSupply >= InitialSupply)
// that may not hold after legitimate operations like burns.
func ValidateState(token Token) error {
	if token.Name == "" {
		return fmt.Errorf("token name cannot be empty")
	}
	if token.Symbol == "" {
		return fmt.Errorf("token symbol cannot be empty")
	}
	if !TokenSymbolRegex.MatchString(token.Symbol) {
		return fmt.Errorf("token symbol must be alphanumeric, start with a letter, and max 32 characters")
	}
	if token.Decimals > 18 {
		return fmt.Errorf("decimals must be between 0 and 18")
	}
	if token.Logo == "" {
		return fmt.Errorf("logo cannot be empty")
	}
	if token.TotalSupply.IsNil() || token.TotalSupply.IsNegative() {
		return fmt.Errorf("total supply cannot be nil or negative")
	}
	if token.TotalSupply.GT(MaxTokenSupply) {
		return fmt.Errorf("total supply exceeds maximum allowed (%s)", MaxTokenSupply.String())
	}
	if !token.RemainingSupply.IsNil() && token.RemainingSupply.IsNegative() {
		return fmt.Errorf("remaining supply cannot be negative")
	}
	if !token.RemainingSupply.IsNil() && token.RemainingSupply.GT(token.TotalSupply) {
		return fmt.Errorf("remaining supply (%s) exceeds total supply (%s)", token.RemainingSupply.String(), token.TotalSupply.String())
	}
	if token.MinimalDenom == "" {
		return fmt.Errorf("minimal denom cannot be empty")
	}
	return nil
}

// Validate validates a token structure (for creation-time validation)
func Validate(token Token) error {
	if token.Name == "" {
		return fmt.Errorf("token name cannot be empty")
	}

	if token.Symbol == "" {
		return fmt.Errorf("token symbol cannot be empty")
	}
	if !TokenSymbolRegex.MatchString(token.Symbol) {
		return fmt.Errorf("token symbol must be alphanumeric, start with a letter, and max 32 characters")
	}

	if token.Decimals > 18 {
		return fmt.Errorf("decimals must be between 0 and 18")
	}

	if token.Logo == "" {
		return fmt.Errorf("logo cannot be empty")
	}

	if token.InitialSupply.IsNil() {
		return fmt.Errorf("initial supply cannot be nil")
	}

	if token.InitialSupply.IsNegative() {
		return fmt.Errorf("initial supply cannot be negative")
	}

	if token.TotalSupply.IsNil() {
		return fmt.Errorf("total supply cannot be nil")
	}

	if token.TotalSupply.IsNegative() {
		return fmt.Errorf("total supply cannot be negative")
	}

	// Upper bound check for supply values
	if token.InitialSupply.GT(MaxTokenSupply) {
		return fmt.Errorf("initial supply exceeds maximum allowed (%s)", MaxTokenSupply.String())
	}
	if token.TotalSupply.GT(MaxTokenSupply) {
		return fmt.Errorf("total supply exceeds maximum allowed (%s)", MaxTokenSupply.String())
	}

	if !token.TotalSupply.IsNil() && !token.InitialSupply.IsNil() && token.TotalSupply.LT(token.InitialSupply) {
		return fmt.Errorf("total supply cannot be less than initial supply")
	}

	// Reject dead tokens: zero total supply with no minting capability
	if !token.Unlimited && !token.TotalSupply.IsNil() && token.TotalSupply.IsZero() {
		return fmt.Errorf("total supply cannot be zero for non-unlimited tokens")
	}

	if token.MinimalDenom == "" {
		return fmt.Errorf("minimal denom cannot be empty")
	}

	// Validate creator address
	if token.Creator != "" {
		if _, err := sdk.AccAddressFromBech32(token.Creator); err != nil {
			return fmt.Errorf("invalid creator address: %s", err)
		}
	}

	// Validate distributions (only when present — empty is valid for genesis/migration)
	if len(token.Distributions) > MaxDistributions {
		return fmt.Errorf("too many distributions (%d > max %d)", len(token.Distributions), MaxDistributions)
	}
	if len(token.Distributions) > 0 {
		totalPercent := uint32(0)
		for _, dist := range token.Distributions {
			if _, err := sdk.AccAddressFromBech32(dist.Address); err != nil {
				return fmt.Errorf("invalid address in distribution: %s", err)
			}

			if dist.Percent == 0 || dist.Percent > 100 {
				return fmt.Errorf("distribution percentage must be between 1 and 100")
			}

			totalPercent += dist.Percent
		}

		if totalPercent != 100 {
			return fmt.Errorf("distribution percentages must sum to 100, got %d", totalPercent)
		}
	}

	// Validate tax — nil check prevents panic on uninitialized Tax.Percent
	if !token.Tax.Percent.IsNil() {
		if token.Tax.Percent.IsNegative() || token.Tax.Percent.GT(MaxTaxPercent) {
			return fmt.Errorf("tax percentage must be between 0 and %s (50%%)", MaxTaxPercent.String())
		}

		if token.Tax.Percent.GT(math.LegacyZeroDec()) {
			if _, err := sdk.AccAddressFromBech32(token.Tax.RecipientAddress); err != nil {
				return fmt.Errorf("invalid tax recipient address: %s", err)
			}
		}
	}

	return nil
}
