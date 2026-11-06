package app

import (
	"testing"

	"cosmossdk.io/x/feegrant"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	icatypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"

	stocmoduletypes "stoc/x/stoc/types"
)

// TestBlockedAddresses_NeverEmpty pins the security-critical shape of the
// blocklist (SA-AUDIT-2026-06-11 fix19 A16-APP-L2). BlockedAddresses()
// panics on an empty blockAccAddrs (SA-M5) — that panic path is dead code
// unless someone edits app_config.go; this test makes such an edit fail at
// PR time instead of at node boot.
func TestBlockedAddresses_NeverEmpty(t *testing.T) {
	blocked := BlockedAddresses()

	if len(blocked) < 6 {
		t.Fatalf("BlockedAddresses returned %d entries, want >= 6 — blockAccAddrs was trimmed, see SA-M5", len(blocked))
	}

	// govtypes is intentionally NOT blocked: gov must receive proposal
	// deposits. Blocking it would brick governance (SA-M5 rationale).
	if blocked[govtypes.ModuleName] {
		t.Fatalf("govtypes module account must NOT be blocked — gov deposits would fail (SA-M5)")
	}

	// Spot-pin the audit-driven entries so a refactor that rebuilds the
	// list from scratch cannot silently drop them.
	if !blocked[stocmoduletypes.ModuleName] {
		t.Fatal("x/stoc module account must be blocked (SA-C2: reserve-lock + SupplyInvariant chain-halt vector)")
	}
	if !blocked[feegrant.ModuleName] {
		t.Fatal("feegrant module account must be blocked (A16-APP-H2: tax-bypass corridor)")
	}
	// v5.0.3: lazily created module accounts must not be squattable by a
	// plain send (a squatted BaseAccount makes GetModuleAccount panic).
	if !blocked[ibctransfertypes.ModuleName] {
		t.Fatal("ibc transfer module account must be blocked (v5.0.3 module-account squatting)")
	}
	if !blocked[icatypes.ModuleName] {
		t.Fatal("interchainaccounts module account must be blocked (v5.0.3 module-account squatting)")
	}
}

// TestBlockedAddresses_EveryMaccExceptGov pins that every module account in
// the permission list is blocked, except gov (deposits). A new module account
// added to moduleAccPerms without a blocklist entry would be squattable again.
func TestBlockedAddresses_EveryMaccExceptGov(t *testing.T) {
	blocked := BlockedAddresses()
	for _, p := range moduleAccPerms {
		if p.Account == govtypes.ModuleName {
			continue
		}
		if !blocked[p.Account] {
			t.Fatalf("module account %q is in moduleAccPerms but not in blockAccAddrs", p.Account)
		}
	}
}
