package backend

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	tmrpctypes "github.com/cometbft/cometbft/rpc/core/types"
	tmtypes "github.com/cometbft/cometbft/types"

	"github.com/cosmos/evm/config"
	"github.com/cosmos/evm/encoding"
	"github.com/cosmos/evm/rpc/backend/mocks"
	servertypes "github.com/cosmos/evm/server/types"
	"github.com/cosmos/evm/testutil/constants"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TestReceiptsFromCometBlock_IndexerBehind covers a just-committed block whose
// txs the EVM indexer has not stored yet: receipts must be derived from the
// block results instead of failing the whole eth_getBlockByNumber call.
func TestReceiptsFromCometBlock_IndexerBehind(t *testing.T) {
	// BuildTx needs the EVM coin info; same global setup as tx_info_pool_test.go
	_ = evmtypes.SetChainConfig(evmtypes.DefaultChainConfig(config.EighteenDecimalsChainID))
	_ = evmtypes.NewEVMConfigurator().WithEVMCoinInfo(config.ChainsCoinInfo[config.EighteenDecimalsChainID]).Configure()

	backend := setupMockBackend(t)
	height := int64(42)
	// the mock backend's codec only knows the base interfaces; decoding the block tx needs MsgEthereumTx
	encCfg := encoding.MakeConfig(constants.ExampleChainID.EVMChainID)
	evmtypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	backend.ClientCtx = backend.ClientCtx.WithTxConfig(encCfg.TxConfig).WithCodec(encCfg.Codec).WithInterfaceRegistry(encCfg.InterfaceRegistry)

	msg := buildMsgEthereumTx(t)
	cosmosTx, err := msg.BuildTx(backend.ClientCtx.TxConfig.NewTxBuilder(), "aatom")
	require.NoError(t, err)
	txBytes, err := backend.ClientCtx.TxConfig.TxEncoder()(cosmosTx)
	require.NoError(t, err)

	anyData := codectypes.UnsafePackAny(&evmtypes.MsgEthereumTxResponse{Hash: msg.Hash().Hex()})
	encodedData, err := backend.ClientCtx.Codec.Marshal(&sdk.TxMsgData{MsgResponses: []*codectypes.Any{anyData}})
	require.NoError(t, err)

	resBlock := &tmrpctypes.ResultBlock{Block: &tmtypes.Block{
		Header: tmtypes.Header{Height: height},
		Data:   tmtypes.Data{Txs: []tmtypes.Tx{txBytes}},
	}}
	blockRes := &tmrpctypes.ResultBlockResults{
		Height: height,
		TxsResults: []*abcitypes.ExecTxResult{{
			Code:    0,
			Data:    encodedData,
			GasUsed: 21000,
			Events: []abcitypes.Event{{Type: evmtypes.EventTypeEthereumTx, Attributes: []abcitypes.EventAttribute{
				{Key: evmtypes.AttributeKeyEthereumTxHash, Value: msg.Hash().Hex()},
				{Key: evmtypes.AttributeKeyTxIndex, Value: "0"},
				{Key: evmtypes.AttributeKeyTxGasUsed, Value: "21000"},
			}}},
		}},
	}

	// indexer knows nothing about this block yet
	backend.Indexer = &MockIndexer{txResults: map[common.Hash]*servertypes.TxResult{}}
	mockEVMQueryClient := backend.QueryClient.QueryClient.(*mocks.EVMQueryClient)
	mockEVMQueryClient.On("BaseFee", mock.Anything, mock.Anything).Return(&evmtypes.QueryBaseFeeResponse{}, nil)

	receipts, err := backend.ReceiptsFromCometBlock(resBlock, blockRes, []*evmtypes.MsgEthereumTx{msg})
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	require.Equal(t, msg.Hash(), receipts[0].TxHash)
	require.Equal(t, uint64(21000), receipts[0].GasUsed)
	require.Equal(t, uint64(21000), receipts[0].CumulativeGasUsed)
	require.Equal(t, uint(0), receipts[0].TransactionIndex)
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, receipts[0].Status)

	// a tx that is neither indexed nor in the block is still an error
	other := buildMsgEthereumTx(t)
	other.Raw.Transaction = ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 99, Gas: 21000})
	_, err = backend.ReceiptsFromCometBlock(resBlock, blockRes, []*evmtypes.MsgEthereumTx{other})
	require.ErrorContains(t, err, "tx not found")
}
