package match

import (
	"encoding/json"
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

// A price has one level no matter how it was written: 100 and 100.00 are the same
// price to the skiplist, so the book must hold them as one level on both sides.

func TestEqualPriceWrittenDifferentlyRestsOnOneLevel(t *testing.T) {
	t.Parallel()

	t.Run("asks: 100 then 100.00 is one level holding both orders, and a taker fills both", func(t *testing.T) {
		t.Parallel()
		book := newWalkBook()
		walkRest(t, book, "ask-a", Sell, "100", "1", "0")
		walkRest(t, book, "ask-b", Sell, "100.00", "2", "0")

		depth := book.GetDepth(10)
		require.Len(t, depth.Asks, 1, "one price level")
		require.Equal(t, "3", depth.Asks[0].Size)
		require.Equal(t, int64(2), depth.Asks[0].Count)
		require.Len(t, book.Snapshot().Asks, 2, "both orders are in the snapshot")

		batch := walkPlace(t, book, &protocol.PlaceOrderRequest{
			OrderID: "taker", OrderType: Limit, Side: Buy,
			Price: udecimal.MustParse("100"), Size: udecimal.MustParse("3"),
		})
		requireMakerFills(t, batch, []string{"ask-a", "ask-b"}, []string{"1", "2"})
		require.Empty(t, book.GetDepth(10).Asks, "the level is gone after both fills")
	})

	t.Run("bids: 100 then 100.00 is one level holding both orders, and a taker fills both", func(t *testing.T) {
		t.Parallel()
		book := newWalkBook()
		walkRest(t, book, "bid-a", Buy, "100", "1", "0")
		walkRest(t, book, "bid-b", Buy, "100.00", "2", "0")

		depth := book.GetDepth(10)
		require.Len(t, depth.Bids, 1, "one price level")
		require.Equal(t, "3", depth.Bids[0].Size)
		require.Len(t, book.Snapshot().Bids, 2, "both orders are in the snapshot")

		batch := walkPlace(t, book, &protocol.PlaceOrderRequest{
			OrderID: "taker", OrderType: Limit, Side: Sell,
			Price: udecimal.MustParse("100"), Size: udecimal.MustParse("3"),
		})
		requireMakerFills(t, batch, []string{"bid-a", "bid-b"}, []string{"1", "2"})
		require.Empty(t, book.GetDepth(10).Bids, "the level is gone after both fills")
	})

	t.Run("bids: canceling the second order leaves the first at the level", func(t *testing.T) {
		t.Parallel()
		book := newWalkBook()
		walkRest(t, book, "bid-a", Buy, "100", "1", "0")
		walkRest(t, book, "bid-b", Buy, "100.00", "2", "0")

		batch, err := book.CancelOrder(&protocol.CancelOrderRequest{
			BaseCommand: protocol.BaseCommand{UserID: 1, CommandID: "cancel-b", MarketID: book.marketID, Timestamp: 2},
			OrderID:     "bid-b",
		})
		require.NoError(t, err)
		require.Len(t, batch.Logs, 1)
		require.Equal(t, protocol.LogTypeCancel, batch.Logs[0].Type)

		depth := book.GetDepth(10)
		require.Len(t, depth.Bids, 1)
		require.Equal(t, "1", depth.Bids[0].Size)
		require.Equal(t, int64(1), depth.Bids[0].Count)
	})

	t.Run("an amend moving an order between 100 and 100.000 keeps one level", func(t *testing.T) {
		t.Parallel()
		book := newWalkBook()
		walkRest(t, book, "bid-a", Buy, "100", "1", "0")
		walkRest(t, book, "bid-b", Buy, "100.0", "2", "0")

		// Same price written differently with a larger size: the order loses its place
		// and re-rests at the price it already had, joining the same level.
		amend(t, book, "bid-a", "100.000", "5")
		depth := book.GetDepth(10)
		require.Len(t, depth.Bids, 1, "still one level")
		require.Equal(t, "7", depth.Bids[0].Size)
		require.Equal(t, int64(2), depth.Bids[0].Count)

		// Move bid-b away and back, writing the price differently the second time.
		amend(t, book, "bid-b", "101", "")
		require.Len(t, book.GetDepth(10).Bids, 2)
		amend(t, book, "bid-b", "100.00", "")
		depth = book.GetDepth(10)
		require.Len(t, depth.Bids, 1, "returning to 100.00 joins the level at 100")
		require.Equal(t, "7", depth.Bids[0].Size)
		require.Equal(t, int64(2), depth.Bids[0].Count)
	})
}

func TestEqualPriceQueriesReadTheSameLevel(t *testing.T) {
	t.Parallel()

	book := newWalkBook()
	walkRest(t, book, "ask-a", Sell, "100", "1", "0")
	walkRest(t, book, "ask-b", Sell, "100.00", "2", "0")
	walkRest(t, book, "bid-a", Buy, "0.5", "4", "0")

	assert.Equal(t, "3", book.LevelSize(Sell, udecimal.MustParse("100.000")).String(),
		"LevelSize with a longer written price reads the whole level")
	assert.Equal(t, "3", book.LevelSize(Sell, udecimal.MustParse("100")).String())
	assert.Equal(t, "4", book.LevelSize(Buy, udecimal.MustParse("0.50")).String())
	assert.Equal(t, "0", book.LevelSize(Buy, udecimal.MustParse("0.5000001")).String(),
		"a price that is not on the book reads zero")

	best, ok := book.BestPrice(Sell)
	require.True(t, ok)
	assert.True(t, best.Equal(udecimal.MustParse("100")), "best ask is 100, got %s", best)
}

// TestDepthDeltaOneLevelForEqualPrices covers the depth delta and the follower
// side: the builder reports one level for equal prices, ApplyDelta treats a
// differently written price as the same level, and a real duplicate on one side is
// still refused by both ApplyDelta and ApplySnapshot.
func TestDepthDeltaOneLevelForEqualPrices(t *testing.T) {
	t.Parallel()

	t.Run("builder reports one level at the sum of both orders", func(t *testing.T) {
		t.Parallel()
		book := newWalkBook()
		walkRest(t, book, "ask-a", Sell, "100", "1", "0")
		walkRest(t, book, "ask-b", Sell, "100.00", "2", "0")

		builder := NewDepthDeltaBuilder()
		builder.Add(bookLogOf(t, book, "ask-a"))
		builder.Add(bookLogOf(t, book, "ask-b"))
		var d protocol.DepthDelta
		require.True(t, builder.Flush(book, &d))
		require.Len(t, d.Asks, 1, "one level, not one per written price")
		require.Equal(t, "3", d.Asks[0].Size.String())
	})

	t.Run("ApplyDelta with a differently written price updates the same level", func(t *testing.T) {
		t.Parallel()
		ab := NewAggregatedBook()
		require.NoError(t, ab.ApplyDelta(&protocol.DepthDelta{Seq: 1, Asks: []protocol.DepthLevel{lvl("100", "1")}}))
		require.NoError(t, ab.ApplyDelta(&protocol.DepthDelta{Seq: 2, Asks: []protocol.DepthLevel{lvl("100.00", "5")}}))
		requireDeltaBookState(t, ab, 2, []string{"100@5"}, nil)
		require.NoError(
			t,
			ab.ApplyDelta(&protocol.DepthDelta{Seq: 3, Asks: []protocol.DepthLevel{lvl("100.000", "0")}}),
		)
		requireDeltaBookState(t, ab, 3, nil, nil)
	})

	t.Run("a duplicate written two ways is refused by ApplyDelta and ApplySnapshot", func(t *testing.T) {
		t.Parallel()
		ab := NewAggregatedBook()
		err := ab.ApplyDelta(&protocol.DepthDelta{
			Seq:  1,
			Bids: []protocol.DepthLevel{lvl("100", "1"), lvl("100.00", "2")},
		})
		require.ErrorIs(t, err, ErrInvalidDepthDelta)
		requireDeltaBookState(t, ab, 0, nil, nil)

		err = ab.ApplySnapshot(&protocol.DepthDelta{
			Seq:  5,
			Asks: []protocol.DepthLevel{lvl("101", "1"), lvl("101.0", "2")},
		})
		require.ErrorIs(t, err, ErrInvalidDepthDelta)
		requireDeltaBookState(t, ab, 0, nil, nil)
	})
}

// TestUserEventIsIgnoredByDepth: a user event carries the upstream sequence in
// no depth version; the depth builder must not take it for a change.
func TestUserEventIsIgnoredByDepth(t *testing.T) {
	t.Parallel()

	book := newWalkBook()
	walkRest(t, book, "ask-a", Sell, "100", "1", "0")
	user := NewUserEventLog("cmd-user", "default", 1, "account.updated", "k", nil, 3)
	require.Equal(t, uint64(0), user.SeqID)
	raw, err := json.Marshal(user)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "cmd_seq_id")

	builder := NewDepthDeltaBuilder()
	builder.Add(user)
	var d protocol.DepthDelta
	require.False(t, builder.Flush(book, &d), "a user event moves no depth")
	require.Zero(t, d.Seq)
}

