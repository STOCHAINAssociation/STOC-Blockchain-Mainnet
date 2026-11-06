package app

import (
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
)

// Reproduces the real post-restart condition: after a restart BaseApp.Init
// leaves the check state at height 0, so CreateQueryContext(0) fails until
// the first new commit. latestCommittedContext must still return a read-only
// context at the last committed height, with its app hash and block time, so
// the EVM mempool can load its pool state before JSON-RPC serves.
func TestLatestCommittedContextAfterRestart(t *testing.T) {
	const chainID = "stoc_9001-1"
	db := dbm.NewMemDB()
	key := storetypes.NewKVStoreKey("test")
	open := func() *baseapp.BaseApp {
		bApp := baseapp.NewBaseApp("test", log.NewNopLogger(), db, nil, baseapp.SetChainID(chainID))
		bApp.MountStores(key)
		require.NoError(t, bApp.LoadLatestVersion())
		return bApp
	}

	running := open()
	_, err := running.InitChain(&abci.RequestInitChain{ChainId: chainID, InitialHeight: 1})
	require.NoError(t, err)
	blockTime := time.Unix(1_700_000_000, 0).UTC()
	for h := int64(1); h <= 2; h++ {
		_, err := running.FinalizeBlock(&abci.RequestFinalizeBlock{Height: h, Time: blockTime.Add(time.Duration(h) * time.Second)})
		require.NoError(t, err)
		_, err = running.Commit()
		require.NoError(t, err)
	}
	// While running, the regular query context works.
	_, err = running.CreateQueryContext(0, false)
	require.NoError(t, err)

	restarted := open()
	require.Equal(t, int64(2), restarted.LastBlockHeight())
	_, err = restarted.CreateQueryContext(0, false)
	require.Error(t, err, "regular query context fails right after a restart")

	ctx, err := latestCommittedContext(restarted)
	require.NoError(t, err)
	require.Equal(t, int64(2), ctx.BlockHeight())
	require.Equal(t, int64(2), ctx.BlockHeader().Height)
	require.Equal(t, restarted.LastCommitID().Hash, ctx.BlockHeader().AppHash)
	require.True(t, blockTime.Add(2*time.Second).Equal(ctx.BlockTime()), "block time of height 2")
	require.Equal(t, chainID, ctx.ChainID())

	// Read-only branch: writes to it never reach the committed store.
	ctx.KVStore(key).Set([]byte("k"), []byte("v"))
	again, err := latestCommittedContext(restarted)
	require.NoError(t, err)
	require.Nil(t, again.KVStore(key).Get([]byte("k")))
	require.Equal(t, int64(2), restarted.LastBlockHeight())

	// Nothing committed yet: no context.
	fresh := baseapp.NewBaseApp("test", log.NewNopLogger(), dbm.NewMemDB(), nil, baseapp.SetChainID(chainID))
	fresh.MountStores(storetypes.NewKVStoreKey("test"))
	require.NoError(t, fresh.LoadLatestVersion())
	_, err = latestCommittedContext(fresh)
	require.Error(t, err)
}
