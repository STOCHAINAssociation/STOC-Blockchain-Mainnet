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
				if n+1 > maxCounter {
					maxCounter = n + 1
				}
			}
		}
	}
	// Restore counter so next CreateToken won't collide
	// Fallback: if tokens exist but none had parseable _N suffix, use token count
	if maxCounter == 0 && len(genState.Tokens) > 0 {
		maxCounter = uint64(len(genState.Tokens))
	}
	if maxCounter > 0 {
		if err := k.SetTokenCounter(ctx, maxCounter); err != nil {
			panic(err)
		}
	}
}

// ExportGenesis returns the module's exported genesis.
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	genesis := types.DefaultGenesis()
	genesis.Params = k.GetParams(ctx)

	// this line is used by starport scaffolding # genesis/module/export

	//export tokens
	genesis.Tokens = k.GetAllTokens(ctx)

	return genesis
}
