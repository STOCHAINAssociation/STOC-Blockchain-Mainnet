package mempool

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/cosmos/evm/rpc/stream"

	"cosmossdk.io/log"
)

func startTestEventBus(t *testing.T) *cmttypes.EventBus {
	t.Helper()
	bus := cmttypes.NewEventBus()
	require.NoError(t, bus.Start())
	t.Cleanup(func() {
		if bus.IsRunning() {
			_ = bus.Stop()
		}
	})
	return bus
}

func publishTestHeader(t *testing.T, bus *cmttypes.EventBus, height int64) {
	t.Helper()
	require.NoError(t, bus.PublishEventNewBlockHeader(cmttypes.EventDataNewBlockHeader{
		Header: cmttypes.Header{Height: height},
	}))
}

// startTestNewBlockLoop starts the loop like SetEventBus does, with a custom
// per-block hook. It returns the loop's done channel and a stop function.
func startTestNewBlockLoop(t *testing.T, m *ExperimentalEVMMempool, bus *cmttypes.EventBus, onNewBlock func()) (<-chan struct{}, func()) {
	t.Helper()
	m.eventMu.Lock()
	m.eventBus.Store(bus)
	sub, err := subscribeNewBlocks(context.Background(), bus)
	require.NoError(t, err)
	stop := make(chan struct{})
	m.eventStop = stop
	m.eventMu.Unlock()

	done := make(chan struct{})
	go func() {
		m.runNewBlockLoop(bus, sub, stop, onNewBlock)
		close(done)
	}()
	return done, func() {
		m.eventMu.Lock()
		_ = m.stopNewBlockLoopLocked()
		m.eventMu.Unlock()
		<-done
	}
}

// A slow subscriber gets cancelled by the pubsub server (ErrOutOfCapacity).
// The loop must re-subscribe and keep delivering new blocks.
func TestNewBlockLoop_ResubscribesAfterOutOfCapacity(t *testing.T) {
	bus := startTestEventBus(t)
	m := &ExperimentalEVMMempool{logger: log.NewNopLogger()}

	var calls atomic.Int64
	entered := make(chan struct{})
	release := make(chan struct{})
	var first atomic.Bool
	_, stop := startTestNewBlockLoop(t, m, bus, func() {
		calls.Add(1)
		if first.CompareAndSwap(false, true) {
			close(entered)
			<-release // stall like a slow NotifyNewBlock
		}
	})
	defer stop()

	// A probe subscriber tells when the pubsub server has dispatched events.
	probe, err := bus.Subscribe(context.Background(), "probe", stream.NewBlockHeaderEvents, 10_000)
	require.NoError(t, err)
	received := 0
	waitProbe := func(n int) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for received < n {
			select {
			case <-probe.Out():
				received++
			case <-deadline:
				t.Fatalf("probe got %d of %d events", received, n)
			}
		}
	}

	publishTestHeader(t, bus, 1)
	waitProbe(1)
	<-entered

	// Overflow the stalled subscription's buffer, then flush the server.
	overflow := newBlockEventsCapacity + 5
	for i := 0; i < overflow; i++ {
		publishTestHeader(t, bus, int64(2+i))
	}
	publishTestHeader(t, bus, 1000)
	waitProbe(1 + overflow + 1)
	require.Equal(t, int64(0), m.eventResubscribes.Load())

	close(release)
	require.Eventually(t, func() bool { return m.eventResubscribes.Load() == 1 },
		5*time.Second, 10*time.Millisecond, "loop must re-subscribe after the cancel")

	// New blocks reach the mempool again through the new subscription.
	before := calls.Load()
	require.Eventually(t, func() bool {
		publishTestHeader(t, bus, 2000)
		return calls.Load() > before
	}, 5*time.Second, 50*time.Millisecond)
	require.Equal(t, 1, bus.NumClientSubscriptions(SubscriberName))
}

// An unsubscribe the mempool did not ask for is treated like a cancel.
func TestNewBlockLoop_ResubscribesAfterForeignUnsubscribe(t *testing.T) {
	bus := startTestEventBus(t)
	m := &ExperimentalEVMMempool{logger: log.NewNopLogger()}
	var calls atomic.Int64
	_, stop := startTestNewBlockLoop(t, m, bus, func() { calls.Add(1) })
	defer stop()

	require.NoError(t, bus.Unsubscribe(context.Background(), SubscriberName, stream.NewBlockHeaderEvents))
	require.Eventually(t, func() bool { return m.eventResubscribes.Load() == 1 }, 5*time.Second, 10*time.Millisecond)

	before := calls.Load()
	require.Eventually(t, func() bool {
		publishTestHeader(t, bus, 1)
		return calls.Load() > before
	}, 5*time.Second, 50*time.Millisecond)
}

// Stopping the mempool or the event bus ends the loop without re-subscribing.
func TestNewBlockLoop_StopsCleanly(t *testing.T) {
	bus := startTestEventBus(t)
	m := &ExperimentalEVMMempool{logger: log.NewNopLogger()}
	_, stop := startTestNewBlockLoop(t, m, bus, func() {})
	stop()
	require.Equal(t, 0, bus.NumClientSubscriptions(SubscriberName))
	require.Equal(t, int64(0), m.eventResubscribes.Load())

	m2 := &ExperimentalEVMMempool{logger: log.NewNopLogger()}
	done, _ := startTestNewBlockLoop(t, m2, bus, func() {})
	require.NoError(t, bus.Stop())
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not exit after the event bus stopped")
	}
	require.Equal(t, int64(0), m2.eventResubscribes.Load())
}

// HasEventBus is called by x/vm EndBlock inside FinalizeBlock: it must never
// wait on eventMu (held by SetEventBus/Close).
func TestHasEventBus_LockFree(t *testing.T) {
	m := &ExperimentalEVMMempool{logger: log.NewNopLogger()}
	m.eventBus.Store(cmttypes.NewEventBus())
	m.eventMu.Lock()
	defer m.eventMu.Unlock()
	got := make(chan bool, 1)
	go func() { got <- m.HasEventBus() }()
	select {
	case v := <-got:
		require.True(t, v)
	case <-time.After(time.Second):
		t.Fatal("HasEventBus blocked on eventMu")
	}
}
