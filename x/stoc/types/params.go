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
// Params currently carries no fields, so every instance passes validation.
// When a field is added (for example a gov-adjustable cap on tax percentage
// or token supply), this function must check bounds, address formats and
// any cross-field invariants before returning nil. Skipping that update
// would let a governance proposal persist out-of-range values that later
// crash consensus or silently disable enforcement.
func (p Params) Validate() error {
	return nil
}
