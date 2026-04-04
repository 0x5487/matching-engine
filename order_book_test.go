package match

import (
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

const (
	orderIDMarketBuy = "market-buy"
)

func newPlaceCmd(id string, ot OrderType, s Side, price, size float64) *protocol.PlaceOrderParams {
	return &protocol.PlaceOrderParams{
		OrderID:   id,
		OrderType: ot,
		Side:      s,
		Price:     udecimal.MustFromFloat64(price).String(),
		Size:      udecimal.MustFromFloat64(size).String(),
	}
}

func newAmendCmd(id string, price, size float64) *protocol.AmendOrderParams { //nolint:unparam
	return &protocol.AmendOrderParams{
		OrderID:  id,
		NewPrice: udecimal.MustFromFloat64(price).String(),
		NewSize:  udecimal.MustFromFloat64(size).String(),
	}
}

func newCancelCmd(id string) *protocol.CancelOrderParams {
	return &protocol.CancelOrderParams{
		OrderID: id,
	}
}

// testPlace serializes a PlaceOrderParams and calls processCommand synchronously.
func testPlace(
	book *OrderBook,
	userID uint64,
	commandID string,
	ts int64,
	params *protocol.PlaceOrderParams,
) {
	bytes, _ := params.MarshalBinary()
	book.processCommand(&InputEvent{
		Cmd: &protocol.Command{
			MarketID:  book.marketID,
			Type:      protocol.CmdPlaceOrder,
			UserID:    userID,
			CommandID: commandID,
			Timestamp: ts,
			Payload:   bytes,
		},
	})
}

// testCancel serializes a CancelOrderParams and calls processCommand synchronously.
func testCancel(
	book *OrderBook,
	userID uint64,
	commandID string,
	ts int64,
	params *protocol.CancelOrderParams,
) {
	bytes, _ := params.MarshalBinary()
	book.processCommand(&InputEvent{
		Cmd: &protocol.Command{
			MarketID:  book.marketID,
			Type:      protocol.CmdCancelOrder,
			UserID:    userID,
			CommandID: commandID,
			Timestamp: ts,
			Payload:   bytes,
		},
	})
}

// testAmend serializes an AmendOrderParams and calls processCommand synchronously.
func testAmend(
	book *OrderBook,
	userID uint64,
	commandID string,
	ts int64,
	params *protocol.AmendOrderParams,
) {
	bytes, _ := params.MarshalBinary()
	book.processCommand(&InputEvent{
		Cmd: &protocol.Command{
			MarketID:  book.marketID,
			Type:      protocol.CmdAmendOrder,
			UserID:    userID,
			CommandID: commandID,
			Timestamp: ts,
			Payload:   bytes,
		},
	})
}

func createTestOrderBook(t *testing.T) *OrderBook {
	t.Helper()
	publishTrader := NewMemoryPublishLog()
	orderBook := newOrderBook("test-engine", "BTC-USDT", publishTrader)

	testPlace(orderBook, 101, "cmd-buy-1", 1, newPlaceCmd("buy-1", Limit, Buy, 90, 1))
	testPlace(orderBook, 102, "cmd-buy-2", 1, newPlaceCmd("buy-2", Limit, Buy, 80, 1))
	testPlace(orderBook, 103, "cmd-buy-3", 1, newPlaceCmd("buy-3", Limit, Buy, 70, 1))
	testPlace(orderBook, 201, "cmd-sell-1", 1, newPlaceCmd("sell-1", Limit, Sell, 110, 1))
	testPlace(orderBook, 202, "cmd-sell-2", 1, newPlaceCmd("sell-2", Limit, Sell, 120, 1))
	testPlace(orderBook, 203, "cmd-sell-3", 1, newPlaceCmd("sell-3", Limit, Sell, 130, 1))

	assert.Equal(t, int64(3), orderBook.askQueue.orderCount())
	assert.Equal(t, int64(3), orderBook.bidQueue.orderCount())

	return orderBook
}

func TestLimitOrders(t *testing.T) {
	t.Run("take all orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Verify initial LastCmdSeqID is 0
		assert.Equal(t, uint64(0), testOrderBook.LastCmdSeqID())

		// Send command with SeqID
		payload := &protocol.PlaceOrderParams{
			OrderID:   "buyAll",
			OrderType: Limit,
			Side:      Buy,
			Price:     "1000",
			Size:      "10",
		}
		bytes, _ := (payload).MarshalBinary()
		testOrderBook.processCommand(&InputEvent{
			Cmd: &protocol.Command{
				MarketID:  testOrderBook.marketID,
				CommandID: "cmd-buyAll",
				UserID:    300,
				Timestamp: 1,
				SeqID:     100,
				Type:      protocol.CmdPlaceOrder,
				Payload:   bytes,
			},
		})

		// Verify LastCmdSeqID was updated
		assert.Equal(t, uint64(100), testOrderBook.LastCmdSeqID())

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 10, memoryPublishTrader.Count())

		assert.Equal(t, int64(0), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(4), testOrderBook.bidQueue.depthCount())

		// Verify Match Logs
		match1 := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeMatch, match1.Type)
		assert.Equal(t, "sell-1", match1.MakerOrderID)
		assert.Equal(t, uint64(201), match1.MakerUserID)
		assert.Equal(t, "buyAll", match1.OrderID)
		assert.Equal(t, uint64(300), match1.UserID)

		match2 := memoryPublishTrader.Get(7)
		assert.Equal(t, protocol.LogTypeMatch, match2.Type)
		assert.Equal(t, "sell-2", match2.MakerOrderID)
		assert.Equal(t, uint64(202), match2.MakerUserID)

		match3 := memoryPublishTrader.Get(8)
		assert.Equal(t, protocol.LogTypeMatch, match3.Type)
		assert.Equal(t, "sell-3", match3.MakerOrderID)
		assert.Equal(t, uint64(203), match3.MakerUserID)
	})

	t.Run("MatchLimitOrder", func(t *testing.T) {
		orderBook := createTestOrderBook(t)
		testPlace(orderBook, 204, "cmd-match-1", 1, newPlaceCmd("match-1", Limit, Sell, 90, 1))

		memoryPublishTrader, ok := orderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())
	})

	t.Run("AddLimitOrder", func(t *testing.T) {
		orderBook := createTestOrderBook(t)
		testPlace(orderBook, 104, "cmd-new-1", 1, &protocol.PlaceOrderParams{
			OrderID:   "new-1",
			OrderType: Limit,
			Side:      Buy,
			Price:     "100",
			Size:      "1",
		})

		memoryPublishTrader, ok := orderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())
		assert.Equal(t, int64(3), orderBook.askQueue.depthCount())
		assert.Equal(t, int64(4), orderBook.bidQueue.depthCount())

		// Verify the new order is in the book
		val, found := orderBook.findOrder("new-1")
		assert.True(t, found)
		assert.NotNil(t, val)
	})

	t.Run("take some orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(
			testOrderBook,
			204,
			"cmd-sell-match",
			1,
			newPlaceCmd("sell-match", Limit, Sell, 80, 2.5),
		)

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 9, memoryPublishTrader.Count())
		assert.Equal(t, int64(4), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(1), testOrderBook.bidQueue.depthCount())

		// Verify Match Logs
		match1 := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeMatch, match1.Type)
		assert.Equal(t, "buy-1", match1.MakerOrderID)
		assert.Equal(t, uint64(101), match1.MakerUserID)
		assert.Equal(t, "sell-match", match1.OrderID)
		assert.Equal(t, uint64(204), match1.UserID)

		match2 := memoryPublishTrader.Get(7)
		assert.Equal(t, protocol.LogTypeMatch, match2.Type)
		assert.Equal(t, "buy-2", match2.MakerOrderID)
		assert.Equal(t, uint64(102), match2.MakerUserID)
	})

	t.Run("take all orders and finish as cancel because of price", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(
			testOrderBook,
			204,
			"cmd-sell-match",
			1,
			newPlaceCmd("sell-match", IOC, Sell, 75, 4),
		)

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 9, memoryPublishTrader.Count())
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(1), testOrderBook.bidQueue.depthCount())
	})
}