// TestAmendDepthRuleIsOnePlace covers each kind of amend through the one rule
// CalculateDepthChange applies: the visible part removed from OldPrice. A follower
// fed only the delta of each command ends where the book ends.
func TestAmendDepthRuleIsOnePlace(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		newPrice  string
		newSize   string
		wantAsks  []string
		wantMoved bool
	}{
		{
			name:     "in place inside the hidden size moves no depth",
			newSize:  "4",
			wantAsks: []string{"100@2"},
		},
		{
			name:     "in place past the hidden size removes the visible part of the decrease",
			newSize:  "1",
			wantAsks: []string{"100@1"},
		},
		{
			name:     "losing priority at a new price removes the whole visible part at the old price",
			newPrice: "101",
			wantAsks: []string{"101@2"},
		},
		{
			name:     "losing priority with a shrink and a new price removes at the old price, not the new one",
			newPrice: "101",
			newSize:  "3",
			wantAsks: []string{"101@2"},
		},
		{
			name:     "growing in place loses priority and re-matches at the same price",
			newSize:  "9",
			wantAsks: []string{"100@2"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			book := newWalkBook()
			walkRest(t, book, "ice", Sell, "100", "5", "2")
			follower := followerOf(t, book)

			batch, err := book.AmendOrder(&protocol.AmendOrderRequest{
				BaseCommand: protocol.BaseCommand{UserID: 1, CommandID: "amend", MarketID: book.marketID, Timestamp: 2},
				OrderID:     "ice",
				NewPrice:    udecimal.MustParse(orZero(tc.newPrice)),
				NewSize:     udecimal.MustParse(orZero(tc.newSize)),
			})
			require.NoError(t, err)
			require.NotEmpty(t, batch.Logs)
			followCommand(t, book, follower, batch)

			requireSameDepth(t, book, follower)
			got := dlLevels(book.GetDepth(10).Asks)
			require.Equal(t, tc.wantAsks, got)
		})
	}
}

