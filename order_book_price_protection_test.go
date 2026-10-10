package match

import (
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

// A market order may carry a protection price in Price. It is a bound on the fill,
// not a limit that rests: the walk stops at the first level beyond it and the
// remainder comes back as a price_protection reject. Price zero means no bound.

func newProtectionBook(t *testing.T) *OrderBook {
	t.Helper()
	return newOrderBookWithPublisher("test-engine", "BTC-USDT", NewMemoryPublishLog())
}

// restLimit places a resting limit order on the given side.
func restLimit(t *testing.T, book *OrderBook, id string, side Side, price, size string) {
	t.Helper()
	batch, err := book.PlaceOrder(&protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{UserID: 1, CommandID: "cmd-" + id, MarketID: book.marketID, Timestamp: 1},
		OrderID:     id,
		OrderType:   Limit,
		Side:        side,
		Price:       udecimal.MustParse(price),
		Size:        udecimal.MustParse(size),
	})
	require.NoError(t, err)
	require.NotNil(t, batch)
	require.Len(t, batch.Logs, 1, "a resting limit order produces exactly its open log")
}

// marketArgs describes a market order. Quantities are strings so that "0" stands
// for an absent size or quote.
type marketArgs struct {
	size, quote, price, step string
}

// placeMarket places a market order.
func placeMarket(t *testing.T, book *OrderBook, id string, side Side, a marketArgs) *LogBatch {
	t.Helper()
	batch, err := book.PlaceOrder(&protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{UserID: 2, CommandID: "cmd-" + id, MarketID: book.marketID, Timestamp: 2},
		OrderID:     id,
		OrderType:   Market,
		Side:        side,
		Price:       udecimal.MustParse(a.price),
		Size:        udecimal.MustParse(a.size),
		QuoteSize:   udecimal.MustParse(a.quote),
		StepSize:    udecimal.MustParse(a.step),
	})
	require.NoError(t, err)
	require.NotNil(t, batch)
	return batch
}

func requireMatchLog(t *testing.T, log *OrderBookLog, price, size string) {
	t.Helper()
	require.Equal(t, protocol.LogTypeMatch, log.Type)
	assert.Equal(t, price, log.Price.String(), "fill price")
	assert.Equal(t, size, log.Size.String(), "fill size")
}

func requireProtectionReject(t *testing.T, log *OrderBookLog, remaining string) {
	t.Helper()
	require.Equal(t, protocol.LogTypeReject, log.Type)
	assert.Equal(t, protocol.RejectReasonPriceProtection, log.RejectReason)
	assert.Equal(t, remaining, log.Size.String(), "the reject carries the unfilled remainder")
}

// AC-01-1: a quantity-sized buy stops at the first ask above the protection price.
func TestPriceProtectionBuyQuantityStopsAboveBound(t *testing.T) {
	book := newProtectionBook(t)
	restLimit(t, book, "ask-99", Sell, "99", "1")
	restLimit(t, book, "ask-100", Sell, "100", "1")
	restLimit(t, book, "ask-101", Sell, "101", "1")

	batch := placeMarket(t, book, "mkt-buy", Buy, marketArgs{size: "3", quote: "0", price: "100", step: "0"})

	require.Len(t, batch.Logs, 3)
	requireMatchLog(t, batch.Logs[0], "99", "1")
	requireMatchLog(t, batch.Logs[1], "100", "1")
	requireProtectionReject(t, batch.Logs[2], "1")

	assert.Equal(t, int64(1), book.askQueue.depthCount())
	head := book.askQueue.peekHeadOrder()
	require.NotNil(t, head)
	assert.Equal(t, "101", head.Price.String(), "the level beyond the bound is untouched")
	assert.Equal(t, "1", head.Size.String())
}

// AC-01-2: a quantity-sized sell stops at the first bid below the protection price.
func TestPriceProtectionSellQuantityStopsBelowBound(t *testing.T) {
	book := newProtectionBook(t)
	restLimit(t, book, "bid-101", Buy, "101", "1")
	restLimit(t, book, "bid-100", Buy, "100", "1")
	restLimit(t, book, "bid-99", Buy, "99", "1")

	batch := placeMarket(t, book, "mkt-sell", Sell, marketArgs{size: "3", quote: "0", price: "100", step: "0"})

	require.Len(t, batch.Logs, 3)
	requireMatchLog(t, batch.Logs[0], "101", "1")
	requireMatchLog(t, batch.Logs[1], "100", "1")
	requireProtectionReject(t, batch.Logs[2], "1")

	assert.Equal(t, int64(1), book.bidQueue.depthCount())
	head := book.bidQueue.peekHeadOrder()
	require.NotNil(t, head)
	assert.Equal(t, "99", head.Price.String(), "the level beyond the bound is untouched")
}

// AC-01-3: a quote-sized buy stops with the unspent quote as the remainder.
func TestPriceProtectionBuyQuoteStopsAboveBound(t *testing.T) {
	book := newProtectionBook(t)
	restLimit(t, book, "ask-99", Sell, "99", "1")
	restLimit(t, book, "ask-100", Sell, "100", "1")
	restLimit(t, book, "ask-101", Sell, "101", "1")

	batch := placeMarket(
		t,
		book,
		"mkt-buy-quote",
		Buy,
		marketArgs{size: "0", quote: "1000", price: "100", step: "0.01"},
	)

	require.Len(t, batch.Logs, 3)
	requireMatchLog(t, batch.Logs[0], "99", "1")
	requireMatchLog(t, batch.Logs[1], "100", "1")
	requireProtectionReject(t, batch.Logs[2], "801")
	assert.Equal(t, int64(1), book.askQueue.depthCount())
}