func TestMarketOrder(t *testing.T) {
	t.Run("take all orders using quote size", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 2, "cmd-market-buy", 1, &protocol.PlaceOrderParams{
			OrderID:   orderIDMarketBuy,
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      "0",
			QuoteSize: "360",
		})

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 9, memoryPublishTrader.Count())
		assert.Equal(t, int64(0), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})

	t.Run("take some orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-mysell", 1, &protocol.PlaceOrderParams{
			OrderID:   "mysell",
			OrderType: Market,
			Side:      Sell,
			Price:     "0",
			Size:      "0",
			QuoteSize: "90",
		})

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(2), testOrderBook.bidQueue.depthCount())
	})

	t.Run("QuoteSize mode - buy with quote amount", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-quote-buy", 1, &protocol.PlaceOrderParams{
			OrderID:   "market-quote-buy",
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      "0",
			QuoteSize: "230",
		})

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 8, memoryPublishTrader.Count())

		match1 := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeMatch, match1.Type)
		assert.Equal(t, "110", match1.Price.String())
		assert.Equal(t, "1", match1.Size.String())
		assert.Equal(t, "110", match1.Amount.String())

		match2 := memoryPublishTrader.Get(7)
		assert.Equal(t, protocol.LogTypeMatch, match2.Type)
		assert.Equal(t, "120", match2.Price.String())
		assert.Equal(t, "1", match2.Size.String())
		assert.Equal(t, "120", match2.Amount.String())
	})

	t.Run("QuoteSize mode - partial fill of maker order", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-quote-partial", 1, &protocol.PlaceOrderParams{
			OrderID:   "market-quote-partial",
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      "0",
			QuoteSize: "55",
		})

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())

		match := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeMatch, match.Type)
		assert.Equal(t, "110", match.Price.String())
		assert.Equal(t, "0.5", match.Size.String())
		assert.Equal(t, "55", match.Amount.String())
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
	})

	t.Run("QuoteSize mode - no liquidity rejection", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := newOrderBook("test-engine", "BTC-USDT", publishTrader)
		testPlace(orderBook, 0, "cmd-quote-no-liq", 1, &protocol.PlaceOrderParams{
			OrderID:   "market-quote-no-liq",
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      "0",
			QuoteSize: "100",
		})

		assert.Equal(t, 1, publishTrader.Count())
		log := publishTrader.Get(0)
		assert.Equal(t, protocol.LogTypeReject, log.Type)
		assert.Equal(t, protocol.RejectReasonNoLiquidity, log.RejectReason)
		assert.Equal(t, "100", log.Size.String())
	})

	t.Run("Size mode - buy with base quantity", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-base-buy", 1, &protocol.PlaceOrderParams{
			OrderID:   "market-base-buy",
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      "2",
		})

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 8, memoryPublishTrader.Count())

		match1 := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeMatch, match1.Type)
		assert.Equal(t, "110", match1.Price.String())
		assert.Equal(t, "1", match1.Size.String())
		assert.Equal(t, "110", match1.Amount.String())

		match2 := memoryPublishTrader.Get(7)
		assert.Equal(t, protocol.LogTypeMatch, match2.Type)
		assert.Equal(t, "120", match2.Price.String())
		assert.Equal(t, "1", match2.Size.String())
		assert.Equal(t, "120", match2.Amount.String())
	})

	t.Run("Size mode - partial fill of maker order", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-base-partial", 1, &protocol.PlaceOrderParams{
			OrderID:   "market-base-partial",
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      udecimal.MustFromFloat64(0.5).String(),
		})

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())

		match := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeMatch, match.Type)
		assert.Equal(t, "110", match.Price.String())
		assert.Equal(t, "0.5", match.Size.String())
		assert.Equal(t, "55", match.Amount.String())
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
	})

	t.Run("Size mode - take all orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-base-all", 1, &protocol.PlaceOrderParams{
			OrderID:   "market-base-all",
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      "3",
		})

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 9, memoryPublishTrader.Count())
		assert.Equal(t, int64(0), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})
}

