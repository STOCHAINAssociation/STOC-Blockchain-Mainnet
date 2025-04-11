package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ValidateToken validates a token structure
func Validate(token Token) error {
	if token.Name == "" {
		return fmt.Errorf("token name cannot be empty")
	}

	if token.Symbol == "" {
		return fmt.Errorf("token symbol cannot be empty")
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

	// Validate tax
	if token.Tax.Percent.IsNegative() || token.Tax.Percent.GT(math.LegacyOneDec()) {
		return fmt.Errorf("tax percentage must be between 0 and 1")
	}

	if token.Tax.Percent.GT(math.LegacyZeroDec()) {
		if _, err := sdk.AccAddressFromBech32(token.Tax.RecipientAddress); err != nil {
			return fmt.Errorf("invalid tax recipient address: %s", err)
		}
	}

	return nil
}

func ValidateMintToken(symbol string, amount math.Int) error {
	if symbol == "" {
		return fmt.Errorf("symbol cannot be empty")
	}

	if amount.IsNil() || amount.IsZero() || amount.IsNegative() {
		return fmt.Errorf("mint amount must be positive")
	}

	return nil
}
