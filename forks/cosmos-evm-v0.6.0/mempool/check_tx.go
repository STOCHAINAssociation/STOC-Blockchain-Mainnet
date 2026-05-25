package mempool

import (
	"errors"

	abci "github.com/cometbft/cometbft/abci/types"

	errorsmod "cosmossdk.io/errors"

	"github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// NewCheckTxHandler creates a CheckTx handler that integrates with the EVM mempool for transaction validation.
// It wraps the standard transaction execution flow to handle EVM-specific nonce gap errors by routing
// transactions with higher tx sequence numbers to the mempool for potential future execution.
// Returns a handler function that processes ABCI CheckTx requests and manages EVM transaction sequencing.
func NewCheckTxHandler(mempool *ExperimentalEVMMempool) types.CheckTxHandler {
	return func(runTx types.RunTx, request *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
		// STOChain fix 2026-05-25: drop orphan EVM txs on RecheckTx.
		// An EVM tx may live in the CometBFT mempool while legacypool has
		// already evicted it (cumulative balance reject after sender drain,
		// replacement-by-fee, lifetime evict, etc). Without an explicit drop
		// signal CometBFT keeps the tx forever — single-tx ante passes on
		// recheck but proposer's legacypool view rejects every block. Pool
		// observed growing 85 → 1579 over 2 days on devnet (2026-05-25).
		//
		// On Recheck, if the tx hash is gone from legacypool, return an error
		// CheckTx response so CometBFT removes it from its mempool too.
		if request.Type == abci.CheckTxType_Recheck && mempool.IsOrphanEVMTx(request.Tx) {
			return sdkerrors.ResponseCheckTxWithEvents(
				errorsmod.Wrap(sdkerrors.ErrInvalidRequest,
					"EVM tx orphan: dropped by legacypool (likely cumulative-balance reject)"),
				0, 0, nil, false), nil
		}

		gInfo, result, anteEvents, err := runTx(request.Tx, nil)
		if err != nil {
			// detect if there is a nonce gap error (only returned for EVM transactions)
			if errors.Is(err, ErrNonceGap) || errors.Is(err, ErrNonceLow) {
				// send it to the mempool for further triage
				err := mempool.InsertInvalidNonce(request.Tx)
				if err != nil {
					return sdkerrors.ResponseCheckTxWithEvents(err, gInfo.GasWanted, gInfo.GasUsed, anteEvents, false), nil
				}
			}
			// anything else, return regular error
			return sdkerrors.ResponseCheckTxWithEvents(err, gInfo.GasWanted, gInfo.GasUsed, anteEvents, false), nil
		}

		return &abci.ResponseCheckTx{
			GasWanted: int64(gInfo.GasWanted), // #nosec G115 -- this is copied from the Cosmos SDK
			GasUsed:   int64(gInfo.GasUsed),   // #nosec G115 -- this is copied from the Cosmos SDK
			Log:       result.Log,
			Data:      result.Data,
			Events:    types.MarkEventsToIndex(result.Events, nil),
		}, nil
	}
}