// orZero returns s, or "0" when s is empty: an amend field left out keeps the order's value.
func orZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

// requireMakerFills asserts a batch of only match logs, one per maker in order, each
// filling the given size.
func requireMakerFills(t *testing.T, batch *LogBatch, makers, sizes []string) {
	t.Helper()
	require.Len(t, batch.Logs, len(makers), "one match per maker, no open or reject")
	for i, log := range batch.Logs {
		require.Equal(t, protocol.LogTypeMatch, log.Type, "log %d", i)
		require.Equal(t, makers[i], log.MakerOrder.ID, "log %d maker", i)
		require.Equal(t, sizes[i], log.MatchSize.String(), "log %d size", i)
	}
}

// amend sends an amend that changes the price and size named (empty keeps them) and
// requires the amend to be accepted.
func amend(t *testing.T, book *OrderBook, orderID, newPrice, newSize string) {
	t.Helper()
	req := &protocol.AmendOrderRequest{
		BaseCommand: protocol.BaseCommand{
			UserID:    1,
			CommandID: "amend-" + orderID,
			MarketID:  book.marketID,
			Timestamp: 2,
		},
		OrderID:  orderID,
		NewPrice: udecimal.MustParse(orZero(newPrice)),
		NewSize:  udecimal.MustParse(orZero(newSize)),
	}
	batch, err := book.AmendOrder(req)
	require.NoError(t, err)
	require.NotEmpty(t, batch.Logs)
}

// bookLogOf returns a copy of the open log that resting orderID produced.
func bookLogOf(t *testing.T, book *OrderBook, orderID string) *OrderBookLog {
	t.Helper()
	order, ok := book.findOrder(orderID)
	require.True(t, ok, "order %s rests on the book", orderID)
	log := NewOpenLog(book.SeqID(), "log-"+orderID, book.engineID, book.marketID,
		order.ID, order.UserID, order.Side, order.Price, order.Size, order.Size, order.Type, 1)
	return log
}

// followerOf returns an AggregatedBook holding book's levels at its version.
func followerOf(t *testing.T, book *OrderBook) *AggregatedBook {
	t.Helper()
	depth := book.GetDepth(1000)
	ab := NewAggregatedBook()
	require.NoError(t, ab.ApplySnapshot(snapshotFromDepth(t, book.SeqID(), depth.Asks, depth.Bids)))
	return ab
}

// followCommand feeds the logs of one command to the builder, takes the delta the
// book has now, and applies it to follower. The batch must not be released before.
func followCommand(t *testing.T, book *OrderBook, follower *AggregatedBook, batch *LogBatch) {
	t.Helper()
	builder := NewDepthDeltaBuilder()
	builder.AddBatch(batch.Logs)
	var d protocol.DepthDelta
	if builder.Flush(book, &d) {
		require.NoError(t, follower.ApplyDelta(&d))
	}
}

// requireSameDepth asserts that the follower shows the book's version and levels.
func requireSameDepth(t *testing.T, book *OrderBook, follower *AggregatedBook) {
	t.Helper()
	want := book.GetDepth(1000)
	got := follower.GetDepth(1000)
	require.Equal(t, want.UpdateID, got.UpdateID, "version")
	require.Equal(t, dlLevels(want.Asks), dlLevels(got.Asks), "asks")
	require.Equal(t, dlLevels(want.Bids), dlLevels(got.Bids), "bids")
}
