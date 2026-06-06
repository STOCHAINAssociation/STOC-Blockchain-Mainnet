package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Verify that MsgReleaseTokens implements the sdk.Msg interface at compile time
var _ sdk.Msg = &MsgReleaseTokens{}

func (m *MsgReleaseTokens) Route() string { return "stoc" }
func (m *MsgReleaseTokens) Type() string  { return "release_tokens" }
func (m *MsgReleaseTokens) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(m.Creator)
	if err != nil {
		return []sdk.AccAddress{}
	}
	return []sdk.AccAddress{creator}
}
func (m *MsgReleaseTokens) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(m))
}

// ValidateBasic does a sanity check on the provided data.
//
// SA-2026-06-03 (multi-recipient release): the prior single-recipient form
// was replaced by a distributions list because release is a primary-issuance
// event (similar to the CreateToken initial distribution) and issuers
// typically want to drip-release to many holders atomically in a single
// transaction (e.g. ICO tranche, founder allocation, vesting unlock). We
// reject empty lists, duplicate addresses, non-positive amounts, and any
// individual amount above the per-token MaxTokenSupply cap. Cumulative
// reserve sufficiency is checked later by the keeper because it requires
// reading on-chain state.
func (m *MsgReleaseTokens) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(m.Creator)
	if err != nil {
		return errorsmod.Wrap(err, "invalid creator address")
	}

	if m.Symbol == "" {
		return errorsmod.Wrap(ErrInvalidTokenSymbol, "symbol cannot be empty")
	}
	// Symbol is actually a minimalDenom (e.g., "SYMBOL_0"), validate it as a denom
	if err = sdk.ValidateDenom(m.Symbol); err != nil {
		return errorsmod.Wrapf(ErrInvalidTokenSymbol, "invalid symbol/denom format: %v", err)
	}

	if len(m.Distributions) == 0 {
		return errorsmod.Wrap(ErrInvalidAmount, "distributions list cannot be empty")
	}
	if len(m.Distributions) > MaxMultiSendOutputs {
		return errorsmod.Wrapf(ErrInvalidAmount,
			"distributions list has %d entries, exceeds MaxMultiSendOutputs (%d) — split into multiple MsgReleaseTokens",
			len(m.Distributions), MaxMultiSendOutputs)
	}

	// SA-AUDIT-2026-06-08 fix15-4 (R3 token-keeper-fresh-0 +
	// completeness-critic-r3-1): key seenAddrs by canonical bech32 form so
	// uppercase / mixed-case duplicates surface here at ValidateBasic
	// rather than at the bank-op layer. Mirror of the fix15-4 change in
	// types/msg_create_token.go ValidateBasic.
	seenAddrs := make(map[string]struct{}, len(m.Distributions))
	for i, dist := range m.Distributions {
		parsedAddr, err := sdk.AccAddressFromBech32(dist.Address)
		if err != nil {
			return errorsmod.Wrapf(err, "distributions[%d]: invalid address", i)
		}
		canonical := parsedAddr.String()
		if _, dup := seenAddrs[canonical]; dup {
			return errorsmod.Wrapf(ErrInvalidAmount,
				"distributions[%d]: address %s appears more than once — collapse to a single entry",
				i, canonical)
		}
		seenAddrs[canonical] = struct{}{}

		if dist.Amount.IsNil() || dist.Amount.IsZero() || dist.Amount.IsNegative() {
			return errorsmod.Wrapf(ErrInvalidAmount, "distributions[%d]: amount must be positive (got %s)", i, dist.Amount.String())
		}
		if dist.Amount.GT(MaxTokenSupply) {
			return errorsmod.Wrapf(ErrInvalidAmount,
				"distributions[%d]: amount %s exceeds maximum token supply (%s)",
				i, dist.Amount.String(), MaxTokenSupply.String())
		}
	}

	return nil
}
