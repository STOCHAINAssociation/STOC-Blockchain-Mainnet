package stoc_test

import (
	"testing"

	keepertest "stoc/testutil/keeper"
	"stoc/testutil/nullify"
	stoc "stoc/x/stoc/module"
	"stoc/x/stoc/types"

	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),

		// this line is used by starport scaffolding # genesis/test/state
	}

	k, ctx := keepertest.StocKeeper(t)
	stoc.InitGenesis(ctx, k, genesisState)
	got := stoc.ExportGenesis(ctx, k)
	require.NotNil(t, got)

	nullify.Fill(&genesisState)
	nullify.Fill(got)

	// this line is used by starport scaffolding # genesis/test/assert
}
