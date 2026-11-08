package backend

import (
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/pkg/errors"

	"github.com/cometbft/cometbft/libs/bytes"

	rpctypes "github.com/cosmos/evm/rpc/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// GetCode returns the contract code at the given address and block number.
func (b *Backend) GetCode(address common.Address, blockNrOrHash rpctypes.BlockNumberOrHash) (hexutil.Bytes, error) {
	blockNum, err := b.BlockNumberFromComet(blockNrOrHash)
	if err != nil {
		return nil, err
	}

	req := &evmtypes.QueryCodeRequest{
		Address: address.String(),
	}

	res, err := b.QueryClient.Code(rpctypes.ContextWithHeight(blockNum.Int64()), req)
	if err != nil {
		return nil, err
	}

	return res.Code, nil
}

// GetProof returns an account object with proof and any storage proofs
func (b *Backend) GetProof(address common.Address, storageKeys []string, blockNrOrHash rpctypes.BlockNumberOrHash) (*rpctypes.AccountResult, error) {
	blockNum, err := b.BlockNumberFromComet(blockNrOrHash)
	if err != nil {
		return nil, err
	}

	height := int64(blockNum)

	_, err = b.CometHeaderByNumber(blockNum)
	if err != nil {
		// the error message imitates geth behavior
		return nil, errors.New("header not found")
	}

	// if the height is equal to zero, meaning the query condition of the block is either "pending" or "latest"
	if height == 0 {
		bn, err := b.BlockNumber()
		if err != nil {
			return nil, err
		}

		if bn > math.MaxInt64 {
			return nil, fmt.Errorf("not able to query block number greater than MaxInt64")
		}

		height = int64(bn) //#nosec G115 -- checked for int overflow already
	}

	ctx := rpctypes.ContextWithHeight(height)
	clientCtx := b.ClientCtx.WithHeight(height)

	// query storage proofs
	storageProofs := make([]rpctypes.StorageResult, len(storageKeys))

	for i, key := range storageKeys {
		hexKey := common.HexToHash(key)
		valueBz, proof, err := b.QueryClient.GetProof(clientCtx, evmtypes.StoreKey, evmtypes.StateKey(address, hexKey.Bytes()))
		if err != nil {
			return nil, err
		}

		storageProofs[i] = rpctypes.StorageResult{
			Key:   key,
			Value: (*hexutil.Big)(new(big.Int).SetBytes(valueBz)),
			Proof: GetHexProofs(proof),
		}
	}

	// query EVM account
	req := &evmtypes.QueryAccountRequest{
		Address: address.String(),
	}

	res, err := b.QueryClient.Account(ctx, req)
	if err != nil {
		return nil, err
	}

	// query account proofs
	accountKey := bytes.HexBytes(append(authtypes.AddressStoreKeyPrefix, address.Bytes()...))
	_, proof, err := b.QueryClient.GetProof(clientCtx, authtypes.StoreKey, accountKey)
	if err != nil {
		return nil, err
	}

	balance, ok := sdkmath.NewIntFromString(res.Balance)
	if !ok {
		return nil, errors.New("invalid balance")
	}

	return &rpctypes.AccountResult{
		Address:      address,
		AccountProof: GetHexProofs(proof),
		Balance:      (*hexutil.Big)(balance.BigInt()),
		CodeHash:     common.HexToHash(res.CodeHash),
		Nonce:        hexutil.Uint64(res.Nonce),
		StorageHash:  common.Hash{}, // NOTE: Cosmos EVM doesn't have a storage hash. TODO: implement?
		StorageProof: storageProofs,
	}, nil
}

// GetStorageAt returns the contract storage at the given address, block number, and key.
func (b *Backend) GetStorageAt(address common.Address, key string, blockNrOrHash rpctypes.BlockNumberOrHash) (hexutil.Bytes, error) {
	blockNum, err := b.BlockNumberFromComet(blockNrOrHash)
	if err != nil {
		return nil, err
	}

	req := &evmtypes.QueryStorageRequest{
		Address: address.String(),
		Key:     key,
	}

	res, err := b.QueryClient.Storage(rpctypes.ContextWithHeight(blockNum.Int64()), req)
	if err != nil {
		return nil, err
	}

	value := common.HexToHash(res.Value)
	return value.Bytes(), nil
}

// GetBalance returns the provided account's *spendable* balance up to the provided block number.
func (b *Backend) GetBalance(address common.Address, blockNrOrHash rpctypes.BlockNumberOrHash) (*hexutil.Big, error) {
	blockNum, err := b.BlockNumberFromComet(blockNrOrHash)
	if err != nil {
		return nil, err
	}

	req := &evmtypes.QueryBalanceRequest{
		Address: address.String(),
	}

	_, err = b.CometHeaderByNumber(blockNum)
	if err != nil {
		return nil, err
	}

	res, err := b.QueryClient.Balance(rpctypes.ContextWithHeight(blockNum.Int64()), req)
	if err != nil {
		return nil, err
	}

	val, ok := sdkmath.NewIntFromString(res.Balance)
	if !ok {
		return nil, errors.New("invalid balance")
	}

	// balance can only be negative in case of pruned node
	if val.IsNegative() {
		return nil, errors.New("couldn't fetch balance. Node state is pruned")
	}

	return (*hexutil.Big)(val.BigInt()), nil
}

// GetTransactionCount returns the number of transactions at the given address up to the given block number.
func (b *Backend) GetTransactionCount(address common.Address, blockNum rpctypes.BlockNumber) (*hexutil.Uint64, error) {
	n := hexutil.Uint64(0)
	bn, err := b.BlockNumber()
	if err != nil {
		return &n, err
	}
	height := blockNum.Int64()

	currentHeight := int64(bn) //#nosec G115 -- checked for int overflow already
	if height > currentHeight {
		return &n, errorsmod.Wrapf(
			sdkerrors.ErrInvalidHeight,
			"cannot query with height in the future (current: %d, queried: %d); please provide a valid height",
			currentHeight, height,
		)
	}
	// Get nonce (sequence) from account
	from := sdk.AccAddress(address.Bytes())
	accRet := b.ClientCtx.AccountRetriever

	err = accRet.EnsureExists(b.ClientCtx, from)
	if err != nil {
		// account doesn't exist yet, return 0
		return &n, nil
	}

	includePending := blockNum == rpctypes.EthPendingBlockNumber
	nonce, err := b.pendingNonceWithPool(address, includePending, blockNum.Int64())
	if err != nil {
		return nil, err
	}

	n = hexutil.Uint64(nonce)
	return &n, nil
}

// pendingNonceWithPool returns the next nonce for `address` honouring the
// STOChain v8 Bug C / SA-H25 fix: when the caller wants the pending nonce
// (i.e. the nonce the next outgoing tx should use), we take the max of the
// chain nonce, the chain nonce re-read after a head-advance race window, and
// the EVM txpool's PoolNonce. This is the canonical implementation; every
// site that previously called b.getAccountNonce(..., pending=true, ...) MUST
// call this helper instead so that the fix lands consistently on
// eth_getTransactionCount, SetTxDefaults (eth_sendTransaction /
// eth_estimateGas) and initAccessListTracer (eth_createAccessList).
//
// SA-2026-06-02 HIGH-3 (senior-skeptic audit): the prior implementation only
// patched eth_getTransactionCount, so dApps that signed and broadcast via
// eth_sendTransaction with no preceding nonce round-trip received the stale
// upstream nonce and hit "nonce too low" rejections. Extracting this helper
// is the load-bearing fix; the audit explicitly called this out as a HIGH
// because production dApps frequently use the fire-and-forget batched-send
// pattern.
//
// includePending=false reduces to the upstream getAccountNonce(false, ...)
// behaviour, so call sites that want only the chain-committed nonce can
// still use this helper with no behavioural change.
func (b *Backend) pendingNonceWithPool(address common.Address, includePending bool, height int64) (uint64, error) {
	nonce, err := b.getAccountNonce(address, includePending, height, b.Logger)
	if err != nil {
		return 0, err
	}
	if !includePending || b.Mempool == nil {
		return nonce, nil
	}
	pool := b.Mempool.GetTxPool()
	if pool == nil {
		return nonce, nil
	}
	// SA-H25 audit-2026-05-29: snapshot chain head BEFORE reading pool
	// nonce, then re-query chain nonce at that height to ensure a
	// consistent TOCTOU-safe pair. If a new block commits between the
	// two reads, the caller would otherwise receive a stale max
	// (chainNonce moved up, poolNonce stale) leading wallets to sign
	// with an already-used nonce → replay / rejection. Recheck after
	// pool read.
	poolNonce := pool.PoolNonce(address)
	if nonce2, err2 := b.getAccountNonce(address, false, height, b.Logger); err2 == nil && nonce2 > nonce {
		nonce = nonce2
	}
	if poolNonce > nonce {
		nonce = poolNonce
	}
	return nonce, nil
}
