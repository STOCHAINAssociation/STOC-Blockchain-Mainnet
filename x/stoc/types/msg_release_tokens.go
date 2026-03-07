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

	if m.Amount.IsNil() || m.Amount.IsZero() || m.Amount.IsNegative() {
		return errorsmod.Wrap(ErrInvalidAmount, "release amount must be positive")
	}

	_, err = sdk.AccAddressFromBech32(m.Recipient)
	if err != nil {
		return errorsmod.Wrap(err, "invalid recipient address")
	}

	return nil
}
