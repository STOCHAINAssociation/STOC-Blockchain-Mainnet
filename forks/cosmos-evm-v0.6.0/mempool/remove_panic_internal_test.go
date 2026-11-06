package mempool

import (
	"errors"
	"math/big"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	evmtypes "github.com/cosmos/evm/x/vm/types"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"

	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmempool "github.com/cosmos/cosmos-sdk/types/mempool"
)

type fakeTx struct{ msgs []sdk.Msg }

func (f fakeTx) GetMsgs() []sdk.Msg                    { return f.msgs }
func (f fakeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

type panickingCosmosPool struct{ sdkmempool.ExtMempool }

func (panickingCosmosPool) Remove(sdk.Tx) error { panic("cosmos pool boom") }

func newRemoveTestMempool(t *testing.T) *ExperimentalEVMMempool {
	t.Helper()
	base := sdktestutil.DefaultContext(storetypes.NewKVStoreKey("rm"), storetypes.NewTransientStoreKey("rm_t"))
	ctx := base.WithBlockHeight(5).WithBlockHeader(cmtproto.Header{Height: 5})
	bc := NewBlockchain(func(int64, bool) (sdk.Context, error) { return ctx, nil }, log.NewNopLogger(), nil, nil, 100_000_000)
	bc.setLatestContext(ctx)
	return &ExperimentalEVMMempool{
		logger:     log.NewNopLogger(),
		blockchain: bc,
		// The ante fails for a non-nonce reason, so Remove wants to call
		// RemoveTx; legacyTxPool is nil, so RemoveTx panics.
		anteHandler: func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			return ctx, errors.New("insufficient funds")
		},
		cosmosPool: panickingCosmosPool{},
	}
}

// Review M1 (round 4): the whole EVM branch of Remove (ante check, RemoveTx,
// logging) is panic-safe and still returns nil.
func TestRemove_EVMBranchPanicSafe(t *testing.T) {
	m := newRemoveTestMempool(t)
	key, _ := crypto.GenerateKey()
	to := ethcommon.HexToAddress("0x000000000000000000000000000000000000dEaD")
	chainID := big.NewInt(9001)
	ethTx, err := ethtypes.SignNewTx(key, ethtypes.LatestSignerForChainID(chainID), &ethtypes.DynamicFeeTx{
		ChainID: chainID, Nonce: 0, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(1), Gas: 21000, To: &to,
	})
	require.NoError(t, err)
	msg := &evmtypes.MsgEthereumTx{}
	require.NoError(t, msg.FromSignedEthereumTx(ethTx, ethtypes.LatestSignerForChainID(chainID)))

	require.NotPanics(t, func() {
		require.NoError(t, m.Remove(fakeTx{msgs: []sdk.Msg{msg}}))
		require.NoError(t, m.Remove(fakeTx{msgs: []sdk.Msg{msg}}), "second panic at the same height")
	})
	require.Equal(t, int64(5), m.removePanicHeight.Load(), "panic logged for this height")
}

// The Cosmos branch is untouched: a panic there propagates exactly as before.
func TestRemove_CosmosBranchUnchanged(t *testing.T) {
	m := newRemoveTestMempool(t)
	cosmosMsg := &evmtypes.MsgUpdateParams{}
	require.PanicsWithValue(t, "cosmos pool boom", func() {
		_ = m.Remove(fakeTx{msgs: []sdk.Msg{cosmosMsg}})
	})
	// No messages: unchanged error.
	require.ErrorIs(t, m.Remove(fakeTx{}), ErrNoMessages)
}
