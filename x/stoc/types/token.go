package types

import (
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

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

// blockedTaxRecipientModules lists module accounts that must never receive
// token tax payments. Each of these accounts either rejects inbound transfers
// or is controlled by chain-level logic unrelated to the issuing company, so
// routing tax there would permanently brick transfers of the taxed token.
//
//   - distribution:          validator reward pool, credits through a separate code path
//   - gov:                   governance deposit pool, returns deposits only on proposal resolution
//   - bonded_tokens_pool:    staking bonded collateral, accepts only Delegate / Undelegate flows
//   - not_bonded_tokens_pool: staking unbonding collateral, same restriction as bonded pool
//   - stoc:                  this module's own account, reserved for CreateToken / Release / Burn bookkeeping
var blockedTaxRecipientModules = []string{
	"distribution",
	"gov",
	"bonded_tokens_pool",
	"not_bonded_tokens_pool",
	ModuleName,
}

// BlockedTaxRecipientModule reports whether the given account address matches
// one of the module accounts forbidden from receiving token tax. On match it
// returns the module name; otherwise it returns an empty string. The caller
// should reject the token or transaction when match is true.
func BlockedTaxRecipientModule(addr sdk.AccAddress) string {
	for _, name := range blockedTaxRecipientModules {
		if addr.Equals(sdk.AccAddress(authtypes.NewModuleAddress(name))) {
			return name
		}
	}
	return ""
}

// IsNativeDenom dynamically checks if a denom is a native chain denom
// (case-SENSITIVE). Uses sdk.DefaultBondDenom and evmutil.GetEvmDenom() as the
// single source of truth:
// - Cosmos denom (e.g. "ustoc" or "utstoc")
// - EVM denom (e.g. "astoc" or "atstoc") — from evmutil.GetEvmDenom()
// - Display denom (e.g. "stoc" or "tstoc") — derived by trimming 'u' prefix
//
// Audit round-20 2026-07-09 (SUPERSEDES the earlier SA-L3 "lowercase is fine"
// note): bank denoms are CASE-SENSITIVE in Cosmos SDK — "USTOC" is a distinct
// coin from "ustoc". The prior implementation lowercased the input before every
// comparison, so a mixed-case denom like "USTOC" was classified as native and
// would take the native fast-path that SKIPS the custom-token SendRestrictions.
// Because IsNativeDenom==true means "unrestricted", mis-classifying a non-native
// denom as native is the unsafe direction. Comparison is now exact (no ToLower):
// canonical ustoc/astoc/stoc still match as before (they ARE lowercase), while a
// mixed-case near-collision is correctly NOT native.
//
// POLARITY WARNING (audit round-21 2026-07-13): this exact-match form is correct
// ONLY for the bank coin.Denom SendRestriction gates (custom_token_restriction.go,
// ibc_restriction.go, tax_post.go, app.go), where "not native" == more restriction
// and real bank denoms are always the unique SYMBOL_<counter>. It is the WRONG
// primitive for validating a user-supplied token.Symbol at CreateToken / Validate /
// ValidateState, where "not native" == symbol allowed. Round-20 dropped the ToLower
// unconditionally, which let uppercase symbols "STOC"/"USTOC"/"ASTOC" bypass the
// SA-H8 native-symbol block and register a byte-identical clone of the native gas
// ticker (display-layer phishing). Symbol validators MUST use the case-INSENSITIVE
// IsNativeSymbol / SafeIsNativeSymbol below — never call IsNativeDenom on a raw Symbol.
func IsNativeDenom(denom string) bool {
	cosmosDenom := sdk.DefaultBondDenom
	if denom == cosmosDenom {
		return true
	}
	// Use SafeGetEvmDenom to prevent consensus panic from misconfigured denom.
	// If DefaultBondDenom is corrupted, treat as "not native" instead of halting chain.
	evmDenomStr, err := evmutiltypes.SafeGetEvmDenom()
	if err == nil && denom == evmDenomStr {
		return true
	}
	// Display denom: trim 'u' prefix (e.g. "ustoc" -> "stoc")
	if len(cosmosDenom) > 0 && cosmosDenom[0] == 'u' {
		displayDenom := cosmosDenom[1:]
		if denom == displayDenom {
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

// IsNativeSymbol reports whether a user-supplied token Symbol collides with a
// reserved native chain denom, compared CASE-INSENSITIVELY.
//
// This is the SYMBOL-validation polarity (REJECT-if-native at MsgCreateToken
// ValidateBasic). Token symbols are display-facing tickers and wallets / Keplr /
// explorers routinely upper-case them, so "STOC" / "Ustoc" / "ASTOC" must be
// rejected exactly like "stoc" / "ustoc" / "astoc" — otherwise a custom token
// renders a byte-identical clone of the native gas asset (SA-H8 native-symbol
// carve-out; round-20 regression fixed round-21 2026-07-13).
//
// Deliberately case-insensitive, UNLIKE IsNativeDenom which is case-SENSITIVE for
// the bank coin.Denom SendRestriction polarity (bank denoms are unique
// SYMBOL_<counter>, exact match is correct + safe there). The two guard OPPOSITE
// directions — do NOT unify them or drop the ToLower here.
func IsNativeSymbol(symbol string) bool {
	return IsNativeDenom(strings.ToLower(symbol))
}

// SafeIsNativeSymbol is the panic-recovering, case-insensitive Symbol check used
// by the consensus-path validators (Token.Validate / Token.ValidateState). It
// mirrors SafeIsNativeDenom's fail-closed contract: panicked==true means the
// native-denom config was uninitialised and the caller MUST reject (audit fix H2).
func SafeIsNativeSymbol(symbol string) (isNative bool, panicked bool) {
	return SafeIsNativeDenom(strings.ToLower(symbol))
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
//
// SA-AUDIT-2026-06-11 fix19 A16-STO-I2 — INTENTIONAL DIVERGENCE from
// Validate(): the "dead token" shape (Unlimited=false, InitialSupply=0,
// TotalSupply>0) is REJECTED by Validate() at creation time but ACCEPTED
// here. Burn/mint sequences on a limited token can legitimately walk state
// through shapes creation would never produce, and genesis must be able to
// re-import ANY state a legal operation sequence can reach. Do NOT "align"
// the two functions — that would brick genesis export/import for chains
// whose history contains such tokens. The supply-vs-bank cross-check for
// genesis lives in x/stoc/module/genesis.go (A16-STO-L4), not here.
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
	// SA-AUDIT-2026-06-10 fix16 A14-STOC-L1: control-character rejection (see Validate above).
	for _, r := range token.Name {
		if r < 32 || r == 127 {
			return fmt.Errorf("token name contains invalid control characters")
		}
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
	isNative, panicked := SafeIsNativeSymbol(token.Symbol)
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
		// SA-2026-06-02 MED-1 (senior-skeptic audit): reject non-canonical
		// counter encodings so `FOO_1` and `FOO_01` cannot coexist as two
		// distinct genesis tokens that explorers + indexers would render
		// identically. The previous regex accepted any digit string for
		// the suffix, which made the SA-H12 prefix invariant insufficient:
		// a genesis-injected `FOO_0001` whose Tax.RecipientAddress points
		// at the attacker would visually clone the legitimate `FOO_1`
		// inside KYC dashboards and Keplr token lists, enabling phishing
		// inside the legitimate STO chain.
		if parsed, parseErr := strconv.ParseUint(suffix, 10, 64); parseErr != nil {
			return fmt.Errorf("minimal denom %q counter suffix %q failed to parse: %s", token.MinimalDenom, suffix, parseErr)
		} else if strconv.FormatUint(parsed, 10) != suffix {
			return fmt.Errorf("minimal denom %q counter suffix %q is not in canonical form (no leading zeros allowed; expected %q)", token.MinimalDenom, suffix, strconv.FormatUint(parsed, 10))
		}
	}
	// SA-2026-06-02 LOW-2: enforce token.Id == MinimalDenom so genesis-imported
	// tokens cannot encode a different ID than their MinimalDenom (which is the
	// effective primary key in the keeper store). Reject malformed input rather
	// than silently rewriting it inside SetToken.
	if token.Id != "" && token.Id != token.MinimalDenom {
		return fmt.Errorf("token.Id %q must equal token.MinimalDenom %q", token.Id, token.MinimalDenom)
	}
	// Require creator address — tokens without a creator become permanently orphaned
	// (no one can mint/release). Reject at state validation to prevent genesis injection.
	if token.Creator == "" {
		return fmt.Errorf("creator address cannot be empty")
	}
	creatorAddr, err := sdk.AccAddressFromBech32(token.Creator)
	if err != nil {
		return fmt.Errorf("invalid creator address in state: %s", err)
	}
	// SA-AUDIT-2026-06-10 fix16 A14-STOC-M1: require canonical lowercase bech32
	// for storage. cosmos-sdk's bech32 codec accepts uppercase round-trips, so a
	// genesis Token with all-uppercase Creator parses fine and persists, but
	// ReleaseTokens / MintToken compare creatorAddr.String() (canonical
	// lowercase) != stored raw Creator → ErrUnauthorized → token permanently
	// uncontrollable. Enforce canonical form at every write boundary.
	if creatorAddr.String() != token.Creator {
		return fmt.Errorf("creator address must be canonical lowercase bech32 (got %q, canonical %q)", token.Creator, creatorAddr.String())
	}
	// Validate tax fields — prevents genesis import of tokens with out-of-range tax or invalid recipient
	if !token.Tax.Percent.IsNil() {
		if token.Tax.Percent.IsNegative() || token.Tax.Percent.GT(MaxTaxPercent) {
			return fmt.Errorf("tax percentage must be between 0 and %s (50%%)", MaxTaxPercent.String())
		}
		// SA-2026-06-04 LOW-2 (comprehensive audit): validate RecipientAddress
		// format whenever it is set, regardless of whether Percent>0. A token
		// with Percent=0 + malformed RecipientAddress would previously slip
		// through ValidateState only to crash tax_post.go on the same field
		// the moment a future governance proposal raises Percent above zero.
		// Fail closed at the boundary instead of carrying a latent landmine.
		if token.Tax.RecipientAddress != "" {
			taxAddr, err := sdk.AccAddressFromBech32(token.Tax.RecipientAddress)
			if err != nil {
				return fmt.Errorf("invalid tax recipient address in state: %s", err)
			}
			// A14-STOC-M1: canonical-form enforcement (see Creator above).
			if taxAddr.String() != token.Tax.RecipientAddress {
				return fmt.Errorf("tax recipient must be canonical lowercase bech32 (got %q, canonical %q)", token.Tax.RecipientAddress, taxAddr.String())
			}
			if mod := BlockedTaxRecipientModule(taxAddr); mod != "" {
				return fmt.Errorf("tax recipient cannot be module account %s (%s)", mod, token.Tax.RecipientAddress)
			}
		}
		// Percent>0 still requires a non-empty RecipientAddress — tax must have
		// somewhere to go. Percent==0 + empty address is the legitimate
		// "tax disabled" state and remains permitted.
		if token.Tax.Percent.GT(math.LegacyZeroDec()) && token.Tax.RecipientAddress == "" {
			return fmt.Errorf("tax enabled but recipient address missing")
		}
	}
	// SA-M14/M16: enforce Distributions list cap + per-entry validity at the
	// state-import boundary. Genesis can otherwise inject a token with an
	// arbitrarily long distribution list (unbounded uint32 percent values,
	// invalid addresses) since the proto schema places no constraint here.
	if len(token.Distributions) > MaxDistributions {
		return fmt.Errorf("too many distributions in state (%d > max %d)", len(token.Distributions), MaxDistributions)
	}
	// SA-2026-06-04 MED-1 (comprehensive audit): mirror Validate() invariant
	// totalPercent == 100 here so genesis import + any direct SetToken caller
	// cannot persist a Distributions list that the original MsgCreateToken
	// validation would reject. The audit found this gap in ValidateState
	// allowed a malformed genesis to install (e.g.) two recipients summing
	// to 70%, which downstream code assumes is impossible (creator silently
	// keeps the 30% remainder, breaking the issuer-attested split).
	if len(token.Distributions) > 0 {
		totalPercent := uint32(0)
		for i, dist := range token.Distributions {
			distAddr, err := sdk.AccAddressFromBech32(dist.Address)
			if err != nil {
				return fmt.Errorf("invalid address in distribution[%d]: %s", i, err)
			}
			// A14-STOC-M1: canonical-form enforcement.
			if distAddr.String() != dist.Address {
				return fmt.Errorf("distribution[%d] address must be canonical lowercase bech32 (got %q, canonical %q)", i, dist.Address, distAddr.String())
			}
			if dist.Percent == 0 || dist.Percent > 100 {
				return fmt.Errorf("distribution[%d] percent must be between 1 and 100, got %d", i, dist.Percent)
			}
			// Overflow guard mirrors Validate() — abort before sum exceeds 100.
			if totalPercent > 100-dist.Percent {
				return fmt.Errorf("distribution percentages overflow in state (sum exceeds 100)")
			}
			totalPercent += dist.Percent
		}
		if totalPercent != 100 {
			return fmt.Errorf("distribution percentages in state must sum to 100, got %d", totalPercent)
		}
	}
	return nil
}

// Validate validates a token structure (for creation-time validation)
func Validate(token Token) error {
	if token.Name == "" {
		return fmt.Errorf("token name cannot be empty")
	}
	// SA-AUDIT-2026-06-10 fix16 A14-STOC-L1: control-character rejection mirrors
	// MsgCreateToken.ValidateBasic. cosmos-sdk authz.DispatchActions bypasses
	// ValidateBasic, so without this guard a MsgCreateToken wrapped in
	// authz.MsgExec can persist a Name with NUL/BEL/etc — XSS/display landmine
	// in explorers/wallets that render Name without escaping.
	for _, r := range token.Name {
		if r < 32 || r == 127 {
			return fmt.Errorf("token name contains invalid control characters")
		}
	}

	if token.Symbol == "" {
		return fmt.Errorf("token symbol cannot be empty")
	}
	if !TokenSymbolRegex.MatchString(token.Symbol) {
		return fmt.Errorf("token symbol must be alphanumeric, start with a letter, and max 32 characters")
	}
	// Block native denom symbols to prevent confusion (mirrors ValidateState check)
	// Audit fix H2: fail closed if native-denom check panicked (see safeIsNativeDenom comment).
	isNative, panicked := SafeIsNativeSymbol(token.Symbol)
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
