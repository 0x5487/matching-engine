package match

import (
	"testing"
	"time"

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

func stepSizePlaceCmd(t *testing.T, id string, quote string) *protocol.PlaceOrderRequest {
	t.Helper()
	return &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{UserID: 2, CommandID: "cmd-" + id, MarketID: "BTC-USDT", Timestamp: 1},
		OrderID:     id,
		OrderType:   Market,
		Side:        Buy,
		StepSize:    udecimal.MustParse("0.001"),
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

	batch, err := book.PlaceOrder(stepSizePlaceCmd(t, "quote-buy", "100"))
	require.NoError(t, err)

	matches := matchLogs(t, batch)
	require.Len(t, matches, 1, "the resting offer is matchable and must not be skipped")
	assert.Equal(t, "0.0003", matches[0].MatchSize.String(),
		"the resting size is taken whole; flooring it to 0.001 would reject a matchable order")
	assert.Equal(t, "85000", matches[0].Price.String(), "a market order fills at the resting price")
}

func TestStepSizeDoesNotFloorTheQuantityPath(t *testing.T) {
	book := stepSizeTestBook(t)

	req := stepSizePlaceCmd(t, "qty-buy", "0")
	req.Size = udecimal.MustParse("0.002")
	batch, err := book.PlaceOrder(req)
	require.NoError(t, err)

	matches := matchLogs(t, batch)
	require.Len(t, matches, 1)
	assert.Equal(t, "0.0003", matches[0].MatchSize.String(),
		"min-then-floor would make this zero and reject the whole order over someone else's dust")
}

func TestStepSizeFloorsTheQuoteConversion(t *testing.T) {
	t.Run("a conversion that is not a whole step cannot fill", func(t *testing.T) {
		book := stepSizeTestBook(t)

		// 1.5 quote at 85000 is about 0.0000176, far under one 0.001 step.
		batch, err := book.PlaceOrder(stepSizePlaceCmd(t, "dust", "1.5"))
		require.NoError(t, err)

		assert.Empty(t, matchLogs(t, batch),
			"a size that floors to zero must not become a zero-size fill")
	})

	t.Run("a whole number of steps fills", func(t *testing.T) {
		book := stepSizeTestBook(t)

		// 0.001 BTC at 85000 is 85 quote, which is one whole step.
		batch, err := book.PlaceOrder(stepSizePlaceCmd(t, "one-step", "85"))
		require.NoError(t, err)

		matches := matchLogs(t, batch)
		require.Len(t, matches, 1)
		assert.Equal(t, "0.0003", matches[0].MatchSize.String(),
			"the resting size caps the fill below the whole step the quote would buy")
	})
}

// stepValidationBook returns a book with one clean resting offer on each
// side: a 1.0 sell at 85000 and a 1.0 buy at 84000. Both sizes are whole
// numbers of a 0.001 step, so any dust that appears after this point comes
// from the taker, never from the book.
func stepValidationBook(t *testing.T) *OrderBook {
	t.Helper()
	book := newOrderBookWithPublisher("test-engine", "BTC-USDT", NewMemoryPublishLog())
	testPlace(book, 1, "cmd-ask", 1, &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{UserID: 1, CommandID: "cmd-ask", MarketID: "BTC-USDT"},
		OrderID:     "resting-sell",
		OrderType:   Limit,
		Side:        Sell,
		Price:       udecimal.MustParse("85000"),
		Size:        udecimal.MustParse("1"),
	})
	testPlace(book, 1, "cmd-bid", 2, &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{UserID: 1, CommandID: "cmd-bid", MarketID: "BTC-USDT"},
		OrderID:     "resting-buy",
		OrderType:   Limit,
		Side:        Buy,
		Price:       udecimal.MustParse("84000"),
		Size:        udecimal.MustParse("1"),
	})
	return book
}

