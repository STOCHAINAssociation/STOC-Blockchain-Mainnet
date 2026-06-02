package mempool

import (
	"math/big"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"

	"github.com/cosmos/evm/mempool/miner"
	"github.com/cosmos/evm/mempool/txpool"
	msgtypes "github.com/cosmos/evm/x/vm/types"

	"cosmossdk.io/log"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/mempool"
)

var _ mempool.Iterator = &EVMMempoolIterator{}

// EVMMempoolIterator provides a unified iterator over both EVM and Cosmos transactions in the mempool.
// It implements priority-based transaction selection, choosing between EVM and Cosmos transactions
// based on their fee values. The iterator maintains state to track transaction types and ensures
// proper sequencing during block building.
type EVMMempoolIterator struct {
	/** Mempool Iterators **/
	evmIterator    *miner.TransactionsByPriceAndNonce
	cosmosIterator mempool.Iterator

	/** Utils **/
	logger   log.Logger
	txConfig client.TxConfig

	/** Chain Params **/
	bondDenom string
	chainID   *big.Int

	/** Blockchain Access **/
	blockchain *Blockchain

	// SA-H6 audit-2026-05-29: track which iterator actually produced the last
	// returned tx so advanceCurrentIterator can advance the right one even when
	// EVM conversion failed mid-iteration (would otherwise advance EVM but return
	// Cosmos, causing the same Cosmos tx to repeat → duplicate-tx ProcessProposal
	// reject).
	lastReturnedFromEVM bool
}

// NewEVMMempoolIterator creates a new unified iterator over EVM and Cosmos transactions.
// It combines iterators from both transaction pools and selects transactions based on fee priority.
// Returns nil if both iterators are empty or nil. The bondDenom parameter specifies the native
// token denomination for fee comparisons, and chainId is used for EVM transaction conversion.
func NewEVMMempoolIterator(evmIterator *miner.TransactionsByPriceAndNonce, cosmosIterator mempool.Iterator, logger log.Logger, txConfig client.TxConfig, bondDenom string, chainID *big.Int, blockchain *Blockchain) mempool.Iterator {
	// Check if we have any transactions at all
	hasEVM := evmIterator != nil && !evmIterator.Empty()
	hasCosmos := cosmosIterator != nil && cosmosIterator.Tx() != nil

	// Add the iterator name to the logger
	logger = logger.With(log.ModuleKey, "EVMMempoolIterator")

	if !hasEVM && !hasCosmos {
		logger.Debug("no transactions available in either mempool")
		return nil
	}

	return &EVMMempoolIterator{
		evmIterator:    evmIterator,
		cosmosIterator: cosmosIterator,
		logger:         logger,
		txConfig:       txConfig,
		bondDenom:      bondDenom,
		chainID:        chainID,
		blockchain:     blockchain,
	}
}

// Next advances the iterator to the next transaction and returns the updated iterator.
// It determines which iterator (EVM or Cosmos) provided the current transaction and advances
// that iterator accordingly. Returns nil when no more transactions are available.
func (i *EVMMempoolIterator) Next() mempool.Iterator {
	// Get next transactions on both iterators to determine which iterator to advance
	nextEVMTx, _ := i.getNextEVMTx()
	nextCosmosTx, _ := i.getNextCosmosTx()

	// If no transactions available, we're done
	if nextEVMTx == nil && nextCosmosTx == nil {
		i.logger.Debug("no more transactions available, ending iteration")
		return nil
	}

	i.logger.Debug("advancing to next transaction", "has_evm", nextEVMTx != nil, "has_cosmos", nextCosmosTx != nil)

	// Advance the iterator that provided the current transaction
	i.advanceCurrentIterator()

	// Check if we still have transactions after advancing
	if !i.hasMoreTransactions() {
		i.logger.Debug("no more transactions after advancing, ending iteration")
		return nil
	}

	return i
}

// Tx returns the current transaction from the iterator.
// It selects between EVM and Cosmos transactions based on fee priority
// and converts EVM transactions to SDK format.
func (i *EVMMempoolIterator) Tx() sdk.Tx {
	// Get current transactions from both iterators
	nextEVMTx, _ := i.getNextEVMTx()
	nextCosmosTx, _ := i.getNextCosmosTx()

	i.logger.Debug("getting current transaction", "has_evm", nextEVMTx != nil, "has_cosmos", nextCosmosTx != nil)

	// Return the preferred transaction based on fee priority
	tx := i.getPreferredTransaction(nextEVMTx, nextCosmosTx)

	if tx == nil {
		i.logger.Debug("no preferred transaction available")
	} else {
		i.logger.Debug("returning preferred transaction")
	}

	return tx
}

