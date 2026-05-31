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
// SA-L2 audit-2026-05-29: intentionally a no-op because the Params struct
// currently has no fields. When a field is added (e.g. a gov-adjustable cap
// on tax percentage or token supply), this function MUST check bounds,
// address formats and cross-field invariants before returning nil. The
// keeper-level MsgUpdateParams handler must also be extended to call
// Validate() so governance cannot install out-of-range values that later
// crash consensus or silently disable enforcement. ParamSetPairs() above is
// a no-op for the same reason — keep both in sync when fields land.
func (p Params) Validate() error {
	return nil
}