// AC-01-4: a quote-sized sell that spends its whole quote on one level emits no reject.
func TestQuoteSellSpendsWholeQuoteWithoutReject(t *testing.T) {
	book := newProtectionBook(t)
	restLimit(t, book, "bid-100", Buy, "100", "5")

	batch := placeMarket(t, book, "mkt-sell-quote", Sell, marketArgs{size: "0", quote: "250", price: "0", step: "0.01"})

	require.Len(t, batch.Logs, 1)
	requireMatchLog(t, batch.Logs[0], "100", "2.5")
	assert.Equal(t, int64(1), book.bidQueue.depthCount())
}

// AC-01-5: a quote-sized sell stops at a bid below the bound and returns the quote
// it did not spend.
func TestPriceProtectionSellQuoteStopsBelowBound(t *testing.T) {
	book := newProtectionBook(t)
	restLimit(t, book, "bid-101", Buy, "101", "1")
	restLimit(t, book, "bid-99", Buy, "99", "10")

	batch := placeMarket(
		t,
		book,
		"mkt-sell-quote",
		Sell,
		marketArgs{size: "0", quote: "500", price: "100", step: "0.01"},
	)

	require.Len(t, batch.Logs, 2)
	requireMatchLog(t, batch.Logs[0], "101", "1")
	requireProtectionReject(t, batch.Logs[1], "399")
	assert.Equal(t, int64(1), book.bidQueue.depthCount())
	assert.Equal(t, "99", book.bidQueue.peekHeadOrder().Price.String())
}

// AC-01-6: when nothing fills, exactly one price_protection reject carries the
// original size and the book is unchanged.
func TestPriceProtectionWithNoFillEmitsOneReject(t *testing.T) {
	book := newProtectionBook(t)
	restLimit(t, book, "ask-101", Sell, "101", "1")

	batch := placeMarket(t, book, "mkt-buy", Buy, marketArgs{size: "1", quote: "0", price: "100", step: "0"})

	require.Len(t, batch.Logs, 1)
	requireProtectionReject(t, batch.Logs[0], "1")
	assert.Equal(t, int64(1), book.askQueue.depthCount())
	assert.Equal(t, "1", book.askQueue.peekHeadOrder().Size.String())
}

// AC-01-7: the bound is inclusive, so a level exactly at the protection price fills.
func TestPriceProtectionBoundIsInclusive(t *testing.T) {
	book := newProtectionBook(t)
	restLimit(t, book, "ask-100", Sell, "100", "1")

	batch := placeMarket(t, book, "mkt-buy", Buy, marketArgs{size: "1", quote: "0", price: "100", step: "0"})

	require.Len(t, batch.Logs, 1)
	requireMatchLog(t, batch.Logs[0], "100", "1")
	assert.Equal(t, int64(0), book.askQueue.depthCount())
}

// The bound is inclusive on the sell side too: a bid exactly at the protection price fills.
func TestPriceProtectionSellBoundIsInclusive(t *testing.T) {
	book := newProtectionBook(t)
	restLimit(t, book, "bid-100", Buy, "100", "1")

	batch := placeMarket(t, book, "mkt-sell", Sell, marketArgs{size: "1", quote: "0", price: "100", step: "0"})

	require.Len(t, batch.Logs, 1)
	requireMatchLog(t, batch.Logs[0], "100", "1")
	assert.Equal(t, int64(0), book.bidQueue.depthCount())
}

// A market order with a negative price is an invalid request.
func TestMarketOrderNegativePriceIsInvalid(t *testing.T) {
	book := newProtectionBook(t)
	restLimit(t, book, "ask-100", Sell, "100", "1")

	batch := placeMarket(t, book, "mkt-buy-neg", Buy, marketArgs{size: "1", quote: "0", price: "-1", step: "0"})

	require.Len(t, batch.Logs, 1)
	assert.Equal(t, protocol.LogTypeReject, batch.Logs[0].Type)
	assert.Equal(t, protocol.RejectReasonInvalidPayload, batch.Logs[0].RejectReason)
	assert.Equal(t, int64(1), book.askQueue.depthCount(), "an invalid order must not touch the book")
}

// Price zero keeps the unbounded behavior: the same book fills through every level.
func TestMarketOrderWithoutPriceIsUnbounded(t *testing.T) {
	book := newProtectionBook(t)
	restLimit(t, book, "ask-99", Sell, "99", "1")
	restLimit(t, book, "ask-100", Sell, "100", "1")
	restLimit(t, book, "ask-101", Sell, "101", "1")

	batch := placeMarket(t, book, "mkt-buy", Buy, marketArgs{size: "3", quote: "0", price: "0", step: "0"})

	require.Len(t, batch.Logs, 3)
	requireMatchLog(t, batch.Logs[2], "101", "1")
	assert.Equal(t, int64(0), book.askQueue.depthCount())
}
