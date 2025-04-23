package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ sdk.Msg = &MsgUpdateParams{}

// ValidateBasic does a sanity check on the provided data.
func (m *MsgMintTokens) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(m.Creator)
	if err != nil {
		return errorsmod.Wrap(err, "invalid creator address")
	}

	if m.Symbol == "" {
		return errorsmod.Wrap(ErrInvalidTokenSymbol, "symbol cannot be empty")
	}

	if m.Amount.IsNil() || m.Amount.IsZero() || m.Amount.IsNegative() {
		return errorsmod.Wrap(ErrInvalidAmount, "mint amount must be positive")
	}

	return nil
}
