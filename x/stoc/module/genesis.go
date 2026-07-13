package stoc

import (
	"strconv"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"stoc/x/stoc/keeper"
	"stoc/x/stoc/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func InitGenesis(ctx sdk.Context, k keeper.Keeper, genState types.GenesisState) {
	// this line is used by starport scaffolding # genesis/module/init
	if err := k.SetParams(ctx, genState.Params); err != nil {
		panic(err)
	}

	// Initialize tokens and restore token counter from existing token IDs
	var maxCounter uint64
	for _, token := range genState.Tokens {
		if err := k.SetToken(ctx, token); err != nil {
			panic(err)
		}
		// Parse counter from minimalDenom (format: "SYMBOL_N")
		if idx := strings.LastIndex(token.MinimalDenom, "_"); idx >= 0 {
			if n, err := strconv.ParseUint(token.MinimalDenom[idx+1:], 10, 64); err == nil {
				// Saturate at max uint64 to prevent overflow; CreateToken will reject with counter overflow error
				next := n + 1
				if n == ^uint64(0) {
					next = n
				}
				if next > maxCounter {
					maxCounter = next
				}
			}
		}
	}
	// Restore counter so next CreateToken won't collide
	// If tokens exist but none had parseable _N suffix, panic to prevent counter collision.
	// This is safer than guessing — a wrong counter can cause duplicate minimalDenoms.
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
