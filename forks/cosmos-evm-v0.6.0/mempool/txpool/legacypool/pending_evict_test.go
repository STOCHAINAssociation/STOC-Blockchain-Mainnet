package legacypool

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/params"
)

// TestPendingNotEvictedRightAfterPromotion guards the pending-lifetime eviction
// loop against a missing heartbeat. promoteExecutables deletes beats[addr] as
// soon as the account's queue becomes empty, which is the normal state right
// after a fresh executable tx is promoted. The pending eviction loop then read
// a zero heartbeat, computed an age of ~2000 years and evicted the freshly
// promoted tx on the next eviction tick (<= 1 minute), long before Lifetime.
func TestPendingNotEvictedRightAfterPromotion(t *testing.T) {
	defer func(old time.Duration) { evictionInterval = old }(evictionInterval)
	evictionInterval = 50 * time.Millisecond

	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	blockchain := newTestBlockChain(params.TestChainConfig, 1000000, statedb, new(event.Feed))

	config := testTxPoolConfig
	config.Lifetime = time.Hour

	pool := New(config, blockchain)
	if err := pool.Init(config.PriceLimit, blockchain.CurrentBlock(), newReserver()); err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)
	testAddBalance(pool, addr, big.NewInt(1_000_000_000))

	tx := pricedTransaction(0, 100000, big.NewInt(1), key)
	if err := pool.addRemoteSync(tx); err != nil {
		t.Fatalf("failed to add tx: %v", err)
	}
	if pending, queued := pool.Stats(); pending != 1 || queued != 0 {
		t.Fatalf("after add: pending=%d queued=%d, want 1/0", pending, queued)
	}

	// Let several eviction ticks run. Lifetime is one hour, so nothing may go.
	time.Sleep(6 * evictionInterval)

	if pending, _ := pool.Stats(); pending != 1 {
		t.Fatalf("fresh pending tx evicted after %v (lifetime %v): pending=%d", 6*evictionInterval, config.Lifetime, pending)
	}
	if !pool.Has(tx.Hash()) {
		t.Fatal("fresh pending tx no longer in pool lookup")
	}
}

// TestPendingStillEvictedAfterLifetime keeps the original intent of the
// pending eviction patch: a pending bucket idle longer than Lifetime is dropped.
func TestPendingStillEvictedAfterLifetime(t *testing.T) {
	defer func(old time.Duration) { evictionInterval = old }(evictionInterval)
	evictionInterval = 50 * time.Millisecond

	statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
	blockchain := newTestBlockChain(params.TestChainConfig, 1000000, statedb, new(event.Feed))

	config := testTxPoolConfig
	config.Lifetime = 200 * time.Millisecond

	pool := New(config, blockchain)
	if err := pool.Init(config.PriceLimit, blockchain.CurrentBlock(), newReserver()); err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)
	testAddBalance(pool, addr, big.NewInt(1_000_000_000))

	if err := pool.addRemoteSync(pricedTransaction(0, 100000, big.NewInt(1), key)); err != nil {
		t.Fatalf("failed to add tx: %v", err)
	}
	time.Sleep(config.Lifetime + 6*evictionInterval)

	if pending, _ := pool.Stats(); pending != 0 {
		t.Fatalf("stuck pending tx not evicted after lifetime: pending=%d", pending)
	}
}

// TestPendingFeeStuckSkipUsesNextBaseFee covers audit finding L1: the SA-M8
// fee-stuck skip must treat a bucket as fee-stuck when its lowest-nonce fee
// cap is below the larger of the current base fee and the next-block base
// fee estimate (pool.priced.urgent.baseFee), not only the current one.
// It calls the eviction step directly (no ticker, no sleeps).
func TestPendingFeeStuckSkipUsesNextBaseFee(t *testing.T) {
	cases := []struct {
		name          string
		current, next int64
		age           time.Duration
		wantKept      bool
	}{
		// Rising fees: includable now, but not in the next block (the old
		// check evicted this bucket).
		{"next above fee cap", 50, 150, 2 * time.Hour, true},
		// Falling fees: the proposer still filters with the current one.
		{"current above fee cap", 150, 50, 2 * time.Hour, true},
		// Includable under both: definitively stuck, evict.
		{"both below fee cap", 50, 60, 2 * time.Hour, false},
		// The hard cap still applies to fee-stuck buckets.
		{"fee-stuck past hard cap", 50, 150, pendingHardEvictAfter + time.Minute, false},
		// Younger than Lifetime: never evicted.
		{"fresh", 50, 60, time.Minute, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			statedb, _ := state.New(types.EmptyRootHash, state.NewDatabaseForTesting())
			blockchain := newTestBlockChain(params.TestChainConfig, 1000000, statedb, new(event.Feed))

			config := testTxPoolConfig
			config.Lifetime = time.Hour

			pool := New(config, blockchain)
			if err := pool.Init(config.PriceLimit, blockchain.CurrentBlock(), newReserver()); err != nil {
				t.Fatal(err)
			}
			defer pool.Close()

			key, _ := crypto.GenerateKey()
			addr := crypto.PubkeyToAddress(key.PublicKey)
			testAddBalance(pool, addr, big.NewInt(1_000_000_000_000))

			// Fee cap 100.
			if err := pool.addRemoteSync(dynamicFeeTx(0, 100000, big.NewInt(100), big.NewInt(1), key)); err != nil {
				t.Fatalf("failed to add tx: %v", err)
			}
			pool.mu.Lock()
			pool.currentHead.Store(&types.Header{
				Number:   new(big.Int),
				GasLimit: 1000000,
				BaseFee:  big.NewInt(tc.current),
			})
			pool.priced.urgent.baseFee = big.NewInt(tc.next)
			pool.beats[addr] = time.Now().Add(-tc.age)
			pool.evictStalePendingLocked()
			pending, _ := pool.stats()
			pool.mu.Unlock()

			if tc.wantKept && pending != 1 {
				t.Fatalf("bucket evicted (current %d, next %d, fee cap 100, age %v): pending=%d", tc.current, tc.next, tc.age, pending)
			}
			if !tc.wantKept && pending != 0 {
				t.Fatalf("bucket not evicted (current %d, next %d, fee cap 100, age %v): pending=%d", tc.current, tc.next, tc.age, pending)
			}
		})
	}
}
