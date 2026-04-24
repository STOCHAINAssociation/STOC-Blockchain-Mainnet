package types

import "fmt"

// this line is used by starport scaffolding # genesis/types/import

// DefaultIndex is the default global index
const DefaultIndex uint64 = 1

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		// this line is used by starport scaffolding # genesis/types/default
		Params: DefaultParams(),
		Tokens: []Token{},
	}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	// this line is used by starport scaffolding # genesis/types/validate
	seenDenoms := make(map[string]bool, len(gs.Tokens))
	for _, token := range gs.Tokens {
		// Use ValidateState (not Validate) because exported genesis may contain
		// post-mutation state where TotalSupply < InitialSupply after burns
		if err := ValidateState(token); err != nil {
			return err
		}
		if seenDenoms[token.MinimalDenom] {
			return fmt.Errorf("duplicate token MinimalDenom: %s", token.MinimalDenom)
		}
		seenDenoms[token.MinimalDenom] = true
	}

	return gs.Params.Validate()
}