func TestPostOnlyOrder(t *testing.T) {
	t.Run("place a post only order", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-post-only", 1, &protocol.PlaceOrderParams{
			OrderID:   "post_only",
			OrderType: PostOnly,
			Side:      Buy,
			Price:     "100",
			Size:      "1",
		})

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(4), testOrderBook.bidQueue.depthCount())
	})

	t.Run("fail to place a post only order", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(
			testOrderBook,
			0,
			"cmd-post-only",
			1,
			newPlaceCmd("post_only", PostOnly, Buy, 115, 1),
		)

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, trade.Type)
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})
}

func TestIOCOrder(t *testing.T) {
	t.Run("no match any orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-ioc", 1, newPlaceCmd("ioc", IOC, Buy, 100, 1))

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, trade.Type)
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})

	t.Run("take all orders with no error", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-ioc", 1, newPlaceCmd("ioc", IOC, Buy, 1000, 3))

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 9, memoryPublishTrader.Count())
		assert.Equal(t, int64(0), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})

	t.Run("take all orders and finish as cancel", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-ioc", 1, newPlaceCmd("ioc", IOC, Sell, 10, 4))

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 10, memoryPublishTrader.Count())
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(0), testOrderBook.bidQueue.depthCount())
	})

	t.Run("take some orders and finish as cancel", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-ioc", 1, newPlaceCmd("ioc", IOC, Buy, 115, 2))

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 8, memoryPublishTrader.Count())
		assert.Equal(t, int64(2), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})
}

func TestFOKOrder(t *testing.T) {
	t.Run("no match any orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-fok", 1, newPlaceCmd("fok", FOK, Buy, 100, 1))

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, trade.Type)
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})

	t.Run("take all orders with no error", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-fok", 1, newPlaceCmd("fok", FOK, Buy, 1000, 3))

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 9, memoryPublishTrader.Count())
		assert.Equal(t, int64(0), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})

	t.Run("take all orders and finish as cancel", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-fok", 1, newPlaceCmd("fok", FOK, Sell, 10, 4))

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, trade.Type)
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})

	t.Run("take some orders and finish as cancel", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-fok", 1, newPlaceCmd("fok", FOK, Buy, 115, 2))

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, trade.Type)
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})

	t.Run("multiple orders at same price level", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := newOrderBook("test-engine", "BTC-USDT", publishTrader)

		testPlace(orderBook, 201, "cmd-sell-1", 1, newPlaceCmd("sell-1", Limit, Sell, 110, 3))
		testPlace(orderBook, 202, "cmd-sell-2", 1, newPlaceCmd("sell-2", Limit, Sell, 110, 2))
		assert.Equal(t, int64(2), orderBook.askQueue.orderCount())

		testPlace(orderBook, 301, "cmd-fok-buy", 1, newPlaceCmd("fok-buy", FOK, Buy, 115, 5))

		memoryPublishTrader, ok := orderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 4, memoryPublishTrader.Count())

		log1 := memoryPublishTrader.Get(2)
		log2 := memoryPublishTrader.Get(3)
		assert.Equal(t, protocol.LogTypeMatch, log1.Type, "Third log should be Match")
		assert.Equal(t, protocol.LogTypeMatch, log2.Type, "Fourth log should be Match")
		assert.Equal(t, int64(0), orderBook.askQueue.depthCount())
	})

	t.Run("cross multiple price levels", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := newOrderBook("test-engine", "BTC-USDT", publishTrader)

		testPlace(orderBook, 201, "cmd-sell-1", 1, newPlaceCmd("sell-1", Limit, Sell, 110, 2))
		testPlace(orderBook, 202, "cmd-sell-2", 1, newPlaceCmd("sell-2", Limit, Sell, 120, 3))
		assert.Equal(t, int64(2), orderBook.askQueue.depthCount())

		testPlace(orderBook, 301, "cmd-fok-buy", 1, newPlaceCmd("fok-buy", FOK, Buy, 125, 5))

		memoryPublishTrader, ok := orderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 4, memoryPublishTrader.Count())
		assert.Equal(t, int64(0), orderBook.askQueue.depthCount())
	})

	t.Run("exact size match at price level", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := newOrderBook("test-engine", "BTC-USDT", publishTrader)

		for i := 1; i <= 3; i++ {
			testPlace(
				orderBook,
				/* #nosec G115 */
				uint64(200+i),
				"cmd-sell-"+udecimal.MustFromInt64(int64(i), 0).String(),
				1,
				newPlaceCmd(
					"sell-"+udecimal.MustFromInt64(int64(i), 0).String(),
					Limit,
					Sell,
					110,
					1,
				),
			)
		}
		assert.Equal(t, int64(1), orderBook.askQueue.depthCount()) // 3 at same price = depth 1

		testPlace(orderBook, 301, "cmd-fok-buy", 1, newPlaceCmd("fok-buy", FOK, Buy, 115, 3))

		memoryPublishTrader, ok := orderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 6, memoryPublishTrader.Count())
		assert.Equal(
			t,
			int64(0),
			orderBook.askQueue.depthCount(),
			"All sell orders should be matched",
		)
	})
}

