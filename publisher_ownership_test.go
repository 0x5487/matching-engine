package match

// The Publisher ownership boundary, asserted rather than described.
//
// A book reclaims the logs of a call when its next call starts, so anything a
// Publisher keeps past Publish reads recycled memory. That failure is silent -
// no error, no panic, just a log that now belongs to another call. Only a copy
// made inside Publish survives, which is what these tests pin.

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

// TestCopyingPublisherKeepsIntactValues is the correct pattern: copy inside
// Publish, then the copy outlives the call.
func TestCopyingPublisherKeepsIntactValues(t *testing.T) {
	pub := &copyingPublisher{}
	book := NewOrderBook(ownMarket, WithPublisher(pub))

	batch, err := book.PlaceOrder(ownershipTestOrder(t, 2))
	require.NoError(t, err)

	// Exactly what the engine does: publish the batch of this call.
	pub.Publish(batch.Logs)

	require.Len(t, pub.kept, 1)
	assert.Equal(t, ownOrder, pub.kept[0].OrderID)
	assert.Equal(t, protocol.LogTypeOpen, pub.kept[0].Type)
	assert.True(t, pub.kept[0].Price.Equal(udecimal.MustFromInt64(ownPrice, 0)))
}

// TestPublisherCannotRelease documents why a Publisher has no Release to
// call: the interface hands over the slice, not the LogBatch that owns it.
func TestPublisherCannotRelease(t *testing.T) {
	var pub Publisher = &copyingPublisher{}

	_, hasRelease := any(pub).(interface{ Release() })
	assert.False(
		t,
		hasRelease,
		"Publisher has no access to the LogBatch its call returned; if this ever becomes true the ownership story changed",
	)
}
