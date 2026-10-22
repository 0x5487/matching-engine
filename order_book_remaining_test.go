package match

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

// remainingModel is the total remaining size of every resting order as the log
// stream says it is: an open or an amend states the total, a match takes its size
// from the maker and from a taker that is already resting, and a cancel or a
// fully matched order leaves the model. It never reads the book.
type remainingModel map[string]udecimal.Decimal

func (m remainingModel) apply(logs []*OrderBookLog) {
	for _, l := range logs {
		switch l.Type {
		case protocol.LogTypeOpen, protocol.LogTypeAmend:
			m[l.OrderID] = l.Remaining
		case protocol.LogTypeMatch:
			m.take(l.OrderID, l.MatchSize)
			m.take(l.MakerOrder.ID, l.MatchSize)
		case protocol.LogTypeCancel:
			delete(m, l.OrderID)
		default:
		}
	}
}

func (m remainingModel) take(orderID string, size udecimal.Decimal) {
	left, ok := m[orderID]
	if !ok {
		return
	}
	left = left.Sub(size)
	if left.IsZero() {
		delete(m, orderID)
		return
	}
	m[orderID] = left
}

// requireRemainingMatchesModel checks that the book's Remaining, the one owner of
// the total, agrees with the log-derived model for every resting order, and that
// the book and the model hold the same orders.
func requireRemainingMatchesModel(t *testing.T, book *OrderBook, model remainingModel, step string) {
	t.Helper()
	snap := book.Snapshot()
	resting := make(map[string]udecimal.Decimal, len(snap.Bids)+len(snap.Asks))
	for _, o := range snap.Bids {
		resting[o.ID] = o.Size.Add(o.HiddenSize)
	}
	for _, o := range snap.Asks {
		resting[o.ID] = o.Size.Add(o.HiddenSize)
	}
	require.Len(t, model, len(resting), "%s: the model and the book hold other orders", step)
	for id, want := range resting {
		got, ok := book.Remaining(id)
		require.True(t, ok, "%s: resting order %s is not found by Remaining", step, id)
		require.True(
			t,
			got.Equal(want),
			"%s: order %s Remaining %s, its visible plus hidden is %s",
			step,
			id,
			got,
			want,
		)
		require.True(
			t,
			model[id].Equal(want),
			"%s: order %s the logs say %s, the book says %s",
			step,
			id,
			model[id],
			want,
		)
	}
}

// TestRemainingIsTheOrdersTotalAfterEveryCall runs seeded random commands on one
// book: limit and market orders, icebergs that replenish, cancels and amends that
// change the size and the price. After every call, each resting order's total
// (visible plus hidden) equals what its logs say, and an order that has gone is
// not found.
func TestRemainingIsTheOrdersTotalAfterEveryCall(t *testing.T) {
	t.Parallel()
	for seed := range uint64(6) {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			t.Parallel()
			//nolint:gosec // G404: a seeded generator makes the random commands repeatable; it is not for security
			rng := rand.New(rand.NewPCG(seed, seed+1))
			book := NewOrderBook(fieldsMarket, WithPublisher(NewDiscardPublishLog()))
			model := remainingModel{}
			var resting []string
			seq, clock := uint64(0), int64(1)
			next := func() protocol.BaseCommand {
				seq++
				clock++
				return protocol.BaseCommand{
					SeqID: seq, CommandID: fmt.Sprintf("c-%d", seq), UserID: 1,
					MarketID: fieldsMarket, Timestamp: clock,
				}
			}
			for step := range 2000 {
				name := fmt.Sprintf("seed %d step %d", seed, step)
				switch choice := rng.IntN(10); {
				case choice < 5 || len(resting) == 0:
					id := fmt.Sprintf("o-%d", step)
					side := Buy
					if rng.IntN(2) == 0 {
						side = Sell
					}
					size := udecimal.MustFromInt64(int64(1+rng.IntN(12)), 0)
					visible := udecimal.Zero
					if rng.IntN(3) == 0 {
						visible = udecimal.MustFromInt64(int64(1+rng.IntN(4)), 0)
					}
					orderType := Limit
					if rng.IntN(5) == 0 {
						orderType = IOC
					}
					price := udecimal.MustFromInt64(int64(98+rng.IntN(5)), 0)
					batch, err := book.PlaceOrder(&protocol.PlaceOrderRequest{
						BaseCommand: next(), OrderID: id, Side: side, OrderType: orderType,
						Price: price, Size: size, VisibleSize: visible,
					})
					require.NoError(t, err)
					model.apply(batch.Logs)
					if _, ok := model[id]; ok {
						resting = append(resting, id)
					}
				case choice < 7:
					i := rng.IntN(len(resting))
					id := resting[i]
					batch, err := book.CancelOrder(&protocol.CancelOrderRequest{BaseCommand: next(), OrderID: id})
					require.NoError(t, err)
					model.apply(batch.Logs)
					resting = append(resting[:i], resting[i+1:]...)
				default:
					i := rng.IntN(len(resting))
					id := resting[i]
					newPrice := udecimal.Zero
					if rng.IntN(2) == 0 {
						newPrice = udecimal.MustFromInt64(int64(98+rng.IntN(5)), 0)
					}
					newSize := udecimal.Zero
					if rng.IntN(3) != 0 {
						newSize = udecimal.MustFromInt64(int64(1+rng.IntN(14)), 0)
					}
					batch, err := book.AmendOrder(&protocol.AmendOrderRequest{
						BaseCommand: next(), OrderID: id, NewPrice: newPrice, NewSize: newSize,
					})
					require.NoError(t, err)
					model.apply(batch.Logs)
				}
				resting = keepResting(resting, model)
				requireRemainingMatchesModel(t, book, model, name)
			}
		})
	}
}

