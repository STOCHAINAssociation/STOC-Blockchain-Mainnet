package mempool_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/stretchr/testify/require"

	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/cosmos/evm/mempool"
	"github.com/cosmos/evm/rpc/stream"
)

// End to end through SetEventBus (audit M5): after the event bus drops the
// mempool's subscription, new blocks still reach NotifyNewBlock.
func TestSetEventBus_NotifyNewBlockAfterResubscribe(t *testing.T) {
	h := newRecheckHarness(t)
	bus := cmttypes.NewEventBus()
	require.NoError(t, bus.Start())
	t.Cleanup(func() { _ = bus.Stop() })

	heads := make(chan core.ChainHeadEvent, 128)
	headSub := h.mp.GetBlockchain().SubscribeChainHeadEvent(heads)
	defer headSub.Unsubscribe()

	h.mp.SetEventBus(bus)
	require.True(t, h.mp.HasEventBus())

	drain := func() {
		for {
			select {
			case <-heads:
			default:
				return
			}
		}
	}
	waitHead := func() {
		t.Helper()
		drain()
		require.Eventually(t, func() bool {
			require.NoError(t, bus.PublishEventNewBlockHeader(cmttypes.EventDataNewBlockHeader{Header: cmttypes.Header{Height: 2}}))
			select {
			case <-heads:
				return true
			case <-time.After(20 * time.Millisecond):
				return false
			}
		}, 5*time.Second, 10*time.Millisecond, "NotifyNewBlock not called")
	}

	waitHead()
	require.NoError(t, bus.Unsubscribe(context.Background(), mempool.SubscriberName, stream.NewBlockHeaderEvents))
	waitHead()

	require.NoError(t, h.mp.Close())
	require.Equal(t, 0, bus.NumClientSubscriptions(mempool.SubscriberName))
	require.NoError(t, h.mp.Close(), "Close is idempotent")
}

// Review round 4, MUST: the pubsub loop can stall behind a slow unbuffered
// subscriber (tx indexer). SetEventBus must not panic when Subscribe times
// out; it subscribes in the background and catches up once it succeeds.
func TestSetEventBus_SubscribeStallDoesNotPanic(t *testing.T) {
	defer mempool.SetEventBusCallTimeout(50*time.Millisecond, 20*time.Millisecond)()
	h := newRecheckHarness(t)
	bus := cmttypes.NewEventBus()
	require.NoError(t, bus.Start())
	t.Cleanup(func() { _ = bus.Stop() })

	// Stall the pubsub loop: an unbuffered subscriber that does not read.
	stall, err := bus.SubscribeUnbuffered(context.Background(), "slow-indexer", stream.NewBlockHeaderEvents)
	require.NoError(t, err)
	require.NoError(t, bus.PublishEventNewBlockHeader(cmttypes.EventDataNewBlockHeader{Header: cmttypes.Header{Height: 2}}))

	heads := make(chan core.ChainHeadEvent, 128)
	headSub := h.mp.GetBlockchain().SubscribeChainHeadEvent(heads)
	defer headSub.Unsubscribe()

	require.NotPanics(t, func() { h.mp.SetEventBus(bus) })
	require.True(t, h.mp.HasEventBus())
	for len(heads) > 0 { // the startup prime's head
		<-heads
	}
	time.Sleep(150 * time.Millisecond) // several timed-out attempts
	require.Equal(t, 0, bus.NumClientSubscriptions(mempool.SubscriberName))

	// Unstall: the background subscribe succeeds and catches up once.
	<-stall.Out()
	require.Eventually(t, func() bool { return bus.NumClientSubscriptions(mempool.SubscriberName) == 1 },
		5*time.Second, 10*time.Millisecond)
	select {
	case <-heads:
	case <-time.After(5 * time.Second):
		t.Fatal("no catch-up NotifyNewBlock after the background subscribe")
	}
	require.NoError(t, bus.Unsubscribe(context.Background(), "slow-indexer", stream.NewBlockHeaderEvents))
	require.NoError(t, h.mp.Close())
}
