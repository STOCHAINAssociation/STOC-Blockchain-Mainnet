package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Verify that MsgBurnToken implements the sdk.Msg interface at compile time
var _ sdk.Msg = &MsgBurnToken{}

func (m *MsgBurnToken) Route() string { return "stoc" }
func (m *MsgBurnToken) Type() string  { return "burn_token" }
func (m *MsgBurnToken) GetSigners() []sdk.AccAddress {
	creator, err := sdk.AccAddressFromBech32(m.Creator)
	if err != nil {
		return []sdk.AccAddress{}
	}
	return []sdk.AccAddress{creator}
}
func (m *MsgBurnToken) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(m))
}

// ValidateBasic does a sanity check on the provided data.
func (m *MsgBurnToken) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(m.Creator)
	if err != nil {
		return errorsmod.Wrap(err, "invalid creator address")
	}

	if m.Denom == "" {
		return errorsmod.Wrap(ErrInvalidTokenSymbol, "denom cannot be empty")
	}
	if err := sdk.ValidateDenom(m.Denom); err != nil {
		return errorsmod.Wrapf(ErrInvalidTokenSymbol, "invalid denom: %v", err)
	}

	// Validate amount when not burning all
	if !m.BurnAll {
		if m.Amount.IsNil() || !m.Amount.IsPositive() {
			return errorsmod.Wrap(ErrInvalidAmount, "burn amount must be positive")
		}
	}

	return nil
}
