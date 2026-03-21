package types

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	evmutiltypes "stoc/x/evmutil/types"
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
// Uses sdk.DefaultBondDenom and evmutil.GetEvmDenom() as single source of truth:
// - Cosmos denom (e.g. "ustoc" or "utstoc")
// - EVM denom (e.g. "astoc" or "atstoc") — from evmutil.GetEvmDenom()
// - Display denom (e.g. "stoc" or "tstoc") — derived by trimming 'u' prefix
func IsNativeDenom(denom string) bool {
	d := strings.ToLower(denom)
	cosmosDenom := strings.ToLower(sdk.DefaultBondDenom)
	if d == cosmosDenom {
		return true
	}
	// Use evmutil as single source of truth for EVM denom derivation
	evmDenom := strings.ToLower(evmutiltypes.GetEvmDenom())
	if d == evmDenom {
		return true
	}
	// Display denom: trim 'u' prefix (e.g. "ustoc" -> "stoc")
	if len(cosmosDenom) > 0 && cosmosDenom[0] == 'u' {
		displayDenom := cosmosDenom[1:]
		if d == displayDenom {
			return true
		}
	}
	return false
}

// safeIsNativeDenom wraps IsNativeDenom with panic recovery.
// IsNativeDenom calls evmutil.GetEvmDenom() which panics if sdk.DefaultBondDenom
// is not properly initialized (e.g., "stake" in tests instead of "ustoc").
func safeIsNativeDenom(denom string) (isNative bool) {
	defer func() {
		if r := recover(); r != nil {
			isNative = false
		}
	}()
	return IsNativeDenom(denom)
}

// ValidateState validates a token for state persistence (post-creation mutations like burn/mint).
// Unlike Validate(), this skips creation-time invariants (e.g., TotalSupply >= InitialSupply)
// that may not hold after legitimate operations like burns.
func ValidateState(token Token) error {
	if token.Name == "" {
		return fmt.Errorf("token name cannot be empty")
	}
	if len(token.Name) > 64 {
		return fmt.Errorf("token name too long (max 64 characters)")
	}
	if token.Symbol == "" {
		return fmt.Errorf("token symbol cannot be empty")
	}
	if !TokenSymbolRegex.MatchString(token.Symbol) {
		return fmt.Errorf("token symbol must be alphanumeric, start with a letter, and max 32 characters")
	}
	// Block native denom symbols to prevent confusion and genesis injection attacks.
	// Use recover because IsNativeDenom calls GetEvmDenom() which can panic
	// if sdk.DefaultBondDenom is not properly initialized (e.g., in tests).
	if isNative := safeIsNativeDenom(token.Symbol); isNative {
		return fmt.Errorf("token symbol %q conflicts with native chain denom", token.Symbol)
	}
	if token.Decimals > 18 {
		return fmt.Errorf("decimals must be between 0 and 18")
	}
	if token.Logo == "" {
		return fmt.Errorf("logo cannot be empty")
	}
	if len(token.Logo) > 256 {
		return fmt.Errorf("logo too long (max 256 characters)")
	}
	// Validate logo URL scheme to prevent persisting malicious payloads (XSS/SSRF defense)
	if !strings.HasPrefix(token.Logo, "https://") && !strings.HasPrefix(token.Logo, "ipfs://") {
		return fmt.Errorf("logo must be a valid URL (https:// or ipfs://)")
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
	// Require creator address — tokens without a creator become permanently orphaned
	// (no one can mint/release). Reject at state validation to prevent genesis injection.
	if token.Creator == "" {
		return fmt.Errorf("creator address cannot be empty")
	}
	if _, err := sdk.AccAddressFromBech32(token.Creator); err != nil {
		return fmt.Errorf("invalid creator address in state: %s", err)
	}
	// Validate tax fields — prevents genesis import of tokens with out-of-range tax or invalid recipient
	if !token.Tax.Percent.IsNil() {
		if token.Tax.Percent.IsNegative() || token.Tax.Percent.GT(MaxTaxPercent) {
			return fmt.Errorf("tax percentage must be between 0 and %s (50%%)", MaxTaxPercent.String())
		}
		if token.Tax.Percent.GT(math.LegacyZeroDec()) {
			if token.Tax.RecipientAddress == "" {
				return fmt.Errorf("tax enabled but recipient address missing")
			}
			if _, err := sdk.AccAddressFromBech32(token.Tax.RecipientAddress); err != nil {
				return fmt.Errorf("invalid tax recipient address in state: %s", err)
			}
		}
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
			if token.Tax.RecipientAddress == "" {
				return fmt.Errorf("tax enabled but recipient address missing")
			}
			if _, err := sdk.AccAddressFromBech32(token.Tax.RecipientAddress); err != nil {
				return fmt.Errorf("invalid tax recipient address: %s", err)
			}
		}
	}

	return nil
}