// STOChain v8 Bug B fix (2026-05-16): allow PrepareProposal handler to discard the
// entire sender-bucket from the EVM iterator when the current head tx fails ante.
// Upstream behaviour Shifts to the next nonce from the same account, which then
// also fails ante (nonce-too-high cascade), wasting iterations. Geth-style: Pop
// the account so other senders proceed in this block.
//
// PopCurrentAccount drops the current head's account from the underlying
// price-and-nonce heap. Safe to call when the iterator has selected an EVM tx
// and the caller has determined that tx (and all its account-mates) cannot mine
// in this block. No-op when current head is a Cosmos tx or iterator is empty.
//
// SA-2026-06-02 MED-8 (senior-skeptic audit): the prior implementation
// re-queried shouldUseEVM() at Pop time, which RE-EVALUATES the head
// selection. After Tx() falls back to the Cosmos lane because convertEVMToSDKTx
// failed (lastReturnedFromEVM=false), the EVM iterator head is still EVM and
// shouldUseEVM() may yet return true — Pop would then drop an innocent EVM
// bucket whose only sin was being aligned with a fee-matching Cosmos tx that
// already failed ante. We now consult the cached lastReturnedFromEVM that
// Tx() sets, which is the SAME signal the caller used to decide to call
// PopCurrentAccount in the first place. Pre-condition: callers MUST invoke
// Tx() before PopCurrentAccount (the existing PrepareProposal wrapper
// already does).
func (i *EVMMempoolIterator) PopCurrentAccount() {
	if i == nil || i.evmIterator == nil || i.evmIterator.Empty() {
		return
	}
	// Use the cached signal from the last Tx() call rather than re-querying
	// shouldUseEVM() — see MED-8 rationale above.
	if !i.lastReturnedFromEVM {
		return
	}
	i.evmIterator.Pop()
}

// CurrentIsEVM reports whether the most-recently-returned head transaction
// is from the EVM lane (true) or the Cosmos lane (false). Reads the cached
// lastReturnedFromEVM that Tx() sets. Used by custom PrepareProposal
// handlers to decide between PopCurrentAccount (EVM-side bucket pop) and
// standard Cosmos invalid-tx cleanup.
//
// SA-2026-06-02 MED-8: same rationale as PopCurrentAccount — relying on the
// cached field avoids the re-evaluation race when Tx()'s fallback path has
// switched lanes after a failed EVM-to-Cosmos conversion.
func (i *EVMMempoolIterator) CurrentIsEVM() bool {
	if i == nil {
		return false
	}
	return i.lastReturnedFromEVM
}

// =============================================================================
// UTILITY FUNCTIONS
// =============================================================================

// shouldUseEVM determines which transaction type to prioritize based on fee comparison.
// Returns true if the EVM transaction should be selected, false if Cosmos transaction should be used.
// EVM transactions will be prioritized in the following conditions:
// 1. Cosmos mempool has no transactions
// 2. EVM mempool has no transactions (fallback to Cosmos)
// 3. Cosmos transaction has no fee information
// 4. Cosmos transaction fee denomination doesn't match bond denom
// 5. Cosmos transaction fee is lower than the EVM transaction fee
// 6. Cosmos transaction fee overflows when converted to uint256
func (i *EVMMempoolIterator) shouldUseEVM() bool {
	// Get next transactions from both iterators
	nextEVMTx, evmFee := i.getNextEVMTx()
	nextCosmosTx, cosmosFee := i.getNextCosmosTx()

	// Handle cases where only one type is available
	if nextEVMTx == nil {
		i.logger.Debug("no EVM transaction available, preferring Cosmos")
		return false // Use Cosmos when no EVM transaction available
	}
	if nextCosmosTx == nil {
		i.logger.Debug("no Cosmos transaction available, preferring EVM")
		return true // Use EVM when no Cosmos transaction available
	}

	// Both have transactions - compare fees
	// cosmosFee can never be nil, but can be zero if no valid fee found
	if cosmosFee.IsZero() {
		i.logger.Debug("Cosmos transaction has no valid fee, preferring EVM", "evm_fee", evmFee.String())
		return true // Use EVM if Cosmos transaction has no valid fee
	}

	// Compare fees - prefer EVM unless Cosmos has higher fee
	cosmosHigher := cosmosFee.Gt(evmFee)
	i.logger.Debug("comparing transaction fees",
		"evm_fee", evmFee.String(),
		"cosmos_fee", cosmosFee.String())

	return !cosmosHigher
}