//nolint:revive // argument-limit: one parameter per field of the order under test keeps each case readable
func stepValidationPlace(
	t *testing.T,
	book *OrderBook,
	id string,
	ot OrderType,
	s Side,
	price, size, step string,
) *LogBatch {
	t.Helper()
	batch, err := book.PlaceOrder(&protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			UserID: 2, CommandID: "cmd-" + id, MarketID: "BTC-USDT", Timestamp: 10,
		},
		OrderID:   id,
		OrderType: ot,
		Side:      s,
		Price:     udecimal.MustParse(price),
		Size:      udecimal.MustParse(size),
		StepSize:  udecimal.MustParse(step),
	})
	require.NoError(t, err, "PlaceOrder returns rejects as logs, not errors")
	require.NotNil(t, batch)
	return batch
}

// requireInvalidPayload only inspects the batch. The caller owns it and releases it
// exactly once; a helper that released it too would put the batch in the pool twice.
func requireInvalidPayload(t *testing.T, batch *LogBatch, id string) {
	t.Helper()
	require.Len(t, batch.Logs, 1, "a refused order emits exactly one log, never a match beside it")
	log := batch.Logs[0]
	assert.Equal(t, protocol.LogTypeReject, log.Type, "order %q must be rejected", id)
	assert.Equal(
		t, protocol.RejectReasonInvalidPayload, log.RejectReason,
		"order %q breaks the step rule, not liquidity or price", id,
	)
}

// TestQuantityStepRejectsDustTakers pins the SDK's self-defense: a
// quantity-sized order carrying a step size it is not a whole number of is
// refused at the door, before matching. Without this, a shared-library caller
// that skips API-side validation can walk dust straight onto the book, and a
// dust FOK taker would even fill in full.
func TestQuantityStepRejectsDustTakers(t *testing.T) {
	tests := []struct {
		name      string
		orderType OrderType
		side      Side
		price     string
	}{
		{name: "limit buy", orderType: Limit, side: Buy, price: "85000"},
		{name: "limit sell", orderType: Limit, side: Sell, price: "84000"},
		{name: "market sell", orderType: Market, side: Sell, price: "0"},
		// A market buy sized in base takes the quantity path too: the check
		// does not split by side, only by whether Size is zero.
		{name: "market buy sized in base", orderType: Market, side: Buy, price: "0"},
		{name: "IOC buy", orderType: IOC, side: Buy, price: "85000"},
		{name: "FOK buy", orderType: FOK, side: Buy, price: "85000"},
		// Priced to rest rather than cross: the rejection must come from the
		// step rule, not from PostOnlyMatch.
		{name: "post-only buy", orderType: PostOnly, side: Buy, price: "84000"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			book := stepValidationBook(t)
			batch := stepValidationPlace(
				t, book, "dust", tc.orderType, tc.side,
				tc.price, "0.0003", "0.001",
			)
			requireInvalidPayload(t, batch, "dust")
		})
	}
}

// TestQuantityStepSkipsZeroStep pins the migration compatibility: callers
// that do not carry a step size keep the old behavior. A zero step means
// "no unit to measure against", not "a unit of zero".
func TestQuantityStepSkipsZeroStep(t *testing.T) {
	book := stepValidationBook(t)
	batch, err := book.PlaceOrder(&protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			UserID: 2, CommandID: "cmd-dust", MarketID: "BTC-USDT", Timestamp: 10,
		},
		OrderID:   "dust",
		OrderType: Limit,
		Side:      Buy,
		Price:     udecimal.MustParse("85000"),
		Size:      udecimal.MustParse("0.0003"),
		StepSize:  udecimal.Zero,
	})
	require.NoError(t, err)
	require.NotNil(t, batch)
	matches := matchLogs(t, batch)
	require.Len(t, matches, 1, "without a step there is nothing to refuse")
	assert.Equal(t, "0.0003", matches[0].MatchSize.String())
}

