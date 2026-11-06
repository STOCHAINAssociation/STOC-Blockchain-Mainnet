package mempool_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"

	evmmempool "github.com/cosmos/evm/mempool"
	"github.com/cosmos/evm/mempool/txpool"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func runTxReturning(err error) sdk.RunTx {
	return func(_ []byte, _ sdk.Tx) (sdk.GasInfo, *sdk.Result, []abci.Event, error) {
		return sdk.GasInfo{GasWanted: 21000}, nil, nil, err
	}
}

// A tx already held by the legacypool (e.g. promoted from a nonce gap and
// re-broadcast) must be accepted so CometBFT stores and gossips it.
func TestCheckTxHandler_AlreadyKnownIsAccepted(t *testing.T) {
	handler := evmmempool.NewCheckTxHandler(nil)

	res, err := handler(runTxReturning(fmt.Errorf("insert: %w", txpool.ErrAlreadyKnown)), &abci.RequestCheckTx{
		Tx:   []byte("tx"),
		Type: abci.CheckTxType_New,
	})
	require.NoError(t, err)
	require.Equal(t, uint32(0), res.Code, res.Log)
	require.Equal(t, int64(21000), res.GasWanted)
}

// Any other error is still returned to CometBFT.
func TestCheckTxHandler_OtherErrorRejected(t *testing.T) {
	handler := evmmempool.NewCheckTxHandler(nil)

	res, err := handler(runTxReturning(errors.New("boom")), &abci.RequestCheckTx{
		Tx:   []byte("tx"),
		Type: abci.CheckTxType_New,
	})
	require.NoError(t, err)
	require.NotEqual(t, uint32(0), res.Code)
}