func TestCancelOrder(t *testing.T) {
	testOrderBook := createTestOrderBook(t)

	testCancel(testOrderBook, 201, "cmd-cancel-sell-1", 1, newCancelCmd("sell-1"))
	assert.Equal(t, int64(2), testOrderBook.askQueue.depthCount())

	testCancel(testOrderBook, 101, "cmd-cancel-buy-1", 1, newCancelCmd("buy-1"))
	assert.Equal(t, int64(2), testOrderBook.bidQueue.depthCount())

	testCancel(testOrderBook, 999, "cmd-cancel-aaaaaa", 1, newCancelCmd("aaaaaa"))
	assert.Equal(t, int64(2), testOrderBook.bidQueue.depthCount())
}

func TestAmendOrder(t *testing.T) {
	t.Run("decrease size preserves priority", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		testAmend(testOrderBook, 101, "cmd-amend-buy-1", 1, newAmendCmd("buy-1", 90, 0.5))

		depth := testOrderBook.depth(10)
		assert.Equal(t, "0.5", depth.Bids[0].Size)

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeAmend, log.Type)
		assert.Equal(t, "0.5", log.Size.String())
		assert.Equal(t, "1", log.OldSize.String())

		// Verify Priority: Add another order at same price, match against them.
		testPlace(testOrderBook, 401, "cmd-buy-new", 1, newPlaceCmd("buy-new", Limit, Buy, 90, 1))
		testPlace(
			testOrderBook,
			402,
			"cmd-sell-match",
			1,
			newPlaceCmd("sell-match", Limit, Sell, 90, 0.5),
		)

		assert.Equal(t, 9, memoryPublishTrader.Count())
		matchLog := memoryPublishTrader.Get(8)
		assert.Equal(t, protocol.LogTypeMatch, matchLog.Type)
		assert.Equal(t, "buy-1", matchLog.MakerOrderID) // Priority kept!
		assert.Equal(t, uint64(101), matchLog.MakerUserID)
		assert.Equal(t, "sell-match", matchLog.OrderID)
		assert.Equal(t, uint64(402), matchLog.UserID)
	})

	t.Run("increase size loses priority", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		testPlace(
			testOrderBook,
			0,
			"cmd-buy-2-compete",
			1,
			newPlaceCmd("buy-2-compete", Limit, Buy, 90, 1),
		)
		testAmend(testOrderBook, 101, "cmd-amend-buy-1", 1, newAmendCmd("buy-1", 90, 2))
		testPlace(
			testOrderBook,
			0,
			"cmd-sell-match",
			1,
			newPlaceCmd("sell-match", Limit, Sell, 90, 1),
		)

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		found := false
		for i := range memoryPublishTrader.Count() {
			log := memoryPublishTrader.Get(i)
			if log.Type == protocol.LogTypeMatch && log.OrderID == "sell-match" {
				assert.Equal(t, "buy-2-compete", log.MakerOrderID) // Priority lost!
				found = true
				break
			}
		}
		assert.True(t, found, "Match log not found")
	})

	t.Run("change price moves level", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		testAmend(testOrderBook, 101, "cmd-amend-buy-1", 1, newAmendCmd("buy-1", 95, 1))

		depth := testOrderBook.depth(10)
		assert.Equal(t, "95", depth.Bids[0].Price)
		assert.Equal(t, "1", depth.Bids[0].Size)
		assert.Equal(t, "80", depth.Bids[1].Price)
	})

	t.Run("change price and size simultaneously", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		testAmend(
			testOrderBook,
			101,
			"cmd-amend-buy-1",
			1,
			&protocol.AmendOrderParams{OrderID: "buy-1", NewPrice: "95", NewSize: "5"},
		)

		depth := testOrderBook.depth(10)
		assert.Equal(t, "95", depth.Bids[0].Price)
		assert.Equal(t, "5", depth.Bids[0].Size)

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeAmend, log.Type)
		assert.Equal(t, "95", log.Price.String())
		assert.Equal(t, "5", log.Size.String())
		assert.Equal(t, "90", log.OldPrice.String())
		assert.Equal(t, "1", log.OldSize.String())
	})

	t.Run("amend order crosses spread and matches", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		testAmend(testOrderBook, 101, "cmd-amend-buy-1", 1, newAmendCmd("buy-1", 115, 2))

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 9, memoryPublishTrader.Count())

		amendLog := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeAmend, amendLog.Type)
		assert.Equal(t, "115", amendLog.Price.String())
		assert.Equal(t, "2", amendLog.Size.String())

		matchLog := memoryPublishTrader.Get(7)
		assert.Equal(t, protocol.LogTypeMatch, matchLog.Type)
		assert.Equal(t, "buy-1", matchLog.OrderID)
		assert.Equal(t, "sell-1", matchLog.MakerOrderID)
		assert.Equal(t, "110", matchLog.Price.String())
		assert.Equal(t, "1", matchLog.Size.String())

		openLog := memoryPublishTrader.Get(8)
		assert.Equal(t, protocol.LogTypeOpen, openLog.Type)
		assert.Equal(t, "buy-1", openLog.OrderID)
		assert.Equal(t, "115", openLog.Price.String())
		assert.Equal(t, "1", openLog.Size.String())

		depth := testOrderBook.depth(10)
		assert.Equal(t, "115", depth.Bids[0].Price)
		assert.Equal(t, "1", depth.Bids[0].Size)
	})
}

func TestDepth(t *testing.T) {
	testOrderBook := createTestOrderBook(t)

	result := testOrderBook.depth(5)
	assert.Len(t, result.Asks, 3)
	assert.Len(t, result.Bids, 3)
}

