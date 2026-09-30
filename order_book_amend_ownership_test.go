package match

// Amend moves a resting order into the book at a new price, and the engine
// implements that by running the order through the matching path. The logs from
// that inner match are then forwarded into the amend's own batch.
//
// That forwarding is why Amend is the one caller that cannot simply Release the
// inner batch: Release recycles logs, and by then they belong to the outer batch.
// The engine therefore hands back only the container.
//
// This test pins the ownership down from the caller's side, because that is the
// contract the rest of the codebase depends on: everything in the batch returned
// by AmendOrder stays valid until the caller releases it, and nothing the engine
// recycled internally is observable through it.

import (
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

const amendMarket = "BTC-USDT"

func amendPlace(
	tb testing.TB,
	id string,
	side protocol.Side,
	price, seq int64,
) *protocol.PlaceOrderRequest {
	tb.Helper()
	return &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			CommandID: "cmd-" + id,
			MarketID:  amendMarket,
			UserID:    1,
			Timestamp: seq,
		},
		OrderID:   id,
		OrderType: protocol.OrderTypeLimit,
		Side:      side,
		Price:     udecimal.MustFromInt64(price, 0),
		Size:      udecimal.MustFromInt64(1, 0),
	}
}

func amendOrder(tb testing.TB, id string, price, size int64, seq int64) *protocol.AmendOrderRequest {
	tb.Helper()
	return &protocol.AmendOrderRequest{
		BaseCommand: protocol.BaseCommand{
			CommandID: "cmd-" + id,
			MarketID:  amendMarket,
			UserID:    1,
			Timestamp: seq,
		},
		OrderID:  id,
		NewPrice: udecimal.MustFromInt64(price, 0),
		NewSize:  udecimal.MustFromInt64(size, 0),
	}
}

// TestAmendBatchOwnsEveryLogItReturns checks that an amend which crosses several
// levels hands back one intact batch: the amend log plus every match it produced.
func TestAmendBatchOwnsEveryLogItReturns(t *testing.T) {
	book := NewOrderBook(amendMarket)

	for i, price := range []int64{100, 101, 102} {
		batch, err := book.PlaceOrder(amendPlace(t, "ask-"+string(rune('a'+i)),
			protocol.SideSell, price, int64(10+i)))
		require.NoError(t, err)
		require.Len(t, batch.Logs, 1)
		batch.Release()
	}

	// A bid resting below the whole ladder.
	bid, err := book.PlaceOrder(amendPlace(t, "bid", protocol.SideBuy, 99, 20))
	require.NoError(t, err)
	require.Len(t, bid.Logs, 1)
	require.Equal(t, protocol.LogTypeOpen, bid.Logs[0].Type)
	bid.Release()

	// Amend it up through all three asks in one command.
	batch, err := book.AmendOrder(amendOrder(t, "bid", 103, 3, 21))
	require.NoError(t, err)

	var amends, matches, opens int
	for i := range batch.Logs {
		switch batch.Logs[i].Type {
		case protocol.LogTypeAmend:
			amends++
		case protocol.LogTypeMatch:
			matches++
		case protocol.LogTypeOpen:
			opens++
		default:
			t.Fatalf("unexpected log type in amend batch: %v", batch.Logs[i].Type)
		}
	}
	assert.Equal(t, 1, amends, "exactly one amend log")
	assert.Equal(t, 3, matches,
		"one match log per crossed level, all still intact after the engine recycled the inner batch")
	assert.Equal(t, 0, opens, "a fully crossing amend leaves nothing resting")

	// The amend log carries the pre-amend price, which only reads correctly if
	// the batch was never recycled underneath us.
	for i := range batch.Logs {
		if batch.Logs[i].Type == protocol.LogTypeAmend {
			assert.True(t, batch.Logs[i].OldPrice.Equal(udecimal.MustFromInt64(99, 0)),
				"amend log must still hold the old price at the time the caller sees it")
		}
	}

	batch.Release()
}

// TestBookStaysCorrectAfterAmend is the pool-integrity check. Amend recycles a
// batch container whose logs it has already forwarded, so a mistake there would
// hand the same log to two owners. The symptom would not be a wrong amend - it
// would be a later, unrelated order seeing a corrupted log.
func TestBookStaysCorrectAfterAmend(t *testing.T) {
	book := NewOrderBook(amendMarket)

	// Repeat enough times that a recycled container is very likely to be reused.
	for round := range 50 {
		base := int64(1000 + round*10)

		for lv := range 3 {
			batch, err := book.PlaceOrder(amendPlace(t,
				"ask-"+string(rune('a'+round))+string(rune('a'+lv)),
				protocol.SideSell, base+int64(lv), base+int64(lv)))
			require.NoError(t, err)
			require.Len(t, batch.Logs, 1, "round %d ask %d", round, lv)
			require.Equal(t, protocol.LogTypeOpen, batch.Logs[0].Type, "round %d ask %d", round, lv)
			batch.Release()
		}

		id := "bid-" + string(rune('a'+round))
		bid, err := book.PlaceOrder(amendPlace(t, id, protocol.SideBuy, base-1, base+5))
		require.NoError(t, err)
		bid.Release()

		batch, err := book.AmendOrder(amendOrder(t, id, base+3, 3, base+6))
		require.NoError(t, err)

		matches := 0
		for i := range batch.Logs {
			if batch.Logs[i].Type == protocol.LogTypeMatch {
				matches++
				assert.NotEmpty(t, batch.Logs[i].OrderID, "round %d: match log lost its order id", round)
				assert.False(t, batch.Logs[i].Price.IsZero(), "round %d: match log lost its price", round)
			}
		}
		assert.Equal(t, 3, matches, "round %d", round)
		batch.Release()
	}
}
