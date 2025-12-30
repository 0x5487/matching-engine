package match

import (
	"context"
	"testing"
	"time"

	"github.com/0x5487/matching-engine/protocol"
	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
)

func newPlaceCmd(id string, ot OrderType, s Side, price, size float64, userID int64) *protocol.PlaceOrderCommand {
	return &protocol.PlaceOrderCommand{
		OrderID:   id,
		OrderType: ot,
		Side:      s,
		Price:     udecimal.MustFromFloat64(price).String(),
		Size:      udecimal.MustFromFloat64(size).String(),
		UserID:    userID,
	}
}

func newAmendCmd(id string, price, size float64, userID int64) *protocol.AmendOrderCommand {
	return &protocol.AmendOrderCommand{
		OrderID:  id,
		NewPrice: udecimal.MustFromFloat64(price).String(),
		NewSize:  udecimal.MustFromFloat64(size).String(),
		UserID:   userID,
	}
}

func newCancelCmd(id string, userID int64) *protocol.CancelOrderCommand {
	return &protocol.CancelOrderCommand{
		OrderID: id,
		UserID:  userID,
	}
}

func createTestOrderBook(t *testing.T) *OrderBook {
	ctx := context.Background()

	publishTrader := NewMemoryPublishLog()
	orderBook := NewOrderBook("BTC-USDT", publishTrader)
	go func() {
		_ = orderBook.Start()
	}()

	_ = orderBook.PlaceOrder(ctx, newPlaceCmd("buy-1", Limit, Buy, 90, 1, 101))
	_ = orderBook.PlaceOrder(ctx, newPlaceCmd("buy-2", Limit, Buy, 80, 1, 102))
	_ = orderBook.PlaceOrder(ctx, newPlaceCmd("buy-3", Limit, Buy, 70, 1, 103))
	_ = orderBook.PlaceOrder(ctx, newPlaceCmd("sell-1", Limit, Sell, 110, 1, 201))
	_ = orderBook.PlaceOrder(ctx, newPlaceCmd("sell-2", Limit, Sell, 120, 1, 202))
	_ = orderBook.PlaceOrder(ctx, newPlaceCmd("sell-3", Limit, Sell, 130, 1, 203))

	// Wait for all 6 orders to be processed
	assert.Eventually(t, func() bool {
		stats, err := orderBook.GetStats()
		return err == nil && stats.AskOrderCount == 3 && stats.BidOrderCount == 3
	}, 1*time.Second, 10*time.Millisecond)

	return orderBook
}

func TestLimitOrders(t *testing.T) {

	t.Run("take all orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Verify initial LastCmdSeqID is 0
		assert.Equal(t, uint64(0), testOrderBook.LastCmdSeqID())

		// Send command with SeqID
		payload := &protocol.PlaceOrderCommand{
			OrderID:   "buyAll",
			OrderType: Limit,
			Side:      Buy,
			Price:     "1000",
			Size:      "10",
			UserID:    300,
		}
		bytes, _ := testOrderBook.serializer.Marshal(payload)
		testOrderBook.ExecuteCommand(&protocol.Command{
			MarketID: testOrderBook.marketID,
			SeqID:    100,
			Type:     protocol.CmdPlaceOrder,
			Payload:  bytes,
		})

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 10
		}, 1*time.Second, 10*time.Millisecond)

		// Verify LastCmdSeqID was updated
		assert.Equal(t, uint64(100), testOrderBook.LastCmdSeqID())

		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(0), stats.AskDepthCount)
		assert.Equal(t, int64(4), stats.BidDepthCount)

		// Verify Match Logs
		match1 := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeMatch, match1.Type)
		assert.Equal(t, "sell-1", match1.MakerOrderID)
		assert.Equal(t, int64(201), match1.MakerUserID)
		assert.Equal(t, "buyAll", match1.OrderID)
		assert.Equal(t, int64(300), match1.UserID)

		match2 := memoryPublishTrader.Get(7)
		assert.Equal(t, protocol.LogTypeMatch, match2.Type)
		assert.Equal(t, "sell-2", match2.MakerOrderID)
		assert.Equal(t, int64(202), match2.MakerUserID)

		match3 := memoryPublishTrader.Get(8)
		assert.Equal(t, protocol.LogTypeMatch, match3.Type)
		assert.Equal(t, "sell-3", match3.MakerOrderID)
		assert.Equal(t, int64(203), match3.MakerUserID)
	})

	t.Run("MatchLimitOrder", func(t *testing.T) {
		orderBook := createTestOrderBook(t)
		_ = orderBook.PlaceOrder(context.Background(), newPlaceCmd("match-1", Limit, Sell, 90, 1, 204))

		memoryPublishTrader, _ := orderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)
	})

	t.Run("AddLimitOrder", func(t *testing.T) {
		orderBook := createTestOrderBook(t)
		order := &protocol.PlaceOrderCommand{
			OrderID:   "new-1",
			OrderType: Limit,
			Side:      Buy,
			Price:     "100",
			Size:      "1",
			UserID:    104,
		}
		err := orderBook.PlaceOrder(context.Background(), order)
		assert.NoError(t, err)

		memoryPublishTrader, _ := orderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		stats, err := orderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(3), stats.AskDepthCount)
		assert.Equal(t, int64(4), stats.BidDepthCount)

		// Verify the new order is in the book
		val, found := orderBook.findOrder("new-1")
		assert.True(t, found)
		assert.NotNil(t, val)
	})

	t.Run("take some orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("sell-match", Limit, Sell, 80, 2.5, 204))

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 9
		}, 1*time.Second, 10*time.Millisecond)

		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(4), stats.AskDepthCount)
		assert.Equal(t, int64(1), stats.BidDepthCount)

		// Verify Match Logs
		match1 := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeMatch, match1.Type)
		assert.Equal(t, "buy-1", match1.MakerOrderID)
		assert.Equal(t, int64(101), match1.MakerUserID)
		assert.Equal(t, "sell-match", match1.OrderID)
		assert.Equal(t, int64(204), match1.UserID)

		match2 := memoryPublishTrader.Get(7)
		assert.Equal(t, protocol.LogTypeMatch, match2.Type)
		assert.Equal(t, "buy-2", match2.MakerOrderID)
		assert.Equal(t, int64(102), match2.MakerUserID)
	})

	t.Run("take all orders and finish as cancel because of price", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Sell IOC at 75. Bids are 90, 80, 70.
		// Matches 90 and 80. Stops at 70 (75 > 70).
		// Remaining size is rejected/cancelled.
		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("sell-match", IOC, Sell, 75, 4, 204))

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			// 6 Initial + 2 Matches + 1 Reject = 9 Logs
			return memoryPublishTrader.Count() == 9
		}, 1*time.Second, 10*time.Millisecond)

		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(3), stats.AskDepthCount)
		assert.Equal(t, int64(1), stats.BidDepthCount) // Bid at 70 remains
	})
}

func TestMarketOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("take all orders using quote size", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &protocol.PlaceOrderCommand{
			OrderID:   "market-buy",
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      "0",
			QuoteSize: "360", // 110 + 120 + 130 = 360 USDT to spend
			UserID:    2,
		}
		err := testOrderBook.PlaceOrder(ctx, order)
		assert.NoError(t, err)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 9
		}, 1*time.Second, 10*time.Millisecond)

		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(0), stats.AskDepthCount)
		assert.Equal(t, int64(3), stats.BidDepthCount)
	})

	t.Run("take some orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &protocol.PlaceOrderCommand{
			OrderID:   "mysell",
			OrderType: Market,
			Side:      Sell,
			Price:     "0",
			Size:      "0",
			QuoteSize: "90", // 90 USDT worth
		}

		err := testOrderBook.PlaceOrder(ctx, order)
		assert.NoError(t, err)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(3), stats.AskDepthCount)
		assert.Equal(t, int64(2), stats.BidDepthCount)
	})

	t.Run("QuoteSize mode - buy with quote amount", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Buy using 230 quote currency (e.g., USDT)
		// Should match: sell-1 at 110 (1 BTC = 110 USDT) and sell-2 at 120 (1 BTC = 120 USDT)
		order := &protocol.PlaceOrderCommand{
			OrderID:   "market-quote-buy",
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      "0",
			QuoteSize: "230", // 110 + 120 = 230 USDT
		}
		err := testOrderBook.PlaceOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 8
		}, 1*time.Second, 10*time.Millisecond)

		// Verify first match: 1 BTC at 110
		match1 := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeMatch, match1.Type)
		assert.Equal(t, "110", match1.Price.String())
		assert.Equal(t, "1", match1.Size.String())
		assert.Equal(t, "110", match1.Amount.String())

		// Verify second match: 1 BTC at 120
		match2 := memoryPublishTrader.Get(7)
		assert.Equal(t, protocol.LogTypeMatch, match2.Type)
		assert.Equal(t, "120", match2.Price.String())
		assert.Equal(t, "1", match2.Size.String())
		assert.Equal(t, "120", match2.Amount.String())
	})

	t.Run("QuoteSize mode - partial fill of maker order", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Buy using 55 quote currency - should partially fill sell-1 at 110
		order := &protocol.PlaceOrderCommand{
			OrderID:   "market-quote-partial",
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      "0",
			QuoteSize: "55", // 55 USDT = 0.5 BTC at 110
		}
		err := testOrderBook.PlaceOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		match := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeMatch, match.Type)
		assert.Equal(t, "110", match.Price.String())
		assert.Equal(t, "0.5", match.Size.String()) // 55 / 110 = 0.5 BTC
		assert.Equal(t, "55", match.Amount.String())

		// Verify remaining sell order
		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(3), stats.AskDepthCount)
	})

	t.Run("QuoteSize mode - no liquidity rejection", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := NewOrderBook("BTC-USDT", publishTrader)
		go func() { _ = orderBook.Start() }()

		order := &protocol.PlaceOrderCommand{
			OrderID:   "market-quote-no-liq",
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      "0",
			QuoteSize: "100",
		}
		err := orderBook.PlaceOrder(ctx, order)
		assert.NoError(t, err)
		assert.Eventually(t, func() bool {
			return publishTrader.Count() >= 1
		}, 1*time.Second, 10*time.Millisecond)
		log := publishTrader.Get(0)
		assert.Equal(t, protocol.LogTypeReject, log.Type)
		assert.Equal(t, protocol.RejectReasonNoLiquidity, log.RejectReason)
		assert.Equal(t, "100", log.Size.String()) // Remaining quote amount
	})

	t.Run("Size mode - buy with base quantity", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Buy 2 base currency units (e.g., 2 BTC)
		order := &protocol.PlaceOrderCommand{
			OrderID:   "market-base-buy",
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      "2", // 2 BTC
		}
		err := testOrderBook.PlaceOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 8
		}, 1*time.Second, 10*time.Millisecond)

		// Verify first match: 1 BTC at 110
		match1 := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeMatch, match1.Type)
		assert.Equal(t, "110", match1.Price.String())
		assert.Equal(t, "1", match1.Size.String())
		assert.Equal(t, "110", match1.Amount.String())

		// Verify second match: 1 BTC at 120
		match2 := memoryPublishTrader.Get(7)
		assert.Equal(t, protocol.LogTypeMatch, match2.Type)
		assert.Equal(t, "120", match2.Price.String())
		assert.Equal(t, "1", match2.Size.String())
		assert.Equal(t, "120", match2.Amount.String())
	})

	t.Run("Size mode - partial fill of maker order", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Buy 0.5 base currency - should partially fill sell-1
		order := &protocol.PlaceOrderCommand{
			OrderID:   "market-base-partial",
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      udecimal.MustFromFloat64(0.5).String(), // 0.5 BTC
		}
		err := testOrderBook.PlaceOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		match := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeMatch, match.Type)
		assert.Equal(t, "110", match.Price.String())
		assert.Equal(t, "0.5", match.Size.String())
		assert.Equal(t, "55", match.Amount.String()) // 0.5 * 110 = 55

		// Verify remaining sell order
		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(3), stats.AskDepthCount)
	})

	t.Run("Size mode - take all orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Buy 3 base currency units - consume all sell orders
		order := &protocol.PlaceOrderCommand{
			OrderID:   "market-base-all",
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      "3", // 3 BTC
		}
		err := testOrderBook.PlaceOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 9
		}, 1*time.Second, 10*time.Millisecond)

		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(0), stats.AskDepthCount)
		assert.Equal(t, int64(3), stats.BidDepthCount)
	})
}

func TestPostOnlyOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("place a post only order", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		buyAll := &protocol.PlaceOrderCommand{
			OrderID:   "post_only",
			OrderType: PostOnly,
			Side:      Buy,
			Price:     "100",
			Size:      "1",
		}

		err := testOrderBook.PlaceOrder(ctx, buyAll)
		assert.NoError(t, err)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(3), stats.AskDepthCount)
		assert.Equal(t, int64(4), stats.BidDepthCount)
	})

	t.Run("fail to place a post only order", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("post_only", PostOnly, Buy, 115, 1, 0))
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, trade.Type)
		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(3), stats.AskDepthCount)
		assert.Equal(t, int64(3), stats.BidDepthCount)
	})
}

func TestIOCOrder(t *testing.T) {

	t.Run("no match any orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("ioc", IOC, Buy, 100, 1, 0))

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, trade.Type)
		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(3), stats.AskDepthCount)
		assert.Equal(t, int64(3), stats.BidDepthCount)
	})

	t.Run("take all orders with no error", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("ioc", IOC, Buy, 1000, 3, 0))

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 9
		}, 1*time.Second, 10*time.Millisecond)

		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(0), stats.AskDepthCount)
		assert.Equal(t, int64(3), stats.BidDepthCount)
	})

	t.Run("take all orders and finish as `cancel`", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("ioc", IOC, Sell, 10, 4, 0))

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 10
		}, 1*time.Second, 10*time.Millisecond)

		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(3), stats.AskDepthCount)
		assert.Equal(t, int64(0), stats.BidDepthCount)
	})

	t.Run("take some orders and finish as `cancel`", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("ioc", IOC, Buy, 115, 2, 0))

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 8
		}, 1*time.Second, 10*time.Millisecond)

		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(2), stats.AskDepthCount)
		assert.Equal(t, int64(3), stats.BidDepthCount)
	})
}

func TestFOKOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("no match any orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("fok", FOK, Buy, 100, 1, 0))
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, trade.Type)
		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(3), stats.AskDepthCount)
		assert.Equal(t, int64(3), stats.BidDepthCount)
	})

	t.Run("take all orders with no error", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("fok", FOK, Buy, 1000, 3, 0))

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 9
		}, 1*time.Second, 10*time.Millisecond)

		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(0), stats.AskDepthCount)
		assert.Equal(t, int64(3), stats.BidDepthCount)
	})

	t.Run("take all orders and finish as `cancel`", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("fok", FOK, Sell, 10, 4, 0))
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, trade.Type)
		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(3), stats.AskDepthCount)
		assert.Equal(t, int64(3), stats.BidDepthCount)
	})

	t.Run("take some orders and finish as `cancel`", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("fok", FOK, Buy, 115, 2, 0))
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, trade.Type)
		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(3), stats.AskDepthCount)
		assert.Equal(t, int64(3), stats.BidDepthCount)
	})

	// Test FOK with multiple orders at same price level
	t.Run("multiple orders at same price level", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := NewOrderBook("BTC-USDT", publishTrader)
		go func() {
			_ = orderBook.Start()
		}()

		// Setup: Place 2 orders at price 110
		// Order1: size=3, Order2: size=2, totalSize=5
		_ = orderBook.PlaceOrder(context.Background(), newPlaceCmd("sell-1", Limit, Sell, 110, 3, 201))

		_ = orderBook.PlaceOrder(context.Background(), newPlaceCmd("sell-2", Limit, Sell, 110, 2, 202))

		assert.Eventually(t, func() bool {
			stats, err := orderBook.GetStats()
			return err == nil && stats.AskOrderCount == 2
		}, 1*time.Second, 10*time.Millisecond)

		// FOK Buy: Price=115, Size=5
		// Expected: Should fully match since total size at price 110 is exactly 5
		fokOrder := newPlaceCmd("fok-buy", FOK, Buy, 115, 5, 301)

		err := orderBook.PlaceOrder(context.Background(), fokOrder)
		assert.NoError(t, err)

		// Expected: 2 (setup) + 2 (matches) = 4 logs
		memoryPublishTrader, _ := orderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 4
		}, 1*time.Second, 10*time.Millisecond)
		logCount := memoryPublishTrader.Count()

		if logCount >= 4 {
			log1 := memoryPublishTrader.Get(2)
			log2 := memoryPublishTrader.Get(3)
			assert.Equal(t, protocol.LogTypeMatch, log1.Type, "Third log should be Match")
			assert.Equal(t, protocol.LogTypeMatch, log2.Type, "Fourth log should be Match")
		}

		assert.Eventually(t, func() bool {
			stats, _ := orderBook.GetStats()
			return stats != nil && stats.AskDepthCount == 0
		}, 1*time.Second, 10*time.Millisecond)
	})

	// Test FOK crossing multiple price levels
	t.Run("cross multiple price levels", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := NewOrderBook("BTC-USDT", publishTrader)
		go func() {
			_ = orderBook.Start()
		}()

		// Setup:
		// Price 110: 1 order, size=2
		// Price 120: 1 order, size=3
		// Total available = 5
		_ = orderBook.PlaceOrder(context.Background(), newPlaceCmd("sell-1", Limit, Sell, 110, 2, 201))

		_ = orderBook.PlaceOrder(context.Background(), newPlaceCmd("sell-2", Limit, Sell, 120, 3, 202))

		assert.Eventually(t, func() bool {
			stats, err := orderBook.GetStats()
			return err == nil && stats.AskDepthCount == 2
		}, 1*time.Second, 10*time.Millisecond)

		// FOK Buy: Price=125, Size=5
		// Expected: Should fully match (consume 2 at 110 + 3 at 120 = 5)
		fokOrder := newPlaceCmd("fok-buy", FOK, Buy, 125, 5, 301)

		err := orderBook.PlaceOrder(context.Background(), fokOrder)
		assert.NoError(t, err)

		// Expected: 2 setup + 2 matches = 4
		memoryPublishTrader, _ := orderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 4
		}, 1*time.Second, 10*time.Millisecond)

		assert.Eventually(t, func() bool {
			stats, _ := orderBook.GetStats()
			return stats != nil && stats.AskDepthCount == 0
		}, 1*time.Second, 10*time.Millisecond)
	})

	// Test FOK with exact size match at price level
	t.Run("exact size match at price level", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := NewOrderBook("BTC-USDT", publishTrader)
		go func() {
			_ = orderBook.Start()
		}()

		// Setup: 3 orders at price 110, each size=1, totalSize=3
		for i := 1; i <= 3; i++ {
			_ = orderBook.PlaceOrder(context.Background(), newPlaceCmd("sell-"+udecimal.MustFromInt64(int64(i), 0).String(), Limit, Sell, 110, 1, int64(200+i)))
		}

		assert.Eventually(t, func() bool {
			stats, err := orderBook.GetStats()
			return err == nil && stats.AskDepthCount == 1 // 3 setup at same price should have depth 1
		}, 1*time.Second, 10*time.Millisecond)

		// FOK Buy: Price=115, Size=3
		// Expected: Should fully match
		fokOrder := newPlaceCmd("fok-buy", FOK, Buy, 115, 3, 301)

		err := orderBook.PlaceOrder(ctx, fokOrder)
		assert.NoError(t, err)

		// Expected: 3 setup + 3 matches = 6
		memoryPublishTrader, _ := orderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 6
		}, 1*time.Second, 10*time.Millisecond)

		stats, err := orderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(0), stats.AskDepthCount, "All sell orders should be matched")
	})
}

