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
// SA-L3 audit-2026-05-29 (FALSE POSITIVE — already handled): the input denom
// is lowercased on line below before every comparison, so "USTOC", "Ustoc",
// "uStOc" all collide with "ustoc" / "astoc" / "stoc" exactly the same way
// the canonical lowercase forms do. No mixed-case phishing surface remains
// at the IsNativeDenom boundary. Ensure any future variant check preserves
// this lowercase-first invariant.
func IsNativeDenom(denom string) bool {
	d := strings.ToLower(denom)
	cosmosDenom := strings.ToLower(sdk.DefaultBondDenom)
	if d == cosmosDenom {
		return true
	}
	// Use SafeGetEvmDenom to prevent consensus panic from misconfigured denom.
	// If DefaultBondDenom is corrupted, treat as "not native" instead of halting chain.
	evmDenomStr, err := evmutiltypes.SafeGetEvmDenom()
	if err == nil && d == strings.ToLower(evmDenomStr) {
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

// SafeIsNativeDenom wraps IsNativeDenom with panic recovery.
// IsNativeDenom calls evmutil.GetEvmDenom() which panics if sdk.DefaultBondDenom
// is not properly initialized (e.g., "stake" in tests instead of "ustoc").
//
// Returns (isNative=false, panicked=true) when the underlying call panics. Callers
// in production state-validation paths (ValidateState, etc.) MUST fail closed when
// panicked=true to avoid the audit-fix H2 collision scenario where a token with
// symbol "stoc"/"astoc" is accepted during early upgrade window before
// DefaultBondDenom is initialized, then later collides with the real native denom.
//
// SA-H8 audit-2026-05-29: exported so ante decorators (tax_post, ibc_restriction)
// can use the panic-fail-closed variant — those run on every tx, where a raw
// IsNativeDenom panic would be consensus-halt.
func SafeIsNativeDenom(denom string) (isNative bool, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			isNative = false
			panicked = true
		}
	}()
	isNative = IsNativeDenom(denom)
	return isNative, false
}

// safeIsNativeDenom is the internal-package alias retained for existing callers.
func safeIsNativeDenom(denom string) (bool, bool) {
	return SafeIsNativeDenom(denom)
}

// MaxTokenIDLength bounds the on-chain token ID string. IDs are produced by
// the keeper as a decimal-stringified uint64 counter (≤20 chars) — 64 is a
// safe defensive cap that still leaves headroom for any future ID scheme.
const MaxTokenIDLength = 64

// ValidateState validates a token for state persistence (post-creation mutations like burn/mint).
// Unlike Validate(), this skips creation-time invariants (e.g., TotalSupply >= InitialSupply)
// that may not hold after legitimate operations like burns.
//
// SA-M13/M14/M15/M16 audit-2026-05-29: this function is the security boundary
// for genesis import + any direct SetToken caller that bypasses MsgCreateToken
// validation. All field-level invariants enforced by ValidateBasic must also
// appear here so a malicious genesis cannot inject malformed tokens.
func ValidateState(token Token) error {
	// SA-M15: cap token ID length at the state-import boundary. Proto schema
	// declares ID as unbounded `string`; ValidateBasic does not see genesis.
	if len(token.Id) > MaxTokenIDLength {
		return fmt.Errorf("token id too long (%d > max %d)", len(token.Id), MaxTokenIDLength)
	}
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
	//
	// Audit fix H2: fail closed if the underlying native-denom check panicked.
	// A panic indicates DefaultBondDenom is uninitialized, which is the same
	// window during which a token with symbol "stoc"/"astoc" could be accepted
	// and later collide with the real native denom once initialization completes.
	isNative, panicked := safeIsNativeDenom(token.Symbol)
	if panicked {
		return fmt.Errorf("cannot validate token symbol %q: native denom configuration not initialized", token.Symbol)
	}
	if isNative {
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
	// SA-H12 audit-2026-05-29: enforce MinimalDenom format invariant
	// `SYMBOL_<counter>` to prevent genesis-injected tokens from hijacking
	// FindToken-by-symbol resolution. Format = `<Symbol>_<uint64>`.
	{
		expectedPrefix := token.Symbol + "_"
		if !strings.HasPrefix(token.MinimalDenom, expectedPrefix) {
			return fmt.Errorf("minimal denom %q must have prefix %q", token.MinimalDenom, expectedPrefix)
		}
		suffix := strings.TrimPrefix(token.MinimalDenom, expectedPrefix)
		if suffix == "" {
			return fmt.Errorf("minimal denom %q missing counter suffix", token.MinimalDenom)
		}
		for _, c := range suffix {
			if c < '0' || c > '9' {
				return fmt.Errorf("minimal denom %q counter suffix must be numeric", token.MinimalDenom)
			}
		}
		if strings.TrimSpace(token.MinimalDenom) != token.MinimalDenom {
			return fmt.Errorf("minimal denom %q contains whitespace", token.MinimalDenom)
		}
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
	// SA-M14/M16: enforce Distributions list cap + per-entry validity at the
	// state-import boundary. Genesis can otherwise inject a token with an
	// arbitrarily long distribution list (unbounded uint32 percent values,
	// invalid addresses) since the proto schema places no constraint here.
	if len(token.Distributions) > MaxDistributions {
		return fmt.Errorf("too many distributions in state (%d > max %d)", len(token.Distributions), MaxDistributions)
	}
	for i, dist := range token.Distributions {
		if _, err := sdk.AccAddressFromBech32(dist.Address); err != nil {
			return fmt.Errorf("invalid address in distribution[%d]: %s", i, err)
		}
		if dist.Percent == 0 || dist.Percent > 100 {
			return fmt.Errorf("distribution[%d] percent must be between 1 and 100, got %d", i, dist.Percent)
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
	// Block native denom symbols to prevent confusion (mirrors ValidateState check)
	// Audit fix H2: fail closed if native-denom check panicked (see safeIsNativeDenom comment).
	isNative, panicked := safeIsNativeDenom(token.Symbol)
	if panicked {
		return fmt.Errorf("cannot validate token symbol %q: native denom configuration not initialized", token.Symbol)
	}
	if isNative {
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

			// Overflow check before addition
			if totalPercent > 100-dist.Percent {
				return fmt.Errorf("distribution percentages overflow (sum exceeds 100)")
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
