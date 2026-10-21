package match

import (
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

// logFields is every quantity field of one log, as strings, so a zero field
// reads "0" and a field a log type must not set fails the comparison.
type logFields struct {
	typ            protocol.LogType
	price          string
	oldPrice       string
	matchSize      string
	remaining      string
	oldRemaining   string
	visibleAdded   string
	visibleRemoved string
	quoteRemaining string
}

func fieldsOf(l *OrderBookLog) logFields {
	return logFields{
		typ:            l.Type,
		price:          l.Price.String(),
		oldPrice:       l.OldPrice.String(),
		matchSize:      l.MatchSize.String(),
		remaining:      l.Remaining.String(),
		oldRemaining:   l.OldRemaining.String(),
		visibleAdded:   l.VisibleAdded.String(),
		visibleRemoved: l.VisibleRemoved.String(),
		quoteRemaining: l.QuoteRemaining.String(),
	}
}

// quantityOf returns the one quantity a log of this type carries, for the tests
// that compare sizes across types: the traded size of a match, the total of
// every other log that has one. The field table test pins every field.
func quantityOf(l *OrderBookLog) udecimal.Decimal {
	switch l.Type {
	case protocol.LogTypeMatch:
		return l.MatchSize
	case protocol.LogTypeOpen, protocol.LogTypeCancel, protocol.LogTypeAmend, protocol.LogTypeReject:
		return l.Remaining
	default:
		return udecimal.Zero
	}
}

// fieldCall is one command on a book; it returns the batch the book answered with.
type fieldCall func(t *testing.T, book *OrderBook) *LogBatch

const fieldsMarket = "LOG-FIELDS"

func fieldsBase(commandID string) protocol.BaseCommand {
	return protocol.BaseCommand{CommandID: commandID, UserID: 1, MarketID: fieldsMarket, Timestamp: 1000}
}

//nolint:revive // argument-limit
func placeLogOrder(orderID string, side Side, typ OrderType, price, size, visible, quote, step string) fieldCall {
	return func(t *testing.T, book *OrderBook) *LogBatch {
		t.Helper()
		batch, err := book.PlaceOrder(&protocol.PlaceOrderRequest{
			BaseCommand: fieldsBase("place-" + orderID),
			OrderID:     orderID,
			Side:        side,
			OrderType:   typ,
			Price:       udecimal.MustParse(price),
			Size:        udecimal.MustParse(size),
			VisibleSize: udecimal.MustParse(visible),
			QuoteSize:   udecimal.MustParse(quote),
			StepSize:    udecimal.MustParse(step),
		})
		require.NoError(t, err)
		return batch
	}
}

func cancelLogOrder(orderID string) fieldCall {
	return func(t *testing.T, book *OrderBook) *LogBatch {
		t.Helper()
		batch, err := book.CancelOrder(&protocol.CancelOrderRequest{
			BaseCommand: fieldsBase("cancel-" + orderID),
			OrderID:     orderID,
		})
		require.NoError(t, err)
		return batch
	}
}

func amendLogOrder(orderID, newPrice, newSize string) fieldCall {
	return func(t *testing.T, book *OrderBook) *LogBatch {
		t.Helper()
		batch, err := book.AmendOrder(&protocol.AmendOrderRequest{
			BaseCommand: fieldsBase("amend-" + orderID),
			OrderID:     orderID,
			NewPrice:    udecimal.MustParse(newPrice),
			NewSize:     udecimal.MustParse(newSize),
		})
		require.NoError(t, err)
		return batch
	}
}

// TestLogFieldsByType pins which fields each log type sets and which it leaves
// zero. Each scenario runs its setup commands unasserted, then the step, and
// compares every log of the step's batch field by field.
func TestLogFieldsByType(t *testing.T) {
	t.Parallel()
	const zero = "0"
	tests := []struct {
		name  string
		setup []fieldCall
		step  fieldCall
		want  []logFields
	}{
		{
			name: "open of a whole order: the visible part is the total",
			step: placeLogOrder("maker", Buy, Limit, "100", "10", "0", "0", "0"),
			want: []logFields{
				{
					typ: protocol.LogTypeOpen, price: "100", oldPrice: zero, matchSize: zero, remaining: "10",
					oldRemaining: zero, visibleAdded: "10", visibleRemoved: zero, quoteRemaining: zero,
				},
			},
		},
		{
			name: "open of an iceberg: the hidden part is in Remaining only",
			step: placeLogOrder("maker", Buy, Limit, "100", "10", "4", "0", "0"),
			want: []logFields{
				{
					typ: protocol.LogTypeOpen, price: "100", oldPrice: zero, matchSize: zero, remaining: "10",
					oldRemaining: zero, visibleAdded: "4", visibleRemoved: zero, quoteRemaining: zero,
				},
			},
		},
		{
			name:  "match: the traded size and nothing about the maker's total",
			setup: []fieldCall{placeLogOrder("maker", Sell, Limit, "100", "5", "0", "0", "0")},
			step:  placeLogOrder("taker", Buy, Limit, "100", "3", "0", "0", "0"),
			want: []logFields{
				{
					typ: protocol.LogTypeMatch, price: "100", oldPrice: zero, matchSize: "3", remaining: zero,
					oldRemaining: zero, visibleAdded: zero, visibleRemoved: zero, quoteRemaining: zero,
				},
			},
		},
		{
			name:  "cancel of an iceberg: the total before the cancel, and the visible part it removes",
			setup: []fieldCall{placeLogOrder("maker", Sell, Limit, "100", "10", "4", "0", "0")},
			step:  cancelLogOrder("maker"),
			want: []logFields{
				{
					typ: protocol.LogTypeCancel, price: "100", oldPrice: zero, matchSize: zero, remaining: "10",
					oldRemaining: zero, visibleAdded: zero, visibleRemoved: "4", quoteRemaining: zero,
				},
			},
		},
		{
			name:  "amend that shrinks inside the hidden part: no visible size leaves the book",
			setup: []fieldCall{placeLogOrder("maker", Buy, Limit, "100", "10", "4", "0", "0")},
			step:  amendLogOrder("maker", "0", "8"),
			want: []logFields{
				{
					typ: protocol.LogTypeAmend, price: "100", oldPrice: "100", matchSize: zero, remaining: "8",
					oldRemaining: "10", visibleAdded: zero, visibleRemoved: zero, quoteRemaining: zero,
				},
			},
		},
		{
			name:  "amend that loses priority by price: the amend, then the open at the new price",
			setup: []fieldCall{placeLogOrder("maker", Sell, Limit, "100", "5", "0", "0", "0")},
			step:  amendLogOrder("maker", "101", "0"),
			want: []logFields{
				{
					typ: protocol.LogTypeAmend, price: "101", oldPrice: "100", matchSize: zero, remaining: "5",
					oldRemaining: "5", visibleAdded: zero, visibleRemoved: "5", quoteRemaining: zero,
				},
				{
					typ: protocol.LogTypeOpen, price: "101", oldPrice: zero, matchSize: zero, remaining: "5",
					oldRemaining: zero, visibleAdded: "5", visibleRemoved: zero, quoteRemaining: zero,
				},
			},
		},
		{
			name: "replenish after a fill: the match, the open of the new visible part, the next match",
			setup: []fieldCall{
				placeLogOrder("maker", Sell, Limit, "100", "10", "4", "0", "0"),
			},
			step: placeLogOrder("taker", Buy, Limit, "100", "6", "0", "0", "0"),
			want: []logFields{
				{
					typ: protocol.LogTypeMatch, price: "100", oldPrice: zero, matchSize: "4", remaining: zero,
					oldRemaining: zero, visibleAdded: zero, visibleRemoved: zero, quoteRemaining: zero,
				},
				{
					typ: protocol.LogTypeOpen, price: "100", oldPrice: zero, matchSize: zero, remaining: "6",
					oldRemaining: zero, visibleAdded: "4", visibleRemoved: zero, quoteRemaining: zero,
				},
				{
					typ: protocol.LogTypeMatch, price: "100", oldPrice: zero, matchSize: "2", remaining: zero,
					oldRemaining: zero, visibleAdded: zero, visibleRemoved: zero, quoteRemaining: zero,
				},
			},
		},
		{
			name: "reject of a base-sized order: the base it did not get",
			step: placeLogOrder("taker", Buy, IOC, "100", "4", "0", "0", "0"),
			want: []logFields{
				{
					typ: protocol.LogTypeReject, price: "100", oldPrice: zero, matchSize: zero, remaining: "4",
					oldRemaining: zero, visibleAdded: zero, visibleRemoved: zero, quoteRemaining: zero,
				},
			},
		},
		{
			name: "reject of a quote-sized market order: the quote it did not spend, no base",
			step: placeLogOrder("taker", Buy, Market, "0", "0", "0", "100", "0.01"),
			want: []logFields{
				{
					typ: protocol.LogTypeReject, price: zero, oldPrice: zero, matchSize: zero, remaining: zero,
					oldRemaining: zero, visibleAdded: zero, visibleRemoved: zero, quoteRemaining: "100",
				},
			},
		},
		{
			name:  "quote-sized market order that trades then runs out of liquidity",
			setup: []fieldCall{placeLogOrder("maker", Sell, Limit, "100", "1", "0", "0", "0")},
			step:  placeLogOrder("taker", Buy, Market, "0", "0", "0", "250", "0.01"),
			want: []logFields{
				{
					typ: protocol.LogTypeMatch, price: "100", oldPrice: zero, matchSize: "1", remaining: zero,
					oldRemaining: zero, visibleAdded: zero, visibleRemoved: zero, quoteRemaining: zero,
				},
				{
					typ: protocol.LogTypeReject, price: zero, oldPrice: zero, matchSize: zero, remaining: zero,
					oldRemaining: zero, visibleAdded: zero, visibleRemoved: zero, quoteRemaining: "150",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			book := NewOrderBook(fieldsMarket, WithPublisher(NewDiscardPublishLog()))
			for _, setup := range tt.setup {
				setup(t, book)
			}
			batch := tt.step(t, book)
			got := make([]logFields, 0, len(batch.Logs))
			for _, l := range batch.Logs {
				got = append(got, fieldsOf(l))
			}
			require.Equal(t, tt.want, got)
		})
	}
}
