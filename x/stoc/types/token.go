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
	
	if token.InitialSupply.IsNegative() {
		return fmt.Errorf("initial supply cannot be negative")
	}
	
	if token.TotalSupply.IsNegative() {
		return fmt.Errorf("total supply cannot be negative")
	}
	
	if token.TotalSupply.LT(token.InitialSupply) {
		return fmt.Errorf("total supply cannot be less than initial supply")
	}
	
	// Validate distributions
	totalPercent := math.LegacyZeroDec()
	for _, dist := range token.Distributions {
		if _, err := sdk.AccAddressFromBech32(dist.Address); err != nil {
			return fmt.Errorf("invalid address in distribution: %s", err)
		}
		
		if dist.Percent.IsNegative() || dist.Percent.GT(math.LegacyOneDec()) {
			return fmt.Errorf("distribution percentage must be between 0 and 1")
		}
		
		totalPercent = totalPercent.Add(dist.Percent)
	}
	
	if !totalPercent.Equal(math.LegacyOneDec()) && len(token.Distributions) > 0 {
		return fmt.Errorf("distribution percentages must sum to 1, got %s", totalPercent)
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