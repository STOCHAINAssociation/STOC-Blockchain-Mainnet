package mempool

import (
	"context"
	"errors"
	"time"

	cmtpubsub "github.com/cometbft/cometbft/libs/pubsub"
	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/cosmos/evm/rpc/stream"
)

// STOChain fix (audit M5): keep the EVM mempool subscribed to new blocks.
//
// The CometBFT pubsub server cancels a buffered subscription whose buffer is
// full (ErrOutOfCapacity) and never closes its Out() channel. The old loop
// ranged over Out() and so blocked forever after such a cancel: NotifyNewBlock
// never ran again, the latest context and base fee froze and the legacypool
// never reset, until the node restarted. The loop now watches Canceled() too
// and re-subscribes with backoff until the mempool or the event bus stops.

const (
	// newBlockEventsCapacity is the subscription buffer. Queued events are
	// coalesced (NotifyNewBlock always reads the latest committed state), so
	// a larger buffer only absorbs short stalls of the loop.
	newBlockEventsCapacity = 64
)

var (
	// eventBusCallTimeout bounds each Subscribe/Unsubscribe call.
	eventBusCallTimeout = 5 * time.Second

	// Backoff between re-subscribe attempts (variables for tests).
	eventBusResubscribeMinBackoff = 100 * time.Millisecond
	eventBusResubscribeMaxBackoff = 5 * time.Second
)

func subscribeNewBlocks(ctx context.Context, bus *cmttypes.EventBus) (cmttypes.Subscription, error) {
	sub, err := bus.Subscribe(ctx, SubscriberName, stream.NewBlockHeaderEvents, newBlockEventsCapacity)
	if errors.Is(err, cmtpubsub.ErrAlreadySubscribed) {
		// The pubsub server keeps listing a cancelled subscription as
		// subscribed until it is unsubscribed explicitly.
		_ = bus.Unsubscribe(ctx, SubscriberName, stream.NewBlockHeaderEvents)
		sub, err = bus.Subscribe(ctx, SubscriberName, stream.NewBlockHeaderEvents, newBlockEventsCapacity)
	}
	return sub, err
}

// stopNewBlockLoopLocked stops the current new-block loop and unsubscribes.
// m.eventMu must be held.
func (m *ExperimentalEVMMempool) stopNewBlockLoopLocked() error {
	if m.eventStop != nil {
		close(m.eventStop)
		m.eventStop = nil
	}
	bus := m.eventBus.Load()
	if bus == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), eventBusCallTimeout)
	defer cancel()
	err := bus.Unsubscribe(ctx, SubscriberName, stream.NewBlockHeaderEvents)
	if errors.Is(err, cmtpubsub.ErrSubscriptionNotFound) {
		return nil
	}
	return err
}

func isStopped(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

// runNewBlockLoop calls onNewBlock for new block events until stop is closed
// or the event bus shuts down. Events already queued are coalesced into one
// call. A subscription cancelled by the event bus (slow subscriber, or an
// unsubscribe this mempool did not ask for) is re-established, then
// onNewBlock runs once to catch up on the blocks missed meanwhile.
func (m *ExperimentalEVMMempool) runNewBlockLoop(bus *cmttypes.EventBus, sub cmttypes.Subscription, stop <-chan struct{}, onNewBlock func()) {
	for {
		select {
		case <-stop:
			return
		case <-sub.Out():
			drainNewBlockEvents(sub)
			onNewBlock()
		case <-sub.Canceled():
			reason := sub.Err()
			// nil reason: the pubsub server shut down (node stopping).
			if isStopped(stop) || reason == nil || !bus.IsRunning() {
				return
			}
			m.logger.Error("EVM mempool new-block subscription cancelled by the event bus, re-subscribing",
				"reason", reason)
			newSub := m.resubscribeNewBlocks(bus, stop)
			if newSub == nil {
				return
			}
			sub = newSub
			onNewBlock()
			m.eventResubscribes.Add(1)
		}
	}
}

func drainNewBlockEvents(sub cmttypes.Subscription) {
	for {
		select {
		case <-sub.Out():
		default:
			return
		}
	}
}

// resubscribeNewBlocks retries with exponential backoff until it succeeds,
// stop is closed or the event bus stops (then it returns nil).
func (m *ExperimentalEVMMempool) resubscribeNewBlocks(bus *cmttypes.EventBus, stop <-chan struct{}) cmttypes.Subscription {
	backoff := eventBusResubscribeMinBackoff
	for attempt := 1; ; attempt++ {
		if isStopped(stop) || !bus.IsRunning() {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), eventBusCallTimeout)
		sub, err := subscribeNewBlocks(ctx, bus)
		cancel()
		if err == nil {
			if isStopped(stop) {
				ctx, cancel := context.WithTimeout(context.Background(), eventBusCallTimeout)
				_ = bus.Unsubscribe(ctx, SubscriberName, stream.NewBlockHeaderEvents)
				cancel()
				return nil
			}
			m.logger.Info("EVM mempool re-subscribed to new-block events", "attempt", attempt)
			return sub
		}
		m.logger.Error("EVM mempool failed to re-subscribe to new-block events, retrying",
			"attempt", attempt, "backoff", backoff, "error", err)
		select {
		case <-stop:
			return nil
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > eventBusResubscribeMaxBackoff {
			backoff = eventBusResubscribeMaxBackoff
		}
	}
}
