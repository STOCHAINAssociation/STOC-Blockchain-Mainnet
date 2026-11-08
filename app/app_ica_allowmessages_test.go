package app

import (
	"testing"

	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
)

// TestDefaultICAGenesisDisabled pins the interchain-accounts default genesis
// (v5.0.3): host and controller disabled, host allowlist empty. It replaces
// the fix19 A16-APP-H1 12-message allowlist pin — STOChain no longer runs an
// ICA host at all, and the v5.0.3 upgrade handler applies the same values to
// mainnet/testnet state.
func TestDefaultICAGenesisDisabled(t *testing.T) {
	gs := defaultICAGenesis()

	if gs.HostGenesisState.Params.HostEnabled {
		t.Fatal("ICA host must be disabled in DefaultGenesis (v5.0.3)")
	}
	if n := len(gs.HostGenesisState.Params.AllowMessages); n != 0 {
		t.Fatalf("ICA host AllowMessages must be empty, got %d entries: %v", n, gs.HostGenesisState.Params.AllowMessages)
	}
	if gs.ControllerGenesisState.Params.ControllerEnabled {
		t.Fatal("ICA controller must be disabled in DefaultGenesis (v5.0.3)")
	}
	if err := gs.Validate(); err != nil {
		t.Fatalf("default ICA genesis must validate: %v", err)
	}
}

// TestDefaultIBCGenesisAllowedClients pins the IBC light-client allowlist
// (v5.0.3): only 07-tendermint. Solo machine (self-signed proofs) and
// localhost clients are not allowed.
func TestDefaultIBCGenesisAllowedClients(t *testing.T) {
	gs := defaultIBCGenesis()
	got := gs.ClientGenesis.Params.AllowedClients
	if len(got) != 1 || got[0] != ibcexported.Tendermint {
		t.Fatalf("IBC AllowedClients = %v, want [%s]", got, ibcexported.Tendermint)
	}
	for _, ct := range []string{ibcexported.Solomachine, ibcexported.Localhost, "*"} {
		if gs.ClientGenesis.Params.IsAllowedClient(ct) {
			t.Fatalf("client type %q must not be allowed", ct)
		}
	}
	if err := gs.Validate(); err != nil {
		t.Fatalf("default IBC genesis must validate: %v", err)
	}
}