func TestCancelOrder(t *testing.T) {

	testOrderBook := createTestOrderBook(t)

	_ = testOrderBook.CancelOrder(context.Background(), newCancelCmd("sell-1", 201))
	assert.Eventually(t, func() bool {
		stats, err := testOrderBook.GetStats()
		return err == nil && stats.AskDepthCount == 2
	}, 1*time.Second, 10*time.Millisecond)

	_ = testOrderBook.CancelOrder(context.Background(), newCancelCmd("buy-1", 101))
	assert.Eventually(t, func() bool {
		stats, err := testOrderBook.GetStats()
		return err == nil && stats.BidDepthCount == 2
	}, 1*time.Second, 10*time.Millisecond)

	_ = testOrderBook.CancelOrder(context.Background(), newCancelCmd("aaaaaa", 999))
	assert.Eventually(t, func() bool {
		stats, err := testOrderBook.GetStats()
		return err == nil && stats.BidDepthCount == 2
	}, 1*time.Second, 10*time.Millisecond)
}

func TestAmendOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("decrease size preserves priority", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		// Initial: Buy 90(1), 80(1), 70(1). Sell 110(1), 120(1), 130(1)

		// 1. Amend buy-1 (Price 90) size from 1 to 0.5
		err := testOrderBook.AmendOrder(context.Background(), newAmendCmd("buy-1", 90, 0.5, 101))
		assert.NoError(t, err)
		// Verify Depth
		assert.Eventually(t, func() bool {
			depth, err := testOrderBook.Depth(10)
			return err == nil && len(depth.Bids) > 0 && depth.Bids[0].Size == "0.5"
		}, 1*time.Second, 10*time.Millisecond)
		depth, err := testOrderBook.Depth(10)
		assert.NoError(t, err)
		assert.Equal(t, "0.5", depth.Bids[0].Size)

		// Verify Log
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		// 6 setup + 1 amend
		assert.Equal(t, 7, memoryPublishTrader.Count())
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeAmend, log.Type)
		assert.Equal(t, "0.5", log.Size.String())
		assert.Equal(t, "1", log.OldSize.String())

		// Verify Priority: Add another order at same price, match against them.
		// Add buy-new at 90.
		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("buy-new", Limit, Buy, 90, 1, 401))

		// Sell matching order. Should match buy-1 (0.5) first, then buy-new.
		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("sell-match", Limit, Sell, 90, 0.5, 402))

		// Check logs for match
		// 6 setup + 1 amend + 1 open(buy-new) + 1 match(sell-match vs buy-1)
		// Check logs for match
		// 6 setup + 1 amend + 1 open(buy-new) + 1 match(sell-match vs buy-1)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() >= 9
		}, 1*time.Second, 10*time.Millisecond)
		matchLog := memoryPublishTrader.Get(8)
		assert.Equal(t, protocol.LogTypeMatch, matchLog.Type)
		assert.Equal(t, "buy-1", matchLog.MakerOrderID) // Priority kept!
		assert.Equal(t, int64(101), matchLog.MakerUserID)
		assert.Equal(t, "sell-match", matchLog.OrderID)
		assert.Equal(t, int64(402), matchLog.UserID)
	})

	t.Run("increase size loses priority", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		// Initial: Buy 90(1)

		// 1. Add another order at 90 to compete
		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("buy-2-compete", Limit, Buy, 90, 1, 0))

		// 2. Amend buy-1 size from 1 to 2 (Increase) -> Should lose priority to buy-2-compete
		err := testOrderBook.AmendOrder(context.Background(), newAmendCmd("buy-1", 90, 2, 101))
		assert.NoError(t, err)

		// 3. Sell matching order. Should match buy-2-compete first.
		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("sell-match", Limit, Sell, 90, 1, 0))

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		// Find match log
		assert.Eventually(t, func() bool {
			found := false
			for i := 0; i < memoryPublishTrader.Count(); i++ {
				log := memoryPublishTrader.Get(i)
				if log.Type == protocol.LogTypeMatch && log.OrderID == "sell-match" {
					assert.Equal(t, "buy-2-compete", log.MakerOrderID) // Priority lost!
					found = true
					break
				}
			}
			return found
		}, 1*time.Second, 10*time.Millisecond)
	})

	t.Run("change price moves level", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		// Buy-1 at 90.

		// Amend price to 95
		err := testOrderBook.AmendOrder(context.Background(), newAmendCmd("buy-1", 95, 1, 101))
		assert.NoError(t, err)
		assert.Eventually(t, func() bool {
			depth, err := testOrderBook.Depth(10)
			return err == nil && depth.Bids[0].Price == "95"
		}, 1*time.Second, 10*time.Millisecond)

		depth, err := testOrderBook.Depth(10)
		assert.NoError(t, err)
		assert.Equal(t, "95", depth.Bids[0].Price)
		assert.Equal(t, "1", depth.Bids[0].Size)

		// Old price 90 should be gone (or have other orders)
		// In createTestOrderBook, we had 90, 80, 70. Now 90 moved to 95.
		// Next bid should be 80.
		assert.Equal(t, "80", depth.Bids[1].Price)
	})

	t.Run("change price and size simultaneously", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		// Buy-1 at 90, Size 1.

		// Amend Price to 95 AND Size to 5
		err := testOrderBook.AmendOrder(ctx, &protocol.AmendOrderCommand{OrderID: "buy-1", UserID: 101, NewPrice: "95", NewSize: "5"})
		assert.NoError(t, err)
		assert.Eventually(t, func() bool {
			depth, err := testOrderBook.Depth(10)
			return err == nil && depth.Bids[0].Price == "95"
		}, 1*time.Second, 10*time.Millisecond)

		depth, err := testOrderBook.Depth(10)
		assert.NoError(t, err)
		assert.Equal(t, "95", depth.Bids[0].Price)
		assert.Equal(t, "5", depth.Bids[0].Size)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		log := memoryPublishTrader.Get(6) // 6 setup + 1 amend
		assert.Equal(t, protocol.LogTypeAmend, log.Type)
		assert.Equal(t, "95", log.Price.String())
		assert.Equal(t, "5", log.Size.String())
		assert.Equal(t, "90", log.OldPrice.String())
		assert.Equal(t, "1", log.OldSize.String())
	})

	t.Run("amend order crosses spread and matches", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		// Initial: Sell 110(1), 120(1)... Buy 90(1)...

		// Amend Buy-1 (90) to 115 (Crosses Sell-1 at 110) with Size 2
		// Should match Sell-1 (Price 110, Size 1) fully.
		// Remaining Buy-1 (Size 1) should sit at 115.
		err := testOrderBook.AmendOrder(context.Background(), newAmendCmd("buy-1", 115, 2, 101))
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		// 6 setup + 1 amend + 1 match + 1 open (remaining)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 9
		}, 1*time.Second, 10*time.Millisecond)

		// Verify Amend Log
		amendLog := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeAmend, amendLog.Type)
		assert.Equal(t, "115", amendLog.Price.String())
		assert.Equal(t, "2", amendLog.Size.String())

		// Verify Match Log
		matchLog := memoryPublishTrader.Get(7)
		assert.Equal(t, protocol.LogTypeMatch, matchLog.Type)
		assert.Equal(t, "buy-1", matchLog.OrderID)
		assert.Equal(t, "sell-1", matchLog.MakerOrderID)
		assert.Equal(t, "110", matchLog.Price.String()) // Matched at Maker price
		assert.Equal(t, "1", matchLog.Size.String())

		// Verify Open Log (Remaining part)
		openLog := memoryPublishTrader.Get(8)
		assert.Equal(t, protocol.LogTypeOpen, openLog.Type)
		assert.Equal(t, "buy-1", openLog.OrderID)
		assert.Equal(t, "115", openLog.Price.String())
		assert.Equal(t, "1", openLog.Size.String())

		// Verify Depth: Buy-1 remaining 1 at 115
		depth, err := testOrderBook.Depth(10)
		assert.NoError(t, err)
		assert.Equal(t, "115", depth.Bids[0].Price)
		assert.Equal(t, "1", depth.Bids[0].Size)
	})
}