func TestRejectReason(t *testing.T) {
	t.Run("IOC no liquidity", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := newOrderBook("test-engine", "BTC-USDT", publishTrader)
		testPlace(orderBook, 0, "cmd-ioc-1", 1, &protocol.PlaceOrderParams{
			OrderID:   "ioc-1",
			OrderType: IOC,
			Side:      Buy,
			Price:     "100",
			Size:      "1",
		})

		assert.Equal(t, 1, publishTrader.Count())
		log := publishTrader.Get(0)
		assert.Equal(t, protocol.LogTypeReject, log.Type)
		assert.Equal(t, protocol.RejectReasonNoLiquidity, log.RejectReason)
	})

	t.Run("IOC price mismatch", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-ioc-price", 1, newPlaceCmd("ioc-price", IOC, Buy, 100, 1))

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, log.Type)
		assert.Equal(t, protocol.RejectReasonPriceMismatch, log.RejectReason)
	})

	t.Run("FOK insufficient size", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-fok-size", 1, newPlaceCmd("fok-size", FOK, Buy, 1000, 10))

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, log.Type)
		assert.Equal(t, protocol.RejectReasonInsufficientSize, log.RejectReason)
	})

	t.Run("FOK price mismatch", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-fok-price", 1, newPlaceCmd("fok-price", FOK, Buy, 100, 1))

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, log.Type)
		assert.Equal(t, protocol.RejectReasonPriceMismatch, log.RejectReason)
	})

	t.Run("PostOnly would cross spread", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(
			testOrderBook,
			0,
			"cmd-post-only-cross",
			1,
			newPlaceCmd("post-only-cross", PostOnly, Buy, 115, 1),
		)

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, log.Type)
		assert.Equal(t, protocol.RejectReasonPostOnlyMatch, log.RejectReason)
	})

	t.Run("Market no liquidity", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := newOrderBook("test-engine", "BTC-USDT", publishTrader)
		testPlace(orderBook, 0, "cmd-market-1", 1, newPlaceCmd("market-1", Market, Buy, 0, 100))

		assert.Equal(t, 1, publishTrader.Count())
		log := publishTrader.Get(0)
		assert.Equal(t, protocol.LogTypeReject, log.Type)
		assert.Equal(t, protocol.RejectReasonNoLiquidity, log.RejectReason)
	})
}

func TestMatchAmount(t *testing.T) {
	t.Run("exact size match at price level", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		testOrderBook := newOrderBook("test-engine", "BTC-USDT", publishTrader)
		testPlace(testOrderBook, 204, "cmd-sell-1", 1, newPlaceCmd("sell-1", Limit, Sell, 80, 3))
		testPlace(testOrderBook, 104, "cmd-buy-fok", 1, newPlaceCmd("buy-fok", FOK, Buy, 80, 3))

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 2, memoryPublishTrader.Count())

		matchLog := memoryPublishTrader.Get(1)
		assert.Equal(t, protocol.LogTypeMatch, matchLog.Type)
		assert.Equal(t, "80", matchLog.Price.String())
		assert.Equal(t, "3", matchLog.Size.String())
		assert.Equal(t, "240", matchLog.Amount.String())
	})

	t.Run("multiple orders at same price level", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		testOrderBook := newOrderBook("test-engine", "BTC-USDT", publishTrader)
		testPlace(testOrderBook, 204, "cmd-sell-1", 1, newPlaceCmd("sell-1", Limit, Sell, 80, 1))
		testPlace(testOrderBook, 205, "cmd-sell-2", 1, newPlaceCmd("sell-2", Limit, Sell, 80, 1))
		testPlace(testOrderBook, 104, "cmd-buy-fok", 1, newPlaceCmd("buy-fok", FOK, Buy, 80, 2))

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 4, memoryPublishTrader.Count())

		match1 := memoryPublishTrader.Get(2)
		assert.Equal(t, protocol.LogTypeMatch, match1.Type)
		assert.Equal(t, "80", match1.Price.String())
		assert.Equal(t, "1", match1.Size.String())
		assert.Equal(t, "80", match1.Amount.String())

		match2 := memoryPublishTrader.Get(3)
		assert.Equal(t, protocol.LogTypeMatch, match2.Type)
		assert.Equal(t, "80", match2.Price.String())
		assert.Equal(t, "1", match2.Size.String())
		assert.Equal(t, "80", match2.Amount.String())
	})

	t.Run("multiple matches across price levels", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		testOrderBook := newOrderBook("test-engine", "BTC-USDT", publishTrader)
		testPlace(testOrderBook, 201, "cmd-sell-1", 1, newPlaceCmd("sell-1", Limit, Sell, 110, 1))
		testPlace(testOrderBook, 202, "cmd-sell-2", 1, newPlaceCmd("sell-2", Limit, Sell, 120, 1))
		testPlace(testOrderBook, 203, "cmd-sell-3", 1, newPlaceCmd("sell-3", Limit, Sell, 130, 1))
		testPlace(testOrderBook, 300, "cmd-buy-all", 1, &protocol.PlaceOrderParams{
			OrderID:   "buy-all",
			OrderType: Limit,
			Side:      Buy,
			Price:     "1000",
			Size:      "3",
		})

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 6, memoryPublishTrader.Count())

		match1 := memoryPublishTrader.Get(3)
		assert.Equal(t, protocol.LogTypeMatch, match1.Type)
		assert.Equal(t, "110", match1.Amount.String())

		match2 := memoryPublishTrader.Get(4)
		assert.Equal(t, protocol.LogTypeMatch, match2.Type)
		assert.Equal(t, "120", match2.Amount.String())

		match3 := memoryPublishTrader.Get(5)
		assert.Equal(t, protocol.LogTypeMatch, match3.Type)
		assert.Equal(t, "130", match3.Amount.String())
	})
}

