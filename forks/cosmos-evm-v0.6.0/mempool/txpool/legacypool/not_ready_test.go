package legacypool

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/params"
)

// TestPoolWithoutState covers the restart window (N3): before the pool has
// chain state, the pending nonce must not panic and inserts must fail with
// ErrPoolNotReady, not "exceeds block gas limit".
func TestPoolWithoutState(t *testing.T) {
	blockchain := newTestBlockChain(params.TestChainConfig, 0, vm.StateDB(nil), new(event.Feed))
	pool := New(testTxPoolConfig, blockchain)
	if err := pool.Init(testTxPoolConfig.PriceLimit, blockchain.CurrentBlock(), newReserver()); err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if pool.HasState() {
		t.Fatal("pool must report no state")
	}
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)
	if n := pool.Nonce(addr); n != 0 {
		t.Fatalf("nonce without state: have %d, want 0", n)
	}
	errs := pool.Add([]*types.Transaction{pricedTransaction(0, 1_000_000, big.NewInt(1), key)}, true)
	if len(errs) != 1 || !errors.Is(errs[0], ErrPoolNotReady) {
		t.Fatalf("insert without state: have %v, want ErrPoolNotReady", errs)
	}
	if pending, queued := pool.Stats(); pending != 0 || queued != 0 {
		t.Fatalf("pool not empty: pending=%d queued=%d", pending, queued)
	}
	// A reset onto a head without state keeps the pool as it was (no crash).
	<-pool.requestReset(nil, &types.Header{Number: big.NewInt(5), GasLimit: 1_000_000})
	if pool.HasState() || pool.HeadNumber() != 0 {
		t.Fatal("reset onto a stateless head must be skipped")
	}
}
