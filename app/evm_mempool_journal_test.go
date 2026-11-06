package app

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client/flags"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
)

// The EVM mempool journal is off unless app.toml turns it on, and it never
// lives under <home>/data.
func TestEVMMempoolJournalPath(t *testing.T) {
	home := t.TempDir()

	path, on := evmMempoolJournalPath(simtestutil.AppOptionsMap{flags.FlagHome: home})
	require.False(t, on, "default off")
	require.Empty(t, path)

	path, on = evmMempoolJournalPath(simtestutil.AppOptionsMap{flags.FlagHome: home, evmMempoolJournalFlag: false})
	require.False(t, on)
	require.Empty(t, path)

	path, on = evmMempoolJournalPath(simtestutil.AppOptionsMap{flags.FlagHome: home, evmMempoolJournalFlag: true})
	require.True(t, on)
	require.Equal(t, filepath.Join(home, "evm-mempool", "journal.rlp"), path)
	require.NotContains(t, path, filepath.Join(home, "data"))

	// String values from app.toml parse too.
	path, _ = evmMempoolJournalPath(simtestutil.AppOptionsMap{flags.FlagHome: home, evmMempoolJournalFlag: "true"})
	require.NotEmpty(t, path)

	// No home: disabled even when asked for.
	path, on = evmMempoolJournalPath(simtestutil.AppOptionsMap{evmMempoolJournalFlag: true})
	require.True(t, on)
	require.Empty(t, path)
}