func TestTradeID(t *testing.T) {
	t.Run("trade ID only on match events", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)

		for i := range 6 {
			log := memoryPublishTrader.Get(i)
			assert.Equal(t, protocol.LogTypeOpen, log.Type)
			assert.Equal(t, uint64(0), log.TradeID, "Open event should have TradeID 0")
		}

		testPlace(testOrderBook, 0, "cmd-buy-match", 1, &protocol.PlaceOrderParams{
			OrderID:   "buy-match",
			OrderType: Limit,
			Side:      Buy,
			Price:     "115",
			Size:      "1",
		})

		assert.Equal(t, 7, memoryPublishTrader.Count())
		matchLog := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeMatch, matchLog.Type)
		assert.Positive(t, matchLog.TradeID, "Match event should have TradeID > 0")
	})

	t.Run("trade ID sequential", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "cmd-buy-all", 1, &protocol.PlaceOrderParams{
			OrderID:   "buy-all",
			OrderType: Limit,
			Side:      Buy,
			Price:     "1000",
			Size:      "3",
		})

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 9, memoryPublishTrader.Count())

		match1 := memoryPublishTrader.Get(6)
		match2 := memoryPublishTrader.Get(7)
		match3 := memoryPublishTrader.Get(8)

		assert.Equal(t, protocol.LogTypeMatch, match1.Type)
		assert.Equal(t, protocol.LogTypeMatch, match2.Type)
		assert.Equal(t, protocol.LogTypeMatch, match3.Type)

		assert.Equal(t, match1.TradeID+1, match2.TradeID, "TradeIDs should be sequential")
		assert.Equal(t, match2.TradeID+1, match3.TradeID, "TradeIDs should be sequential")
	})

	t.Run("reject events have no trade ID", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		testPlace(testOrderBook, 0, "fok-reject", 1, &protocol.PlaceOrderParams{
			OrderID:   "fok-reject",
			OrderType: FOK,
			Side:      Buy,
			Price:     "100",
			Size:      "1",
		})

		memoryPublishTrader, ok := testOrderBook.publisher.(*MemoryPublishLog)
		require.True(t, ok)
		assert.Equal(t, 7, memoryPublishTrader.Count())
		rejectLog := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, rejectLog.Type)
		assert.Equal(t, uint64(0), rejectLog.TradeID, "Reject event should have TradeID 0")
	})
}

func TestOrderBookSnapshotRestore(t *testing.T) {
	t.Run("Snapshot and Restore maintain state", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		book := newOrderBook("test-engine", "BTC-USDT", publishTrader)

		testPlace(book, 1, "cmd-bid-1", 1, &protocol.PlaceOrderParams{
			OrderID:   "bid-1",
			OrderType: Limit,
			Side:      Buy,
			Price:     "100",
			Size:      "10",
		})
		testPlace(book, 2, "cmd-ask-1", 2, &protocol.PlaceOrderParams{
			OrderID:   "ask-1",
			OrderType: Limit,
			Side:      Sell,
			Price:     "110",
			Size:      "5",
		})

		assert.Equal(t, int64(1), book.bidQueue.orderCount())
		assert.Equal(t, int64(1), book.askQueue.orderCount())

		// Take Snapshot directly
		snap := book.createSnapshot()
		assert.NotNil(t, snap)
		assert.Equal(t, "BTC-USDT", snap.MarketID)
		assert.Len(t, snap.Bids, 1)
		assert.Len(t, snap.Asks, 1)
		assert.Equal(t, "bid-1", snap.Bids[0].ID)
		assert.Equal(t, "ask-1", snap.Asks[0].ID)
		assert.Positive(t, snap.SeqID)

		// Create a NEW OrderBook and Restore
		restoredBook := newOrderBook("test-engine", "BTC-USDT", NewMemoryPublishLog())
		restoredBook.Restore(snap)

		assert.Equal(t, int64(1), restoredBook.bidQueue.orderCount())
		assert.Equal(t, int64(1), restoredBook.askQueue.orderCount())

		bid := restoredBook.bidQueue.order("bid-1")
		assert.NotNil(t, bid)
		assert.Equal(t, "100", bid.Price.String())

		ask := restoredBook.askQueue.order("ask-1")
		assert.NotNil(t, ask)
		assert.Equal(t, "110", ask.Price.String())

		// Add a matching order to prove continuity
		testPlace(restoredBook, 0, "cmd-buy-match", 1, &protocol.PlaceOrderParams{
			OrderID:   "buy-match",
			OrderType: Limit,
			Side:      Buy,
			Price:     "115",
			Size:      "1",
		})

		assert.Equal(t, int64(1), restoredBook.askQueue.orderCount())
		assert.Equal(t, int64(1), restoredBook.bidQueue.orderCount())

		askAfter := restoredBook.askQueue.order("ask-1")
		assert.Equal(t, "4", askAfter.Size.String())
	})
}