func TestDepth(t *testing.T) {
	testOrderBook := createTestOrderBook(t)

	result, err := testOrderBook.Depth(5)
	assert.NoError(t, err)

	assert.Len(t, result.Asks, 3)
	assert.Len(t, result.Bids, 3)
}

func TestShutdown(t *testing.T) {
	ctx := context.Background()
	t.Run("Shutdown blocks until drain completes", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := NewOrderBook("BTC-USDT", publishTrader)

		// Start the order book in a goroutine
		startDone := make(chan error)
		go func() {
			startDone <- orderBook.Start()
		}()

		// Add some orders
		for i := 0; i < 10; i++ {
			err := orderBook.PlaceOrder(context.Background(), newPlaceCmd("order-"+string(rune('a'+i)), Limit, Buy, float64(100-i), 1, int64(i)))
			assert.NoError(t, err)
		}

		// Shutdown should block and complete successfully
		shutdownCtx := context.Background()
		err := orderBook.Shutdown(shutdownCtx)
		assert.NoError(t, err)

		// Start() should have returned
		select {
		case err := <-startDone:
			assert.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("Start() did not return after Shutdown()")
		}
	})

	t.Run("AddOrder returns ErrShutdown after shutdown", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := NewOrderBook("BTC-USDT", publishTrader)

		go func() {
			_ = orderBook.Start()
		}()

		// Wait for Start() to be ready using GetStats
		assert.Eventually(t, func() bool {
			_, err := orderBook.GetStats()
			return err == nil
		}, 1*time.Second, 10*time.Millisecond)

		// Shutdown
		err := orderBook.Shutdown(ctx)
		assert.NoError(t, err)

		// Try to add an order after shutdown
		order := &protocol.PlaceOrderCommand{
			OrderID:   "after-shutdown",
			OrderType: Limit,
			Side:      Buy,
			Size:      "1",
			Price:     "100",
			UserID:    999,
		}
		// Redeclare ctx for this test case
		ctx := context.Background()
		err = orderBook.PlaceOrder(ctx, order)
		assert.Equal(t, ErrShutdown, err)
	})

	t.Run("Shutdown respects context timeout", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := NewOrderBook("BTC-USDT", publishTrader)

		// Do NOT start the order book - this simulates Start() not being called
		// The drain will never happen, so Shutdown should timeout

		// Publish a command that won't be consumed because Start() wasn't called.
		// This ensures ProducerSequence > ConsumerSequence, causing Shutdown to wait.
		orderBook.cmdBuffer.Publish(InputEvent{})

		// Set isShutdown manually to simulate partial state
		// and close done channel
		orderBook.isShutdown.Store(true)
		close(orderBook.done)

		// Shutdown with a short timeout
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		err := orderBook.Shutdown(ctx)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("Multiple Shutdown calls are safe", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := NewOrderBook("BTC-USDT", publishTrader)

		go func() {
			_ = orderBook.Start()
		}()

		assert.Eventually(t, func() bool {
			_, err := orderBook.GetStats()
			return err == nil
		}, 1*time.Second, 10*time.Millisecond)

		// First shutdown
		err := orderBook.Shutdown(ctx)
		assert.NoError(t, err)

		// Second shutdown should also work (idempotent)
		err = orderBook.Shutdown(ctx)
		assert.NoError(t, err)
	})
}

