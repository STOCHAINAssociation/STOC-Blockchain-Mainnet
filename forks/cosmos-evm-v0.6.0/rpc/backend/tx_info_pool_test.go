package backend

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"github.com/cosmos/evm/config"
	evmmempool "github.com/cosmos/evm/mempool"
	mempoolmocks "github.com/cosmos/evm/mempool/mocks"
	"github.com/cosmos/evm/x/vm/statedb"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"

	"github.com/cosmos/cosmos-sdk/client"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// newPoolOnlyMempool builds a real ExperimentalEVMMempool over mocked keepers.
func newPoolOnlyMempool(t *testing.T, txConfig client.TxConfig) *evmmempool.ExperimentalEVMMempool {
	t.Helper()
	// Global EVM config may already be set by another test in this binary.
	_ = evmtypes.SetChainConfig(evmtypes.DefaultChainConfig(config.EighteenDecimalsChainID))
	_ = evmtypes.NewEVMConfigurator().WithEVMCoinInfo(config.ChainsCoinInfo[config.EighteenDecimalsChainID]).Configure()

	vmKeeper := mempoolmocks.NewVMKeeper(t)
	feeMarketKeeper := mempoolmocks.NewFeeMarketKeeper(t)
	vmKeeper.On("GetBaseFee", mock.Anything).Return(big.NewInt(1_000_000_000)).Maybe()
	feeMarketKeeper.On("GetBlockGasWanted", mock.Anything).Return(uint64(0)).Maybe()
	vmKeeper.On("GetParams", mock.Anything).Return(evmtypes.DefaultParams()).Maybe()
	vmKeeper.On("GetAccount", mock.Anything, mock.Anything).Return(func(sdk.Context, common.Address) *statedb.Account {
		return &statedb.Account{Balance: uint256.NewInt(1_000_000_000_000_000_000), CodeHash: ethtypes.EmptyCodeHash.Bytes()}
	}).Maybe()
	vmKeeper.On("GetState", mock.Anything, mock.Anything, mock.Anything).Return(common.Hash{}).Maybe()
	vmKeeper.On("GetCode", mock.Anything, mock.Anything).Return([]byte{}).Maybe()
	vmKeeper.On("GetCodeHash", mock.Anything, mock.Anything).Return(ethtypes.EmptyCodeHash).Maybe()
	vmKeeper.On("KVStoreKeys").Return(make(map[string]*storetypes.KVStoreKey)).Maybe()
	vmKeeper.On("SetEvmMempool", mock.Anything).Return().Maybe()

	baseCtx := sdktestutil.DefaultContext(storetypes.NewKVStoreKey("test"), storetypes.NewTransientStoreKey("transient_test"))
	getCtx := func(int64, bool) (sdk.Context, error) {
		return baseCtx.
			WithBlockHeight(1).
			WithBlockHeader(cmtproto.Header{Height: 1, AppHash: []byte("00000000000000000000000000000000")}).
			WithEventManager(sdk.NewEventManager()), nil
	}
	cfg := &evmmempool.EVMMempoolConfig{
		BroadCastTxFn: func([]*ethtypes.Transaction) error { return nil },
		BlockGasLimit: 100_000_000,
	}
	mp := evmmempool.NewExperimentalEVMMempool(getCtx, log.NewNopLogger(), vmKeeper, feeMarketKeeper, txConfig, client.Context{}, cfg, 100)
	t.Cleanup(func() { _ = mp.Close() })
	return mp
}

// A tx held only by the legacypool (e.g. dropped from the CometBFT mempool on
// Recheck while fee-stuck) is returned as pending instead of null, without
// querying the CometBFT mempool.
func TestGetTransactionByHashPending_LegacyPoolOnly(t *testing.T) {
	backend := setupMockBackend(t)
	mp := newPoolOnlyMempool(t, backend.ClientCtx.TxConfig)
	backend.Mempool = mp

	chainID := evmtypes.GetEthChainConfig().ChainID
	key, _ := crypto.GenerateKey()
	to := common.HexToAddress("0x000000000000000000000000000000000000dEaD")
	tx, err := ethtypes.SignNewTx(key, ethtypes.LatestSignerForChainID(chainID), &ethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     0,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(1_000_000_000),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(0),
	})
	require.NoError(t, err)
	errs := mp.GetTxPool().Add([]*ethtypes.Transaction{tx}, true)
	require.NoError(t, errs[0])

	// The mocked CometBFT client has no UnconfirmedTxs expectation: reaching
	// it would fail the test.
	rpcTx, err := backend.GetTransactionByHashPending(tx.Hash())
	require.NoError(t, err)
	require.NotNil(t, rpcTx)
	require.Equal(t, tx.Hash(), rpcTx.Hash)
	require.Equal(t, crypto.PubkeyToAddress(key.PublicKey), rpcTx.From)
	require.Nil(t, rpcTx.BlockHash)
	require.Nil(t, rpcTx.BlockNumber)
	require.Equal(t, uint64(0), uint64(rpcTx.Nonce))
}