// TestQuantityStepAcceptsWholeMultiples pins that the check only refuses
// dust: whole numbers of steps behave exactly as before.
func TestQuantityStepAcceptsWholeMultiples(t *testing.T) {
	book := stepValidationBook(t)
	batch := stepValidationPlace(t, book, "clean", Limit, Buy, "85000", "0.002", "0.001")
	matches := matchLogs(t, batch)
	require.Len(t, matches, 1, "a clean taker matches exactly as before")
	assert.Equal(t, "0.002", matches[0].MatchSize.String())
}

// TestQuantityStepRejectsDustVisibleSize pins that the iceberg peak answers
// the same question as the size: the visible part rests on the book, so a
// non-whole peak is refused even when the total size is clean.
func TestQuantityStepRejectsDustVisibleSize(t *testing.T) {
	book := stepValidationBook(t)
	batch, err := book.PlaceOrder(&protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			UserID: 2, CommandID: "cmd-iceberg", MarketID: "BTC-USDT", Timestamp: 10,
		},
		OrderID:     "iceberg",
		OrderType:   Limit,
		Side:        Buy,
		Price:       udecimal.MustParse("84000"),
		Size:        udecimal.MustParse("0.002"),
		VisibleSize: udecimal.MustParse("0.0003"),
		StepSize:    udecimal.MustParse("0.001"),
	})
	require.NoError(t, err, "PlaceOrder returns rejects as logs, not errors")
	require.NotNil(t, batch)
	requireInvalidPayload(t, batch, "iceberg")
}

// TestNonStepQuantitiesStillTerminate answers whether dropping the lot-size check
// can leave the loop spinning. It cannot, and the reason is that the quantity
// path terminates by subtraction rather than by division: every iteration
// consumes either the whole of the maker's size or the whole of the taker's
// remainder, and both strictly reduce what is left.
//
// The floor matters on the quote path, where a division can round away to zero.
// On the quantity path nothing is divided, so a size that is not a whole number of
// steps is merely unusual - it cannot make the loop run forever.
//
// This runs in a goroutine with a deadline so that a regression is a failing
// test rather than a suite that hangs until the panic timeout.
func TestNonStepQuantitiesStillTerminate(t *testing.T) {
	book := newOrderBookWithPublisher("test-engine", "BTC-USDT", NewMemoryPublishLog())

	// Three offers at the same price, each a size that is not a whole number of a
	// 0.001 step. If any of these could wedge the loop, a taker would find out.
	for i, size := range []string{"0.0003", "0.00005", "0.0000075"} {
		testPlace(book, 1, "cmd-resting", int64(i+1), &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				UserID: 1, CommandID: "cmd-resting", MarketID: "BTC-USDT", Timestamp: int64(i + 1),
			},
			OrderID:   "resting-" + size,
			OrderType: Limit,
			Side:      Sell,
			Price:     udecimal.MustParse("85000"),
			Size:      udecimal.MustParse(size),
		})
	}
	require.Equal(t, int64(3), book.askQueue.orderCount())

	done := make(chan *LogBatch, 1)
	go func() {
		batch, err := book.PlaceOrder(&protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				UserID: 2, CommandID: "cmd-taker", MarketID: "BTC-USDT", Timestamp: 10,
			},
			OrderID:   "taker",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustParse("85000"),
			Size:      udecimal.MustParse("0.002"),
			StepSize:  udecimal.MustParse("0.001"),
		})
		if err != nil {
			close(done)
			return
		}
		done <- batch
	}()

	select {
	case batch := <-done:
		require.NotNil(t, batch)
		matches := matchLogs(t, batch)
		require.Len(t, matches, 3, "every offer is reachable, including the sub-step ones")
		assert.Equal(t, "0.0003", matches[0].MatchSize.String(), "the whole offer is taken, not floored to zero")
		assert.Equal(t, "0.00005", matches[1].MatchSize.String())
		assert.Equal(t, "0.0000075", matches[2].MatchSize.String())
		assert.Zero(t, book.askQueue.orderCount(), "the taker consumed all three")
	case <-time.After(5 * time.Second):
		t.Fatal("the matching loop did not terminate on non-step quantities")
	}
}