func TestRejectReason(t *testing.T) {
	ctx := context.Background()
	t.Run("IOC no liquidity", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := NewOrderBook("BTC-USDT", publishTrader)
		go func() { _ = orderBook.Start() }()

		// Empty order book - IOC should be rejected with NoLiquidity
		order := &protocol.PlaceOrderCommand{
			OrderID:   "ioc-1",
			OrderType: IOC,
			Side:      Buy,
			Price:     "100",
			Size:      "1",
		}
		err := orderBook.PlaceOrder(ctx, order)
		assert.NoError(t, err)
		assert.Eventually(t, func() bool {
			return publishTrader.Count() >= 1
		}, 1*time.Second, 10*time.Millisecond)

		log := publishTrader.Get(0)
		assert.Equal(t, protocol.LogTypeReject, log.Type)
		assert.Equal(t, protocol.RejectReasonNoLiquidity, log.RejectReason)
	})

	t.Run("IOC price mismatch", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// IOC Buy at 100, but best ask is 110 - should reject with PriceMismatch
		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("ioc-price", IOC, Buy, 100, 1, 0))
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, log.Type)
		assert.Equal(t, protocol.RejectReasonPriceMismatch, log.RejectReason)
	})

	t.Run("FOK insufficient size", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// FOK Buy for 10 units, but only 3 available - should reject with InsufficientSize
		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("fok-size", FOK, Buy, 1000, 10, 0))
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, log.Type)
		assert.Equal(t, protocol.RejectReasonInsufficientSize, log.RejectReason)
	})

	t.Run("FOK price mismatch", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// FOK Buy at 100, but best ask is 110 - should reject with PriceMismatch
		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("fok-price", FOK, Buy, 100, 1, 0))
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, log.Type)
		assert.Equal(t, protocol.RejectReasonPriceMismatch, log.RejectReason)
	})

	t.Run("PostOnly would cross spread", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// PostOnly Buy at 115, but best ask is 110 - would cross, should reject
		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("post-only-cross", PostOnly, Buy, 115, 1, 0))
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, log.Type)
		assert.Equal(t, protocol.RejectReasonPostOnlyMatch, log.RejectReason)
	})

	t.Run("Market no liquidity", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := NewOrderBook("BTC-USDT", publishTrader)
		go func() { _ = orderBook.Start() }()

		// Empty order book - Market should be rejected with NoLiquidity
		_ = orderBook.PlaceOrder(context.Background(), newPlaceCmd("market-1", Market, Buy, 0, 100, 0))
		assert.Eventually(t, func() bool {
			return publishTrader.Count() >= 1
		}, 1*time.Second, 10*time.Millisecond)
		log := publishTrader.Get(0)
		assert.Equal(t, protocol.LogTypeReject, log.Type)
		assert.Equal(t, protocol.RejectReasonNoLiquidity, log.RejectReason)
	})
}

func TestMatchAmount(t *testing.T) {
	ctx := context.Background()

	t.Run("exact size match at price level", func(t *testing.T) {
		// Use a fresh order book to avoid ID conflicts
		publishTrader := NewMemoryPublishLog()
		testOrderBook := NewOrderBook("BTC-USDT", publishTrader)
		go func() { _ = testOrderBook.Start() }()

		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("sell-1", Limit, Sell, 80, 3, 204))

		// Ensure first order is processed
		assert.Eventually(t, func() bool {
			stats, _ := testOrderBook.GetStats()
			return stats != nil && stats.AskOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("buy-fok", FOK, Buy, 80, 3, 104))

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			// 1 Open (sell-1) + 1 Match (buy-fok) = 2 logs
			return memoryPublishTrader.Count() == 2
		}, 1*time.Second, 10*time.Millisecond)

		matchLog := memoryPublishTrader.Get(1)
		assert.Equal(t, protocol.LogTypeMatch, matchLog.Type)
		assert.Equal(t, "80", matchLog.Price.String())
		assert.Equal(t, "3", matchLog.Size.String())
		assert.Equal(t, "240", matchLog.Amount.String()) // Price * Size = 80 * 3 = 240
	})

	t.Run("multiple orders at same price level", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		testOrderBook := NewOrderBook("BTC-USDT", publishTrader)
		go func() { _ = testOrderBook.Start() }()

		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("sell-1", Limit, Sell, 80, 1, 204))
		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("sell-2", Limit, Sell, 80, 1, 205))

		assert.Eventually(t, func() bool {
			stats, _ := testOrderBook.GetStats()
			return stats != nil && stats.AskOrderCount == 2
		}, 1*time.Second, 10*time.Millisecond)

		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("buy-fok", FOK, Buy, 80, 2, 104))
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)

		assert.Eventually(t, func() bool {
			// 2 Opens (sell-1, sell-2) + 2 Matches (buy-fok split) = 4 logs
			return memoryPublishTrader.Count() == 4
		}, 1*time.Second, 10*time.Millisecond)

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
		testOrderBook := NewOrderBook("BTC-USDT", publishTrader)
		go func() { _ = testOrderBook.Start() }()

		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("sell-1", Limit, Sell, 110, 1, 201))
		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("sell-2", Limit, Sell, 120, 1, 202))
		_ = testOrderBook.PlaceOrder(context.Background(), newPlaceCmd("sell-3", Limit, Sell, 130, 1, 203))

		assert.Eventually(t, func() bool {
			stats, _ := testOrderBook.GetStats()
			return stats != nil && stats.AskOrderCount == 3
		}, 1*time.Second, 10*time.Millisecond)

		// Buy order that matches multiple sell orders
		order := &protocol.PlaceOrderCommand{
			OrderID:   "buy-all",
			OrderType: Limit,
			Side:      Buy,
			Price:     "1000",
			Size:      "3",
			UserID:    300,
		}
		err := testOrderBook.PlaceOrder(ctx, order)
		assert.NoError(t, err)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			// 3 Opens + 3 Matches = 6 logs
			return memoryPublishTrader.Count() == 6
		}, 1*time.Second, 10*time.Millisecond)

		// Match 1: sell-1 at 110, size 1 -> Amount 110
		match1 := memoryPublishTrader.Get(3)
		assert.Equal(t, protocol.LogTypeMatch, match1.Type)
		assert.Equal(t, "110", match1.Amount.String())

		// Match 2: sell-2 at 120, size 1 -> Amount 120
		match2 := memoryPublishTrader.Get(4)
		assert.Equal(t, protocol.LogTypeMatch, match2.Type)
		assert.Equal(t, "120", match2.Amount.String())

		// Match 3: sell-3 at 130, size 1 -> Amount 130
		match3 := memoryPublishTrader.Get(5)
		assert.Equal(t, protocol.LogTypeMatch, match3.Type)
		assert.Equal(t, "130", match3.Amount.String())
	})
}

