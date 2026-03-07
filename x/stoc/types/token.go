package types

import (
	"fmt"
	"math/big"
	"regexp"

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

// ValidateToken validates a token structure
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

	// Validate distributions
	totalPercent := uint32(0)
	for _, dist := range token.Distributions {
		if _, err := sdk.AccAddressFromBech32(dist.Address); err != nil {
			return fmt.Errorf("invalid address in distribution: %s", err)
		}

		if dist.Percent > 100 {
			return fmt.Errorf("distribution percentage must be between 0 and 100")
		}

		totalPercent += dist.Percent
	}

	if totalPercent != 100 {
		return fmt.Errorf("distribution percentages must sum to 100, got %d", totalPercent)
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