func TestOrderValidation(t *testing.T) {
	publishTrader := NewMemoryPublishLog()
	orderBook := newOrderBook("test-engine", "BTC-USDT", publishTrader)

	t.Run("RejectDuplicateOrderID", func(t *testing.T) {
		testPlace(orderBook, 1, "cmd-dup-id", 1, newPlaceCmd("dup-id", Limit, Buy, 100, 1))
		testPlace(orderBook, 1, "cmd-dup-id-2", 1, newPlaceCmd("dup-id", Limit, Buy, 100, 1))

		logs := publishTrader.Logs()
		found := false
		for _, log := range logs {
			if log.Type == protocol.LogTypeReject && log.OrderID == "dup-id" &&
				log.RejectReason == protocol.RejectReasonDuplicateID {
				found = true
			}
		}
		assert.True(t, found)
	})

	t.Run("RejectCancelNonExistentOrder", func(t *testing.T) {
		testCancel(
			orderBook,
			1,
			"cmd-cancel-non-existent",
			1,
			&protocol.CancelOrderParams{OrderID: "non-existent"},
		)

		logs := publishTrader.Logs()
		found := false
		for _, log := range logs {
			if log.Type == protocol.LogTypeReject && log.OrderID == "non-existent" &&
				log.RejectReason == protocol.RejectReasonOrderNotFound {
				found = true
			}
		}
		assert.True(t, found)
	})

	t.Run("RejectAmendNonExistentOrder", func(t *testing.T) {
		testAmend(
			orderBook,
			1,
			"cmd-amend-non-existent",
			1,
			&protocol.AmendOrderParams{
				OrderID:  "non-existent",
				NewPrice: "100",
				NewSize:  "2",
			},
		)

		logs := publishTrader.Logs()
		found := false
		for _, log := range logs {
			if log.Type == protocol.LogTypeReject && log.OrderID == "non-existent" &&
				log.RejectReason == protocol.RejectReasonOrderNotFound {
				found = true
			}
		}
		assert.True(t, found)
	})

	t.Run("RejectUserIDMismatch", func(t *testing.T) {
		testPlace(orderBook, 1, "cmd-owner-1", 1, &protocol.PlaceOrderParams{
			OrderID:   "owner-1",
			OrderType: Limit,
			Side:      Buy,
			Size:      "1",
			Price:     "100",
		})

		assert.NotNil(t, orderBook.bidQueue.order("owner-1"))

		testCancel(orderBook, 2, "cmd-cancel-owner-1", 1, &protocol.CancelOrderParams{
			OrderID: "owner-1",
		})
		testAmend(orderBook, 2, "cmd-amend-owner-1", 1, &protocol.AmendOrderParams{
			OrderID:  "owner-1",
			NewPrice: "110",
			NewSize:  "1",
		})

		logs := publishTrader.Logs()
		cancelRejected := false
		amendRejected := false
		for _, log := range logs {
			if log.Type == protocol.LogTypeReject && log.OrderID == "owner-1" && log.UserID == 2 &&
				log.RejectReason == protocol.RejectReasonOrderNotFound {
				if !cancelRejected {
					cancelRejected = true
					continue
				}
				amendRejected = true
			}
		}
		assert.True(t, cancelRejected && amendRejected)
	})

	t.Run("RejectAmendInvalidPricePayload", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		book := newOrderBook("test-engine", "BTC-USDT", publishTrader)

		testPlace(book, 9, "cmd-amend-invalid-price", 1, &protocol.PlaceOrderParams{
			OrderID:   "amend-invalid-price",
			OrderType: Limit,
			Side:      Buy,
			Price:     "100",
			Size:      "2",
		})

		testAmend(book, 9, "cmd-amend-invalid-price-2", 777, &protocol.AmendOrderParams{
			OrderID:  "amend-invalid-price",
			NewPrice: "bad-price",
			NewSize:  "2",
		})

		order := book.bidQueue.order("amend-invalid-price")
		require.NotNil(t, order)
		assert.Equal(t, "100", order.Price.String())

		found := false
		for _, log := range publishTrader.Logs() {
			if log.Type == protocol.LogTypeReject && log.OrderID == "amend-invalid-price" {
				found = log.RejectReason == protocol.RejectReasonInvalidPayload &&
					log.Timestamp == 777
			}
		}
		assert.True(t, found)
	})

	t.Run("RejectAmendInvalidSizePayload", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		book := newOrderBook("test-engine", "BTC-USDT", publishTrader)

		testPlace(book, 9, "cmd-amend-invalid-size", 1, &protocol.PlaceOrderParams{
			OrderID:   "amend-invalid-size",
			OrderType: Limit,
			Side:      Buy,
			Price:     "100",
			Size:      "2",
		})

		testAmend(book, 9, "cmd-amend-invalid-size-2", 778, &protocol.AmendOrderParams{
			OrderID:  "amend-invalid-size",
			NewPrice: "100",
			NewSize:  "bad-size",
		})

		order := book.bidQueue.order("amend-invalid-size")
		require.NotNil(t, order)
		assert.Equal(t, "2", order.Size.String())

		found := false
		for _, log := range publishTrader.Logs() {
			if log.Type == protocol.LogTypeReject && log.OrderID == "amend-invalid-size" {
				found = log.RejectReason == protocol.RejectReasonInvalidPayload &&
					log.Timestamp == 778
			}
		}
		assert.True(t, found)
	})

	t.Run("RejectInvalidPlacePayloadUsesCommandTimestamp", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		book := newOrderBook("test-engine", "BTC-USDT", publishTrader)

		payload := &protocol.PlaceOrderParams{
			OrderID:   "bad-place",
			OrderType: Limit,
			Side:      Buy,
			Price:     "not-a-decimal",
			Size:      "1",
		}
		bytes, err := (payload).MarshalBinary()
		require.NoError(t, err)

		book.processCommand(&InputEvent{
			Cmd: &protocol.Command{
				MarketID:  "BTC-USDT",
				UserID:    5,
				CommandID: "cmd-bad-place",
				Timestamp: 999,
				Type:      protocol.CmdPlaceOrder,
				Payload:   bytes[:1],
			},
		})

		logs := publishTrader.Logs()
		require.Len(t, logs, 1)
		assert.Equal(t, protocol.LogTypeReject, logs[0].Type)
		assert.Equal(t, "unknown", logs[0].OrderID)
		assert.Equal(t, protocol.RejectReasonInvalidPayload, logs[0].RejectReason)
		assert.Equal(t, int64(999), logs[0].Timestamp)
	})

	t.Run("RejectPlaceWithoutTimestamp", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		book := newOrderBook("test-engine", "BTC-USDT", publishTrader)

		testPlace(book, 55, "cmd-missing-ts", 0, &protocol.PlaceOrderParams{
			OrderID:   "missing-place-timestamp",
			OrderType: Limit,
			Side:      Buy,
			Price:     "100",
			Size:      "1",
		})

		logs := publishTrader.Logs()
		require.Len(t, logs, 1)
		assert.Equal(t, protocol.LogTypeReject, logs[0].Type)
		assert.Equal(t, "missing-place-timestamp", logs[0].OrderID)
		assert.Equal(t, uint64(55), logs[0].UserID)
		assert.Equal(t, protocol.RejectReasonInvalidPayload, logs[0].RejectReason)
		assert.Nil(t, book.bidQueue.order("missing-place-timestamp"))
	})

	t.Run("RejectCancelWithoutTimestamp", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		book := newOrderBook("test-engine", "BTC-USDT", publishTrader)

		testPlace(book, 56, "cmd-cancel-ts", 1, &protocol.PlaceOrderParams{
			OrderID:   "cancel-without-timestamp",
			OrderType: Limit,
			Side:      Buy,
			Price:     "100",
			Size:      "1",
		})

		testCancel(book, 56, "cmd-cancel-ts-2", 0, &protocol.CancelOrderParams{
			OrderID: "cancel-without-timestamp",
		})

		order := book.bidQueue.order("cancel-without-timestamp")
		require.NotNil(t, order)

		found := false
		for _, log := range publishTrader.Logs() {
			if log.Type == protocol.LogTypeReject && log.OrderID == "cancel-without-timestamp" {
				found = log.UserID == 56 && log.RejectReason == protocol.RejectReasonInvalidPayload
			}
		}
		assert.True(t, found)
	})

	t.Run("RejectAmendWithoutTimestamp", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		book := newOrderBook("test-engine", "BTC-USDT", publishTrader)

		testPlace(book, 57, "cmd-amend-ts", 1, &protocol.PlaceOrderParams{
			OrderID:   "amend-without-timestamp",
			OrderType: Limit,
			Side:      Buy,
			Price:     "100",
			Size:      "2",
		})

		testAmend(book, 57, "cmd-amend-ts-2", 0, &protocol.AmendOrderParams{
			OrderID:  "amend-without-timestamp",
			NewPrice: "100",
			NewSize:  "1",
		})

		order := book.bidQueue.order("amend-without-timestamp")
		require.NotNil(t, order)
		assert.Equal(t, "2", order.Size.String())

		found := false
		for _, log := range publishTrader.Logs() {
			if log.Type == protocol.LogTypeReject && log.OrderID == "amend-without-timestamp" {
				found = log.UserID == 57 && log.RejectReason == protocol.RejectReasonInvalidPayload
			}
		}
		assert.True(t, found)
	})
}