func TestTradeID(t *testing.T) {
	ctx := context.Background()

	t.Run("trade ID only on match events", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)

		// All Open events should have TradeID = 0
		for i := 0; i < 6; i++ {
			log := memoryPublishTrader.Get(i)
			assert.Equal(t, protocol.LogTypeOpen, log.Type)
			assert.Equal(t, uint64(0), log.TradeID, "Open event should have TradeID 0")
		}

		// Add a matching order
		order := &protocol.PlaceOrderCommand{
			OrderID:   "buy-match",
			OrderType: Limit,
			Side:      Buy,
			Price:     "115",
			Size:      "1",
		}
		err := testOrderBook.PlaceOrder(ctx, order)
		assert.NoError(t, err)
		// Match event should have TradeID > 0
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)
		matchLog := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeMatch, matchLog.Type)
		assert.Greater(t, matchLog.TradeID, uint64(0), "Match event should have TradeID > 0")
	})

	t.Run("trade ID sequential", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Buy order that matches all 3 sell orders
		order := &protocol.PlaceOrderCommand{
			OrderID:   "buy-all",
			OrderType: Limit,
			Side:      Buy,
			Price:     "1000",
			Size:      "3",
		}
		err := testOrderBook.PlaceOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 9
		}, 1*time.Second, 10*time.Millisecond)

		// 3 matches should have sequential TradeIDs
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

		// FOK that cannot be filled
		order := &protocol.PlaceOrderCommand{
			OrderID:   "fok-reject",
			OrderType: FOK,
			Side:      Buy,
			Price:     "100",
			Size:      "1",
		}
		err := testOrderBook.PlaceOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)
		rejectLog := memoryPublishTrader.Get(6)
		assert.Equal(t, protocol.LogTypeReject, rejectLog.Type)
		assert.Equal(t, uint64(0), rejectLog.TradeID, "Reject event should have TradeID 0")
	})
}

func TestOrderBookSnapshotRestore(t *testing.T) {
	ctx := context.Background()

	t.Run("Snapshot and Restore maintain state", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		book := NewOrderBook("BTC-USDT", publishTrader)
		go func() { _ = book.Start() }()

		// 1. Populate OrderBook with state
		// Add some Limit orders (Bid and Ask)
		err := book.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
			OrderID:   "bid-1",
			OrderType: Limit,
			Side:      Buy,
			Price:     "100",
			Size:      "10",
			UserID:    1,
		})
		assert.NoError(t, err)

		err = book.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
			OrderID:   "ask-1",
			OrderType: Limit,
			Side:      Sell,
			Price:     "110",
			Size:      "5",
			UserID:    2,
		})
		assert.NoError(t, err)

		// Wait for processing
		assert.Eventually(t, func() bool {
			stats, _ := book.GetStats()
			return stats.BidOrderCount == 1 && stats.AskOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		// 2. Take Snapshot
		snap, err := book.TakeSnapshot()
		assert.NoError(t, err)
		assert.NotNil(t, snap)
		assert.Equal(t, "BTC-USDT", snap.MarketID)
		assert.Len(t, snap.Bids, 1)
		assert.Len(t, snap.Asks, 1)
		assert.Equal(t, "bid-1", snap.Bids[0].ID)
		assert.Equal(t, "ask-1", snap.Asks[0].ID)

		// Verify internal counters in snapshot
		// SeqID should be > 0 (Open events)
		assert.Greater(t, snap.SeqID, uint64(0))

		// 3. Create a NEW OrderBook and Restore
		restoredBook := NewOrderBook("BTC-USDT", NewMemoryPublishLog())
		// Don't start it yet, Restore is done on stop state usually, or before processing
		restoredBook.Restore(snap)

		// Verify queues are rebuilt internally
		// Since we haven't started the loop, we can't use GetStats() easily if it relies on loop responding.
		// Detailed inspection:
		assert.Equal(t, int64(1), restoredBook.bidQueue.orderCount())
		assert.Equal(t, int64(1), restoredBook.askQueue.orderCount())

		// Verify Order details
		bid := restoredBook.bidQueue.order("bid-1")
		assert.NotNil(t, bid)
		assert.Equal(t, "100", bid.Price.String())

		ask := restoredBook.askQueue.order("ask-1")
		assert.NotNil(t, ask)
		assert.Equal(t, "110", ask.Price.String())

		// 4. Start Restored Book and verify it functions
		go func() { _ = restoredBook.Start() }()

		assert.Eventually(t, func() bool {
			stats, _ := restoredBook.GetStats()
			return stats.BidOrderCount == 1 && stats.AskOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		// Add a matching order to prove continuity
		err = restoredBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
			OrderID:   "buy-match",
			OrderType: Limit,
			Side:      Buy,
			Price:     "115",
			Size:      "1",
		})
		assert.NoError(t, err)

		assert.Eventually(t, func() bool {
			stats, _ := restoredBook.GetStats()
			// Bid should consume 1 from Ask-1
			// Ask-1 size 5 -> becomes 4
			// Bid-1 size 10 -> remains 10
			return stats.AskOrderCount == 1 && stats.BidOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		// Verify ask size reduced
		askAfter := restoredBook.askQueue.order("ask-1")
		assert.Equal(t, "4", askAfter.Size.String())
	})
}

func TestOrderValidation(t *testing.T) {
	ctx := context.Background()
	publishTrader := NewMemoryPublishLog()
	orderBook := NewOrderBook("BTC-USDT", publishTrader)
	go func() {
		_ = orderBook.Start()
	}()

	t.Run("RejectDuplicateOrderID", func(t *testing.T) {
		_ = orderBook.PlaceOrder(context.Background(), newPlaceCmd("dup-id", Limit, Buy, 100, 1, 1))

		// Try to add same ID again
		err := orderBook.PlaceOrder(ctx, newPlaceCmd("dup-id", Limit, Buy, 100, 1, 1))
		assert.NoError(t, err)

		assert.Eventually(t, func() bool {
			logs := publishTrader.Logs()
			for _, log := range logs {
				if log.Type == protocol.LogTypeReject && log.OrderID == "dup-id" && log.RejectReason == protocol.RejectReasonDuplicateID {
					return true
				}
			}
			return false
		}, 1*time.Second, 10*time.Millisecond)
	})

	t.Run("RejectCancelNonExistentOrder", func(t *testing.T) {
		err := orderBook.CancelOrder(ctx, &protocol.CancelOrderCommand{OrderID: "non-existent", UserID: 1})
		assert.NoError(t, err)

		assert.Eventually(t, func() bool {
			logs := publishTrader.Logs()
			for _, log := range logs {
				if log.Type == protocol.LogTypeReject && log.OrderID == "non-existent" && log.RejectReason == protocol.RejectReasonOrderNotFound {
					return true
				}
			}
			return false
		}, 1*time.Second, 10*time.Millisecond)
	})

	t.Run("RejectAmendNonExistentOrder", func(t *testing.T) {
		err := orderBook.AmendOrder(ctx, &protocol.AmendOrderCommand{OrderID: "non-existent", UserID: 1, NewPrice: "100", NewSize: "2"})
		assert.NoError(t, err)

		assert.Eventually(t, func() bool {
			logs := publishTrader.Logs()
			for _, log := range logs {
				if log.Type == protocol.LogTypeReject && log.OrderID == "non-existent" && log.RejectReason == protocol.RejectReasonOrderNotFound {
					return true
				}
			}
			return false
		}, 1*time.Second, 10*time.Millisecond)
	})

	t.Run("RejectUserIDMismatch", func(t *testing.T) {
		// Create an order with UserID 1
		id := "owner-1"
		orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
			OrderID:   id,
			OrderType: Limit,
			Side:      Buy,
			Size:      "1",
			Price:     "100",
			UserID:    1,
		})

		// Wait for it to be added
		assert.Eventually(t, func() bool {
			stats, _ := orderBook.GetStats()
			return stats.BidOrderCount > 0
		}, 1*time.Second, 10*time.Millisecond)

		// Try to cancel with UserID 2
		err := orderBook.CancelOrder(ctx, &protocol.CancelOrderCommand{OrderID: id, UserID: 2})
		assert.NoError(t, err)

		// Try to amend with UserID 2
		err = orderBook.AmendOrder(ctx, &protocol.AmendOrderCommand{OrderID: id, UserID: 2, NewPrice: "110", NewSize: "1"})
		assert.NoError(t, err)

		// Verify rejections
		assert.Eventually(t, func() bool {
			logs := publishTrader.Logs()
			cancelRejected := false
			amendRejected := false
			for _, log := range logs {
				if log.Type == protocol.LogTypeReject && log.OrderID == id && log.UserID == 2 && log.RejectReason == protocol.RejectReasonOrderNotFound {
					// We use OrderNotFound for security when UserID mismatches
					if !cancelRejected {
						cancelRejected = true
						continue
					}
					amendRejected = true
				}
			}
			return cancelRejected && amendRejected
		}, 1*time.Second, 10*time.Millisecond)
	})
}

