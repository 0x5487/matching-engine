package match

// The Publisher ownership boundary, asserted rather than described.
//
// Release recycles the logs it holds, so anything a Publisher keeps past the
// call reads recycled memory. That failure is silent - no error, no panic, just
// a log full of zeros - which is why it needs a test to keep it from being
// "simplified" back into a bug.
//
// The zeroing assertions below are not asserting on undefined behavior:
// releaseBookLog does `*log = OrderBookLog{}` explicitly, and these tests are
// single-goroutine, so the object cannot have been handed to anyone else.

import (
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

const (
	ownMarket = "BTC-USDT"
	ownPrice  = int64(200)
	ownSize   = int64(1)
	ownOrder  = "own-order-1"
)

// ownershipTestOrder builds a resting sell so the order produces exactly one open log.
func ownershipTestOrder(tb testing.TB, seq int64) *protocol.PlaceOrderRequest {
	tb.Helper()
	return &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			CommandID: "cmd-" + ownOrder,
			MarketID:  ownMarket,
			UserID:    1,
			Timestamp: seq,
		},
		OrderID:   ownOrder,
		OrderType: protocol.OrderTypeLimit,
		Side:      protocol.SideSell,
		Price:     udecimal.MustFromInt64(ownPrice, 0),
		Size:      udecimal.MustFromInt64(ownSize, 0),
	}
}

type copyingPublisher struct{ kept []OrderBookLog }

func (p *copyingPublisher) Publish(logs []*OrderBookLog) {
	for _, l := range logs {
		c := *l
		p.kept = append(p.kept, c)
	}
}

type retainingPublisher struct{ kept []*OrderBookLog }

func (p *retainingPublisher) Publish(logs []*OrderBookLog) { p.kept = logs }

// TestReleaseZeroesHeldLogs locks in the behavior that makes retention unsafe.
// If this ever starts failing because Release stopped zeroing, then a Publisher
// that retains logs has become correct again - but only by accident, and the
// pooling contract has changed with it.
func TestReleaseZeroesHeldLogs(t *testing.T) {
	book := NewOrderBook(ownMarket)

	batch, err := book.PlaceOrder(ownershipTestOrder(t, 1))
	require.NoError(t, err)
	require.Len(t, batch.Logs, 1)

	held := batch.Logs[0]
	require.Equal(t, ownOrder, held.OrderID, "precondition: log is populated before Release")
	require.Equal(t, protocol.LogTypeOpen, held.Type)

	batch.Release()

	assert.Equal(t, OrderBookLog{}, *held, "Release must zero every log it returns to the pool")
}

// TestCopyingPublisherKeepsIntactValues is the correct pattern: copy inside
// Publish, then the copy outlives Release.
func TestCopyingPublisherKeepsIntactValues(t *testing.T) {
	pub := &copyingPublisher{}
	book := NewOrderBook(ownMarket, WithPublisher(pub))

	batch, err := book.PlaceOrder(ownershipTestOrder(t, 2))
	require.NoError(t, err)

	// Exactly what the engine does: publish, then release, in that order.
	pub.Publish(batch.Logs)
	batch.Release()

	require.Len(t, pub.kept, 1)
	assert.Equal(t, ownOrder, pub.kept[0].OrderID)
	assert.Equal(t, protocol.LogTypeOpen, pub.kept[0].Type)
	assert.True(t, pub.kept[0].Price.Equal(udecimal.MustFromInt64(ownPrice, 0)))
}

// TestRetainingPublisherReadsRecycledMemory is the wrong pattern, pinned so
// the failure mode is visible rather than discovered in someone's market data
// feed. It reads zeros.
func TestRetainingPublisherReadsRecycledMemory(t *testing.T) {
	pub := &retainingPublisher{}
	book := NewOrderBook(ownMarket, WithPublisher(pub))

	batch, err := book.PlaceOrder(ownershipTestOrder(t, 3))
	require.NoError(t, err)

	pub.Publish(batch.Logs)
	batch.Release()

	require.Len(t, pub.kept, 1)
	assert.Equal(t, OrderBookLog{}, *pub.kept[0],
		"a Publisher that retains the slice must be documented as reading recycled memory")
}

// TestPublisherCannotRelease documents why a Publisher has no Release to
// call: the interface hands over the slice, not the LogBatch that owns it.
func TestPublisherCannotRelease(t *testing.T) {
	var pub Publisher = &copyingPublisher{}

	_, hasRelease := any(pub).(interface{ Release() })
	assert.False(t, hasRelease,
		"Publisher has no access to LogBatch.Release; if this ever becomes true the ownership story changed")
}