// getNextEVMTx retrieves the next EVM transaction and its fee
func (i *EVMMempoolIterator) getNextEVMTx() (*txpool.LazyTransaction, *uint256.Int) {
	if i.evmIterator == nil {
		return nil, nil
	}
	return i.evmIterator.Peek()
}

// getNextCosmosTx retrieves the next Cosmos transaction and its effective gas tip
func (i *EVMMempoolIterator) getNextCosmosTx() (sdk.Tx, *uint256.Int) {
	if i.cosmosIterator == nil {
		return nil, nil
	}

	tx := i.cosmosIterator.Tx()
	if tx == nil {
		return nil, nil
	}

	// Extract effective gas tip from the transaction (gas price - base fee)
	cosmosEffectiveTip := i.extractCosmosEffectiveTip(tx)
	if cosmosEffectiveTip == nil {
		return tx, uint256.NewInt(0) // Return zero fee if no valid fee found
	}

	return tx, cosmosEffectiveTip
}

// getPreferredTransaction returns the preferred transaction based on fee priority.
// Takes both transaction types as input and returns the preferred one, or nil if neither is available.
func (i *EVMMempoolIterator) getPreferredTransaction(nextEVMTx *txpool.LazyTransaction, nextCosmosTx sdk.Tx) sdk.Tx {
	// If no transactions available, return nil
	if nextEVMTx == nil && nextCosmosTx == nil {
		i.logger.Debug("no transactions available from either mempool")
		return nil
	}

	// Determine which transaction type to prioritize based on fee comparison
	useEVM := i.shouldUseEVM()

	if useEVM {
		i.logger.Debug("preferring EVM transaction based on fee comparison")
		// Prefer EVM transaction if available and convertible
		if nextEVMTx != nil {
			if evmTx := i.convertEVMToSDKTx(nextEVMTx); evmTx != nil {
				i.lastReturnedFromEVM = true
				return evmTx
			}
		}
		// Fall back to Cosmos if EVM is not available or conversion fails
		i.logger.Debug("EVM transaction conversion failed, falling back to Cosmos transaction")
		i.lastReturnedFromEVM = false
		return nextCosmosTx
	}

	// Prefer Cosmos transaction
	i.logger.Debug("preferring Cosmos transaction based on fee comparison")
	i.lastReturnedFromEVM = false
	return nextCosmosTx
}

// advanceCurrentIterator advances the appropriate iterator based on which transaction was used
func (i *EVMMempoolIterator) advanceCurrentIterator() {
	// SA-H6 audit-2026-05-29: use the cached `lastReturnedFromEVM` field rather
	// than re-querying shouldUseEVM(). On the EVM-conversion-fail fallback path
	// getPreferredTransaction returns Cosmos, but shouldUseEVM still reports EVM
	// (head unchanged) — re-querying advances the wrong iterator and the same
	// Cosmos tx repeats forever.
	useEVM := i.lastReturnedFromEVM

	if useEVM {
		i.logger.Debug("advancing EVM iterator")
		// We used EVM transaction, advance EVM iterator
		// NOTE: EVM transactions are automatically removed by the maintenance loop in the txpool
		// so we shift instead of popping
		if i.evmIterator != nil {
			i.evmIterator.Shift()
		} else {
			i.logger.Error("EVM iterator is nil but shouldUseEVM returned true")
		}
	} else {
		i.logger.Debug("advancing Cosmos iterator")
		// We used Cosmos transaction (or EVM failed), advance Cosmos iterator
		if i.cosmosIterator != nil {
			i.cosmosIterator = i.cosmosIterator.Next()
		} else {
			i.logger.Error("Cosmos iterator is nil but shouldUseEVM returned false")
		}
	}
}

