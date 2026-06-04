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
	for _, token := range genState.Tokens {
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