func TestOrderBook_LotSize(t *testing.T) {
	t.Run("default LotSize is 1e-8", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := newOrderBook("test-engine", "BTC-USDT", publishTrader)
		assert.Equal(t, orderBook.lotSize, DefaultLotSize)
	})

	t.Run("custom LotSize via option", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		customLotSize := udecimal.MustParse("0.0001")
		orderBook := newOrderBook(
			"test-engine",
			"BTC-USDT",
			publishTrader,
			WithLotSize(customLotSize),
		)
		assert.Equal(t, orderBook.lotSize, customLotSize)
	})

	t.Run("Market order quote mode - reject when matchSize below LotSize", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		lotSize := udecimal.MustParse("0.001")
		orderBook := newOrderBook("test-engine", "BTC-USDT", publishTrader, WithLotSize(lotSize))

		testPlace(orderBook, 1, "cmd-sell-1", 1, &protocol.PlaceOrderParams{
			OrderID:   "sell-1",
			OrderType: Limit,
			Side:      Sell,
			Price:     "50000",
			Size:      "0.1",
		})
		assert.Equal(t, int64(1), orderBook.askQueue.orderCount())

		testPlace(orderBook, 0, "cmd-market-buy", 2, &protocol.PlaceOrderParams{
			OrderID:   orderIDMarketBuy,
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      "0",
			QuoteSize: udecimal.MustParse("0.04").String(),
		})

		logs := publishTrader.Logs()
		found := false
		for _, log := range logs {
			if log.Type == protocol.LogTypeReject && log.OrderID == orderIDMarketBuy {
				assert.Equal(t, "0.04", log.Size.String())
				found = true
			}
		}
		assert.True(t, found)
		assert.Equal(t, int64(1), orderBook.askQueue.orderCount())
	})

	t.Run("Market order quote mode - partial fill then reject remaining", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		lotSize := udecimal.MustParse("0.001")
		orderBook := newOrderBook("test-engine", "BTC-USDT", publishTrader, WithLotSize(lotSize))

		testPlace(orderBook, 0, "cmd-sell-1", 1, &protocol.PlaceOrderParams{
			OrderID:   "sell-1",
			OrderType: Limit,
			Side:      Sell,
			Size:      udecimal.MustParse("0.005").String(),
			Price:     "1000",
		})
		assert.Equal(t, int64(1), orderBook.askQueue.orderCount())

		testPlace(orderBook, 0, "cmd-market-buy", 2, &protocol.PlaceOrderParams{
			OrderID:   orderIDMarketBuy,
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      "0",
			QuoteSize: udecimal.MustParse("5.5").String(),
		})

		logs := publishTrader.Logs()
		matchFound := false
		rejectFound := false
		for _, log := range logs {
			if log.Type == protocol.LogTypeMatch && log.OrderID == orderIDMarketBuy {
				matchFound = true
			}
			if log.Type == protocol.LogTypeReject && log.OrderID == orderIDMarketBuy {
				rejectFound = true
			}
		}
		assert.True(t, matchFound && rejectFound)
	})
}