func TestOrderBook_LotSize(t *testing.T) {
	ctx := context.Background()

	t.Run("default LotSize is 1e-8", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := NewOrderBook("BTC-USDT", publishTrader)
		go func() { _ = orderBook.Start() }()

		// Verify default lotSize is set
		assert.True(t, orderBook.lotSize == DefaultLotSize)
	})

	t.Run("custom LotSize via option", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		customLotSize := udecimal.MustParse("0.0001")
		orderBook := NewOrderBook("BTC-USDT", publishTrader, WithLotSize(customLotSize))
		go func() { _ = orderBook.Start() }()

		assert.True(t, orderBook.lotSize == customLotSize)
	})

	t.Run("Market order quote mode - reject when matchSize below LotSize", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		// Set LotSize to 0.001 BTC
		lotSize := udecimal.MustParse("0.001")
		orderBook := NewOrderBook("BTC-USDT", publishTrader, WithLotSize(lotSize))
		go func() { _ = orderBook.Start() }()

		// Place a sell order: 0.1 BTC @ 50000 USDT
		err := orderBook.PlaceOrder(context.Background(), &protocol.PlaceOrderCommand{
			OrderID:   "sell-1",
			OrderType: Limit,
			Side:      Sell,
			Price:     "50000",
			Size:      "0.1",
			UserID:    1,
		})
		assert.NoError(t, err)

		assert.Eventually(t, func() bool {
			stats, _ := orderBook.GetStats()
			return stats != nil && stats.AskOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		// Market buy with quoteSize = 0.04 USDT
		// matchSize = 0.04 / 50000 = 0.0000008 BTC < 0.001 (LotSize)
		// Should be rejected immediately
		err = orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
			OrderID:   "market-buy",
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      "0",
			QuoteSize: udecimal.MustParse("0.04").String(),
		})
		assert.NoError(t, err)

		// Wait for reject log
		assert.Eventually(t, func() bool {
			logs := publishTrader.Logs()
			for _, log := range logs {
				if log.Type == protocol.LogTypeReject && log.OrderID == "market-buy" {
					// Verify remaining quote size is returned
					assert.Equal(t, "0.04", log.Size.String())
					return true
				}
			}
			return false
		}, 1*time.Second, 10*time.Millisecond)

		// Verify sell order is still intact
		stats, _ := orderBook.GetStats()
		assert.Equal(t, int64(1), stats.AskOrderCount)
	})

	t.Run("Market order quote mode - partial fill then reject remaining", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		// Set LotSize to 0.001 BTC
		lotSize := udecimal.MustParse("0.001")
		orderBook := NewOrderBook("BTC-USDT", publishTrader, WithLotSize(lotSize))
		go func() { _ = orderBook.Start() }()

		// Place a sell order: 0.005 BTC @ 1000 USDT (worth 5 USDT)
		err := orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
			OrderID:   "sell-1",
			OrderType: Limit,
			Side:      Sell,
			Size:      udecimal.MustParse("0.005").String(),
			Price:     "1000",
		})
		assert.NoError(t, err)

		assert.Eventually(t, func() bool {
			stats, _ := orderBook.GetStats()
			return stats != nil && stats.AskOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		// Market buy with quoteSize = 5.5 USDT
		// First match: 0.005 BTC @ 1000 = 5 USDT
		// Remaining: 0.5 USDT → matchSize = 0.5/1000 = 0.0005 BTC < 0.001 (LotSize)
		// Should match first, then reject remaining
		err = orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
			OrderID:   "market-buy",
			OrderType: Market,
			Side:      Buy,
			Price:     "0",
			Size:      "0",
			QuoteSize: udecimal.MustParse("5.5").String(),
		})
		assert.NoError(t, err)

		// Wait for match and reject log
		assert.Eventually(t, func() bool {
			logs := publishTrader.Logs()
			matchFound := false
			rejectFound := false
			for _, log := range logs {
				if log.Type == protocol.LogTypeMatch && log.OrderID == "market-buy" {
					matchFound = true
				}
				if log.Type == protocol.LogTypeReject && log.OrderID == "market-buy" {
					// Remaining ~0.5 USDT should be in reject log
					rejectFound = true
				}
			}
			return matchFound && rejectFound
		}, 1*time.Second, 10*time.Millisecond)
	})
}