// keepResting drops from ids every order the model no longer holds, keeping order.
func keepResting(ids []string, model remainingModel) []string {
	out := ids[:0]
	for _, id := range ids {
		if _, ok := model[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

// TestRemainingNotFoundForAGoneOrder pins the second result: an order that was
// filled or canceled is not found, and an order that rests is found with its
// total, which includes the hidden part an iceberg keeps off the book.
func TestRemainingNotFoundForAGoneOrder(t *testing.T) {
	t.Parallel()
	book := NewOrderBook(fieldsMarket, WithPublisher(NewDiscardPublishLog()))
	placeLogOrder("ice", Sell, Limit, "100", "10", "4", "0", "0")(t, book)
	got, ok := book.Remaining("ice")
	require.True(t, ok)
	require.Equal(t, "10", got.String(), "visible 4 and hidden 6")

	placeLogOrder("taker", Buy, Limit, "100", "10", "0", "0", "0")(t, book)
	_, ok = book.Remaining("ice")
	require.False(t, ok, "a fully matched order is gone")
	got, ok = book.Remaining("taker")
	require.False(t, ok, "a taker that filled completely never rested")
	require.True(t, got.IsZero())
}

// TestAmendThatCrossesKeepsItsHiddenPart pins an iceberg amend that changes the
// price and crosses the book. The amend re-matches the whole new total, so the
// hidden part is not lost when the visible part is consumed; the rest rests with
// its total and its visible part, which is what an open of the same order states.
func TestAmendThatCrossesKeepsItsHiddenPart(t *testing.T) {
	t.Parallel()
	book := NewOrderBook(fieldsMarket, WithPublisher(NewDiscardPublishLog()))
	placeLogOrder("bid", Buy, Limit, "100", "2", "0", "0", "0")(t, book)
	placeLogOrder("ask", Sell, Limit, "101", "4", "2", "0", "0")(t, book)

	batch := amendLogOrder("ask", "100", "4")(t, book)
	got := make([]logFields, 0, len(batch.Logs))
	for _, l := range batch.Logs {
		got = append(got, fieldsOf(l))
	}
	const zero = "0"
	require.Equal(t, []logFields{
		{
			typ: protocol.LogTypeAmend, price: "100", oldPrice: "101", matchSize: zero, remaining: "4",
			oldRemaining: "4", visibleAdded: zero, visibleRemoved: "2", quoteRemaining: zero,
		},
		{
			typ: protocol.LogTypeMatch, price: "100", oldPrice: zero, matchSize: "2", remaining: zero,
			oldRemaining: zero, visibleAdded: zero, visibleRemoved: zero, quoteRemaining: zero,
		},
		{
			typ: protocol.LogTypeOpen, price: "100", oldPrice: zero, matchSize: zero, remaining: "2",
			oldRemaining: zero, visibleAdded: "2", visibleRemoved: zero, quoteRemaining: zero,
		},
	}, got)

	total, ok := book.Remaining("ask")
	require.True(t, ok, "the amended order rests with its remainder")
	require.Equal(t, "2", total.String(), "the hidden part is kept")
}
