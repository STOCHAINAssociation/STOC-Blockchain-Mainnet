package types

// DefaultGenesisState returns the default genesis state
func DefaultGenesisState() *GenesisState {
	return &GenesisState{}
}

// Validate performs basic genesis state validation returning an error upon any failure.
// NOTE: Intentionally empty — GenesisState has no configurable fields.
// If fields are added to the proto definition, update this method accordingly.
func (gs *GenesisState) Validate() error {
	return nil
}