// extractCosmosEffectiveTip extracts the effective gas tip from a Cosmos transaction
// This aligns with EVM transaction prioritization by calculating: gas_price - base_fee
func (i *EVMMempoolIterator) extractCosmosEffectiveTip(tx sdk.Tx) *uint256.Int {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		i.logger.Debug("Cosmos transaction doesn't implement FeeTx interface")
		return nil // Transaction doesn't implement FeeTx interface
	}

	bondDenomFeeAmount := math.ZeroInt()
	fees := feeTx.GetFee()
	for _, coin := range fees {
		if coin.Denom == i.bondDenom {
			i.logger.Debug("found fee in bond denomination", "denom", coin.Denom, "amount", coin.Amount.String())
			bondDenomFeeAmount = coin.Amount
		}
	}

	// Calculate gas price: fee_amount / gas_limit
	gasPrice, overflow := uint256.FromBig(bondDenomFeeAmount.Quo(math.NewIntFromUint64(feeTx.GetGas())).BigInt())
	if overflow {
		i.logger.Debug("overflowed on gas price calculation")
		return nil
	}

	// Get current base fee from blockchain StateDB
	baseFee := i.getCurrentBaseFee()
	if baseFee == nil {
		// No base fee, return gas price as effective tip
		i.logger.Debug("no base fee available, using gas price as effective tip", "gas_price", gasPrice.String())
		return gasPrice
	}

	// Calculate effective tip: gas_price - base_fee
	if gasPrice.Cmp(baseFee) < 0 {
		// Gas price is lower than base fee, return zero effective tip
		i.logger.Debug("gas price lower than base fee, effective tip is zero", "gas_price", gasPrice.String(), "base_fee", baseFee.String())
		return uint256.NewInt(0)
	}

	effectiveTip := new(uint256.Int).Sub(gasPrice, baseFee)
	i.logger.Debug("calculated effective tip", "gas_price", gasPrice.String(), "base_fee", baseFee.String(), "effective_tip", effectiveTip.String())
	return effectiveTip
}

// getCurrentBaseFee retrieves the current base fee from the blockchain StateDB
func (i *EVMMempoolIterator) getCurrentBaseFee() *uint256.Int {
	if i.blockchain == nil {
		i.logger.Debug("blockchain not available, cannot get base fee")
		return nil
	}

	// Get the current block header to access the base fee
	header := i.blockchain.CurrentBlock()
	if header == nil {
		i.logger.Debug("failed to get current block header")
		return nil
	}

	// Get base fee from the header
	baseFee := header.BaseFee
	if baseFee == nil {
		i.logger.Debug("no base fee in current block header")
		return nil
	}

	// Convert to uint256
	baseFeeUint, overflow := uint256.FromBig(baseFee)
	if overflow {
		i.logger.Debug("base fee overflow when converting to uint256")
		return nil
	}

	i.logger.Debug("retrieved current base fee from blockchain", "base_fee", baseFeeUint.String())
	return baseFeeUint
}

// hasMoreTransactions checks if there are more transactions available in either iterator
func (i *EVMMempoolIterator) hasMoreTransactions() bool {
	nextEVMTx, _ := i.getNextEVMTx()
	nextCosmosTx, _ := i.getNextCosmosTx()
	return nextEVMTx != nil || nextCosmosTx != nil
}

// convertEVMToSDKTx converts an Ethereum transaction to a Cosmos SDK transaction.
// It wraps the EVM transaction in a MsgEthereumTx and builds a proper SDK transaction
// using the configured transaction builder and bond denomination for fees.
func (i *EVMMempoolIterator) convertEVMToSDKTx(nextEVMTx *txpool.LazyTransaction) sdk.Tx {
	if nextEVMTx == nil {
		i.logger.Debug("EVM transaction is nil, skipping conversion")
		return nil
	}

	msgEthereumTx := &msgtypes.MsgEthereumTx{}
	hash := nextEVMTx.Tx.Hash()
	if err := msgEthereumTx.FromSignedEthereumTx(nextEVMTx.Tx, ethtypes.LatestSignerForChainID(i.chainID)); err != nil {
		i.logger.Error("failed to convert signed Ethereum transaction", "error", err, "tx_hash", hash)
		return nil // Return nil for invalid tx instead of panicking
	}

	cosmosTx, err := msgEthereumTx.BuildTx(i.txConfig.NewTxBuilder(), i.bondDenom)
	if err != nil {
		i.logger.Error("failed to build Cosmos transaction from EVM transaction", "error", err, "tx_hash", hash)
		return nil
	}

	i.logger.Debug("successfully converted EVM transaction to Cosmos transaction", "tx_hash", hash)
	return cosmosTx
}
