package types

import (
	"strings"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// Verify that MsgCreateToken implements the sdk.Msg interface at compile time
var _ sdk.Msg = &MsgCreateToken{}

func (m *MsgCreateToken) Route() string { return "stoc" }
func (m *MsgCreateToken) Type() string  { return "create_token" }
func (m *MsgCreateToken) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(m.Creator)
	if err != nil {
		return []sdk.AccAddress{}
	}
	return []sdk.AccAddress{creator}
}
func (m *MsgCreateToken) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(m))
}

// ValidateBasic does a sanity check on the provided data.
func (m *MsgCreateToken) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(m.Creator)
	if err != nil {
		return errorsmod.Wrap(err, "invalid creator address")
	}

	if m.Name == "" {
		return errorsmod.Wrap(ErrInvalidToken, "name cannot be empty")
	}
	if len(m.Name) > 64 {
		return errorsmod.Wrap(ErrInvalidToken, "name too long (max 64 characters)")
	}
	// Reject control characters in name (prevent XSS/display issues in frontends)
	for _, r := range m.Name {
		if r < 32 || r == 127 {
			return errorsmod.Wrap(ErrInvalidToken, "name contains invalid control characters")
		}
	}

	if m.Symbol == "" {
		return errorsmod.Wrap(ErrInvalidTokenSymbol, "symbol cannot be empty")
	}
	if !TokenSymbolRegex.MatchString(m.Symbol) {
		return errorsmod.Wrap(ErrInvalidTokenSymbol, "symbol must be alphanumeric, start with a letter, and max 32 characters")
	}
	// Prevent symbols that could be confused with native denoms (dynamically detected)
	if IsNativeDenom(m.Symbol) {
		return errorsmod.Wrap(ErrInvalidTokenSymbol, "symbol cannot be a native denom")
	}

	if m.Decimals > 18 {
		return errorsmod.Wrap(ErrInvalidToken, "decimals must be between 0 and 18")
	}

	if m.Logo == "" {
		return errorsmod.Wrap(ErrInvalidToken, "logo cannot be empty")
	}
	if len(m.Logo) > 256 {
		return errorsmod.Wrap(ErrInvalidToken, "logo too long (max 256 characters)")
	}
	// Validate logo is a valid URL scheme (prevent arbitrary data/XSS injection and SSRF)
	// Only allow https:// and ipfs:// — http:// is rejected to prevent SSRF via frontend logo fetching
	if !strings.HasPrefix(m.Logo, "https://") && !strings.HasPrefix(m.Logo, "ipfs://") {
		return errorsmod.Wrap(ErrInvalidToken, "logo must be a valid URL (https:// or ipfs://)")
	}

	if m.InitialSupply.IsNil() || m.InitialSupply.IsNegative() {
		return errorsmod.Wrap(ErrInvalidTokenAmount, "initial supply must be non-negative")
	}
	if m.InitialSupply.GT(MaxTokenSupply) {
		return errorsmod.Wrapf(ErrInvalidTokenAmount, "initial supply exceeds maximum allowed (%s)", MaxTokenSupply.String())
	}

	if m.TotalSupply.IsNil() || m.TotalSupply.IsNegative() {
		return errorsmod.Wrap(ErrInvalidTokenAmount, "total supply must be non-negative")
	}
	if m.TotalSupply.GT(MaxTokenSupply) {
		return errorsmod.Wrapf(ErrInvalidTokenAmount, "total supply exceeds maximum allowed (%s)", MaxTokenSupply.String())
	}

	if !m.TotalSupply.IsNil() && !m.InitialSupply.IsNil() && m.TotalSupply.LT(m.InitialSupply) {
		return errorsmod.Wrap(ErrInvalidTokenAmount, "total supply cannot be less than initial supply")
	}

	// Reject dead tokens: zero total supply with no minting capability
	if !m.Unlimited && !m.TotalSupply.IsNil() && m.TotalSupply.IsZero() {
		return errorsmod.Wrap(ErrInvalidTokenAmount, "total supply cannot be zero for non-unlimited tokens (would create a dead token)")
	}

	// Validate distributions if provided
	if len(m.Distributions) > MaxDistributions {
		return errorsmod.Wrapf(ErrInvalidToken, "too many distributions (max %d)", MaxDistributions)
	}
	if len(m.Distributions) > 0 {
		totalPercent := uint32(0)
		seenAddrs := make(map[string]bool, len(m.Distributions))
		for _, dist := range m.Distributions {
			if _, err := sdk.AccAddressFromBech32(dist.Address); err != nil {
				return errorsmod.Wrapf(ErrInvalidAddress, "invalid distribution address: %s", err)
			}
			if seenAddrs[dist.Address] {
				return errorsmod.Wrap(ErrInvalidToken, "duplicate distribution address")
			}
			seenAddrs[dist.Address] = true
			if dist.Percent == 0 || dist.Percent > 100 {
				return errorsmod.Wrap(ErrInvalidToken, "distribution percentage must be between 1 and 100")
			}
			totalPercent += dist.Percent
		}
		if totalPercent != 100 {
			return errorsmod.Wrapf(ErrInvalidToken, "distribution percentages must sum to 100, got %d", totalPercent)
		}
	}

	// Validate tax
	if !m.Tax.Percent.IsNil() {
		if m.Tax.Percent.IsNegative() {
			return errorsmod.Wrap(ErrInvalidToken, "tax percentage cannot be negative")
		}
		if m.Tax.Percent.GT(MaxTaxPercent) {
			return errorsmod.Wrapf(ErrInvalidToken, "tax percentage cannot exceed %s (50%%)", MaxTaxPercent.String())
		}
		if m.Tax.Percent.IsPositive() {
			if m.Tax.RecipientAddress == "" {
				return errorsmod.Wrap(ErrInvalidTaxRecipient, "tax recipient address is required when tax > 0")
			}
			taxAddr, err := sdk.AccAddressFromBech32(m.Tax.RecipientAddress)
			if err != nil {
				return errorsmod.Wrapf(ErrInvalidTaxRecipient, "invalid tax recipient address: %s", err)
			}
			// Reject module account addresses as tax recipient. Tax collection calls
			// bank.SendCoins(recipient, taxRecipientAddr, taxCoin) — if taxRecipientAddr
			// is a blocked module account, EVERY taxable transfer of this token would
			// fail, permanently bricking the token's transferability.
			if sdk.VerifyAddressFormat(taxAddr) == nil {
				for _, blocked := range []string{"distribution", "gov", "bonded_tokens_pool", "not_bonded_tokens_pool", "stoc"} {
					moduleAddr := sdk.AccAddress(authtypes.NewModuleAddress(blocked))
					if taxAddr.Equals(moduleAddr) {
						return errorsmod.Wrapf(ErrInvalidTaxRecipient, "tax recipient cannot be module account %s (%s)", blocked, m.Tax.RecipientAddress)
					}
				}
			}
		}
	}

	return nil
}
