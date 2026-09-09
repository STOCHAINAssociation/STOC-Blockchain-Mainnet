package stoc

import (
	"fmt"
	"strconv"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"stoc/x/stoc/keeper"
	"stoc/x/stoc/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, genState types.GenesisState) {
	// this line is used by starport scaffolding # genesis/module/init
	// SA-2026-06-04 INFO-2 (comprehensive audit): run Params.Validate() at the
	// state-import boundary so a future field on Params (gov cap, address
	// list, ...) is bound-checked before SetParams persists it. The current
	// Params struct is field-less, so this is a no-op today; when fields
	// land the keeper-level MsgUpdateParams handler must also call
	// Validate() — see x/stoc/types/params.go godoc for the dual-update note.
	if err := genState.Params.Validate(); err != nil {
		panic(fmt.Sprintf("invalid genesis params: %v", err))
	}
	if err := k.SetParams(ctx, genState.Params); err != nil {
		panic(err)
	}

	// Initialize tokens and restore token counter from existing token IDs.
	// SA-2026-06-04 LOW-4 (comprehensive audit): track MinimalDenom uniqueness
	// across the genesis loop. SetToken is a plain map-write keyed by
	// MinimalDenom — two genesis entries with the same key would silently
	// overwrite each other, dropping the first token's TotalSupply and
	// Tax.RecipientAddress without any error surfaced to operators.
	seenMinimalDenoms := make(map[string]struct{}, len(genState.Tokens))
	var maxCounter uint64
	for i, token := range genState.Tokens {
		// SA-AUDIT-2026-06-10 fix16 A14-CROSS-M1: proto3 zero-value defense.
		// Reject empty MinimalDenom at genesis. Without this guard, a token
		// proto with the MinimalDenom field omitted (proto3 default = "")
		// installs a bank balance entry keyed by the empty string — a
		// collision landmine for any future denom that happens to coerce to
		// "" in iterator key-formation. SupplyInvariant break risk.
		if token.MinimalDenom == "" {
			panic(fmt.Sprintf("genesis token[%d] has empty MinimalDenom — refusing to import", i))
		}
		if _, exists := seenMinimalDenoms[token.MinimalDenom]; exists {
			panic(fmt.Sprintf("genesis contains duplicate MinimalDenom %q — token keys must be unique", token.MinimalDenom))
		}
		seenMinimalDenoms[token.MinimalDenom] = struct{}{}
		if err := k.SetToken(ctx, token); err != nil {
			panic(err)
		}
		// Parse counter from minimalDenom (format: "SYMBOL_N")
		if idx := strings.LastIndex(token.MinimalDenom, "_"); idx >= 0 {
			if n, err := strconv.ParseUint(token.MinimalDenom[idx+1:], 10, 64); err == nil {
				// SA-2026-06-02 MED-2 (senior-skeptic audit): a genesis token
				// whose MinimalDenom suffix is exactly ^uint64(0) (the
				// maximum uint64) would cause prior versions to silently
				// saturate the counter at ^uint64(0) and brick CreateToken
				// permanently — every subsequent MsgCreateToken would hit
				// the keeper's overflow check and revert. For a chain whose
				// raison d'être is tokenisation this is a denial-of-service
				// vector on the primary business feature. Reject at genesis
				// instead so the misconfiguration surfaces before the chain
				// boots, while still leaving headroom for ^uint64(0)-1
				// tokens to be created via the normal counter path.
				if n >= ^uint64(0)-1 {
					panic(fmt.Sprintf("genesis token %q has MinimalDenom counter %d, which would saturate the chain-wide token counter and brick MsgCreateToken (max allowed at genesis is %d)", token.MinimalDenom, n, ^uint64(0)-2))
				}
				next := n + 1
				if next > maxCounter {
					maxCounter = next
				}
			}
		}
	}
	// SA-2026-06-04 LOW-5 (comprehensive audit): the panic below is reachable
	// IFF a future change to SetToken / ValidateState ever loosens the
	// MinimalDenom format invariant. Today ValidateState's SA-H12 prefix
	// check + canonical-form check reject any token whose MinimalDenom
	// suffix is not a parseable uint64, so this branch never fires in
	// production. Kept intentionally as defense-in-depth: if a future
	// migration ever adds an alternative MinimalDenom shape (e.g. NFT
	// suffix), we want genesis to refuse to boot rather than silently
	// reset the token counter to zero and collide on the next CreateToken.
	if maxCounter == 0 && len(genState.Tokens) > 0 {
		panic("genesis contains tokens but none have parseable '_N' suffix in MinimalDenom — cannot safely reconstruct token counter")
	}
	if maxCounter > 0 {
		if err := k.SetTokenCounter(ctx, maxCounter); err != nil {
			panic(err)
		}
	}

	// SA-AUDIT-2026-06-11 fix19 A16-STO-L4: cross-check bank supply against
	// the module's tracked TotalSupply for every imported token. The runtime
	// invariant (bankSupply == token.TotalSupply, maintained by
	// CreateToken/MintTokens/BurnToken — see msg_server_burn_token.go godoc)
	// is deliberately non-halting at runtime per SA-H14 (drift emits
	// stoc_supply_drift instead of breaking consensus), but at the genesis
	// boundary there is no liveness to protect — a hand-edited or
	// mis-exported genesis whose books disagree should refuse to boot here
	// rather than run indefinitely with divergent accounting. O(N) bank
	// reads, genesis-boot only. Requires bank InitGenesis to run before
	// x/stoc (guaranteed by genesisModuleOrder in app_config.go).
	for _, token := range genState.Tokens {
		bankSupply := k.GetBankKeeper().GetSupply(ctx, token.MinimalDenom)
		if !bankSupply.Amount.Equal(token.TotalSupply) {
			panic(fmt.Sprintf(
				"genesis supply mismatch for token %q: bank supply %s != tracked TotalSupply %s — reconcile the genesis file before boot",
				token.MinimalDenom, bankSupply.Amount.String(), token.TotalSupply.String()))
		}
	}
}

// ExportGenesis returns the module's exported genesis.
// NOTE: Token counter is not in the protobuf GenesisState — it is reconstructed from
// token MinimalDenom suffixes in InitGenesis. The reconstruction is safe because
// CreateToken always generates denoms in "SYMBOL_N" format with monotonically increasing N.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	genesis := types.DefaultGenesis()
	genesis.Params = k.GetParams(ctx)

	// this line is used by starport scaffolding # genesis/module/export

	genesis.Tokens = k.GetAllTokens(ctx)

	return genesis
}
