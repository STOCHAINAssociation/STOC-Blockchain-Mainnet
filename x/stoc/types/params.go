package types

import (
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

var _ paramtypes.ParamSet = (*Params)(nil)

// ParamKeyTable the param key table for launch module
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// NewParams creates a new Params instance
func NewParams() Params {
	return Params{}
}

// DefaultParams returns a default set of parameters
func DefaultParams() Params {
	return NewParams()
}

// ParamSetPairs get the params.ParamSet
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{}
}

// Validate validates the set of params.
//
// SA-L2 audit-2026-05-29: this is intentionally a no-op because the Params
// struct currently has no fields. If you ADD any field to proto/stoc/stoc/
// params.proto, you MUST add a corresponding range/format check here AND
// extend the keeper-level MsgUpdateParams handler to call Validate() so that
// governance cannot install invalid values. ParamSetPairs() above is also a
// no-op for the same reason — keep both in sync when fields land.
func (p Params) Validate() error {
	return nil
}
