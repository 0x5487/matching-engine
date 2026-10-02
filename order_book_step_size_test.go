package match

import (
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

// The step size is the unit of account for the base asset: a balance has to be a
// whole number of steps or it cannot be reconciled against the ledger. That makes
// it more than an order-size rule, and it is why the book does not keep one - it
// arrives on each order instead, so a replay cannot disagree with itself about
// which unit was in force.
//
// These tests pin one thing above all: where the floor happens. Flooring the
// quote conversion and then taking the minimum keeps an order that was placed
// under an older, finer step reachable. Taking the minimum first and then
// flooring sends that same order to zero, which rejects it outright and lets one
// holder's dust stop an unrelated large order.

// stepSizeTestBook returns a book with a single resting offer whose size is
// 0.0003: three steps of 0.0001, and not a whole number of 0.001. That is the
// state the book is left in once the market's step is coarsened, and it is the
// case the ordering has to survive.
func stepSizeTestBook(t *testing.T) *OrderBook {
	t.Helper()
	book := newOrderBookWithPublisher("test-engine", "BTC-USDT", NewMemoryPublishLog())
	testPlace(book, 1, "cmd-resting", 1, &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{UserID: 1, CommandID: "cmd-resting", MarketID: "BTC-USDT"},
		OrderID:     "resting-sell",
		OrderType:   Limit,
		Side:        Sell,
		Price:       udecimal.MustParse("85000"),
		Size:        udecimal.MustParse("0.0003"),
	})
	require.Equal(t, int64(1), book.askQueue.orderCount(), "the resting offer should be on the book")
	return book
}

func stepSizePlaceCmd(t *testing.T, id string, ot OrderType, s Side, quote, step string) *protocol.PlaceOrderRequest {
	t.Helper()
	return &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{UserID: 2, CommandID: "cmd-" + id, MarketID: "BTC-USDT", Timestamp: 1},
		OrderID:     id,
		OrderType:   ot,
		Side:        s,
		StepSize:    udecimal.MustParse(step),
		Size:        udecimal.Zero,
		QuoteSize:   udecimal.MustParse(quote),
	}
}

func matchLogs(t *testing.T, batch *LogBatch) []*OrderBookLog {
	t.Helper()
	require.NotNil(t, batch, "PlaceOrder must return a batch")
	var matches []*OrderBookLog
	for _, l := range batch.Logs {
		if l.Type == protocol.LogTypeMatch {
			matches = append(matches, l)
		}
	}
	return matches
}

func TestStepSizeKeepsOrdersPlacedUnderAnOlderStep(t *testing.T) {
	book := stepSizeTestBook(t)

	batch, err := book.PlaceOrder(stepSizePlaceCmd(t, "quote-buy", Market, Buy, "100", "0.001"))
	require.NoError(t, err)

	matches := matchLogs(t, batch)
	require.Len(t, matches, 1, "the resting offer is matchable and must not be skipped")
	assert.Equal(t, "0.0003", matches[0].Size.String(),
		"the resting size is taken whole; flooring it to 0.001 would reject a matchable order")
	assert.Equal(t, "85000", matches[0].Price.String(), "a market order fills at the resting price")
}

func TestStepSizeDoesNotFloorTheQuantityPath(t *testing.T) {
	book := stepSizeTestBook(t)

	req := stepSizePlaceCmd(t, "qty-buy", Market, Buy, "0", "0.001")
	req.Size = udecimal.MustParse("0.002")
	batch, err := book.PlaceOrder(req)
	require.NoError(t, err)

	matches := matchLogs(t, batch)
	require.Len(t, matches, 1)
	assert.Equal(t, "0.0003", matches[0].Size.String(),
		"min-then-floor would make this zero and reject the whole order over someone else's dust")
}

func TestStepSizeFloorsTheQuoteConversion(t *testing.T) {
	t.Run("a conversion that is not a whole step cannot fill", func(t *testing.T) {
		book := stepSizeTestBook(t)

		// 1.5 quote at 85000 is about 0.0000176, far under one 0.001 step.
		batch, err := book.PlaceOrder(stepSizePlaceCmd(t, "dust", Market, Buy, "1.5", "0.001"))
		require.NoError(t, err)

		assert.Empty(t, matchLogs(t, batch),
			"a size that floors to zero must not become a zero-size fill")
	})

	t.Run("a whole number of steps fills", func(t *testing.T) {
		book := stepSizeTestBook(t)

		// 0.001 BTC at 85000 is 85 quote, which is one whole step.
		batch, err := book.PlaceOrder(stepSizePlaceCmd(t, "one-step", Market, Buy, "85", "0.001"))
		require.NoError(t, err)

		matches := matchLogs(t, batch)
		require.Len(t, matches, 1)
		assert.Equal(t, "0.0003", matches[0].Size.String(),
			"the resting size caps the fill below the whole step the quote would buy")
	})
}