package match

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func createTestOrderBook(t *testing.T) *OrderBook {
	ctx := context.Background()

	publishTrader := NewMemoryPublishLog()
	orderBook := NewOrderBook("BTC-USDT", publishTrader)
	go func() {
		_ = orderBook.Start()
	}()

	orderBuy1 := &PlaceOrderCommand{
		ID:     "buy-1",
		Type:   Limit,
		Side:   Buy,
		Size:   decimal.NewFromInt(1),
		Price:  decimal.NewFromInt(90),
		UserID: 101,
	}

	err := orderBook.AddOrder(ctx, orderBuy1)
	assert.NoError(t, err)

	orderBuy2 := &PlaceOrderCommand{
		ID:     "buy-2",
		Type:   Limit,
		Side:   Buy,
		Size:   decimal.NewFromInt(1),
		Price:  decimal.NewFromInt(80),
		UserID: 102,
	}

	err = orderBook.AddOrder(ctx, orderBuy2)
	assert.NoError(t, err)

	orderBuy3 := &PlaceOrderCommand{
		ID:     "buy-3",
		Type:   Limit,
		Side:   Buy,
		Size:   decimal.NewFromInt(1),
		Price:  decimal.NewFromInt(70),
		UserID: 103,
	}

	err = orderBook.AddOrder(ctx, orderBuy3)
	assert.NoError(t, err)

	orderSell1 := &PlaceOrderCommand{
		ID:     "sell-1",
		Type:   Limit,
		Side:   Sell,
		Size:   decimal.NewFromInt(1),
		Price:  decimal.NewFromInt(110),
		UserID: 201,
	}
	err = orderBook.AddOrder(ctx, orderSell1)
	assert.NoError(t, err)

	orderSell2 := &PlaceOrderCommand{
		ID:     "sell-2",
		Type:   Limit,
		Side:   Sell,
		Size:   decimal.NewFromInt(1),
		Price:  decimal.NewFromInt(120),
		UserID: 202,
	}
	err = orderBook.AddOrder(ctx, orderSell2)
	assert.NoError(t, err)

	orderSell3 := &PlaceOrderCommand{
		ID:     "sell-3",
		Type:   Limit,
		Side:   Sell,
		Size:   decimal.NewFromInt(1),
		Price:  decimal.NewFromInt(130),
		UserID: 203,
	}
	err = orderBook.AddOrder(ctx, orderSell3)
	assert.NoError(t, err)

	// Wait for all 6 orders to be processed
	assert.Eventually(t, func() bool {
		stats, err := orderBook.GetStats()
		return err == nil && stats.AskOrderCount == 3 && stats.BidOrderCount == 3
	}, 1*time.Second, 10*time.Millisecond)

	return orderBook
}

func TestLimitOrders(t *testing.T) {
	ctx := context.Background()

	t.Run("take all orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Verify initial LastCmdSeqID is 0
		assert.Equal(t, uint64(0), testOrderBook.LastCmdSeqID())

		// Send command with SeqID
		testOrderBook.cmdChan <- Command{
			SeqID: 100,
			Type:  CmdPlaceOrder,
			Payload: &PlaceOrderCommand{
				ID:     "buyAll",
				Type:   Limit,
				Side:   Buy,
				Price:  decimal.NewFromInt(1000),
				Size:   decimal.NewFromInt(10),
				UserID: 300,
			},
		}

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
		assert.Equal(t, LogTypeMatch, match1.Type)
		assert.Equal(t, "sell-1", match1.MakerOrderID)
		assert.Equal(t, int64(201), match1.MakerUserID)
		assert.Equal(t, "buyAll", match1.OrderID)
		assert.Equal(t, int64(300), match1.UserID)

		match2 := memoryPublishTrader.Get(7)
		assert.Equal(t, LogTypeMatch, match2.Type)
		assert.Equal(t, "sell-2", match2.MakerOrderID)
		assert.Equal(t, int64(202), match2.MakerUserID)

		match3 := memoryPublishTrader.Get(8)
		assert.Equal(t, LogTypeMatch, match3.Type)
		assert.Equal(t, "sell-3", match3.MakerOrderID)
		assert.Equal(t, int64(203), match3.MakerUserID)
	})

	t.Run("take some orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &PlaceOrderCommand{
			ID:     "mysell",
			Type:   Limit,
			Side:   Sell,
			Size:   decimal.NewFromInt(5),
			Price:  decimal.NewFromInt(75),
			UserID: 301,
		}
		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)

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
		assert.Equal(t, LogTypeMatch, match1.Type)
		assert.Equal(t, "buy-1", match1.MakerOrderID)
		assert.Equal(t, int64(101), match1.MakerUserID)
		assert.Equal(t, "mysell", match1.OrderID)
		assert.Equal(t, int64(301), match1.UserID)

		match2 := memoryPublishTrader.Get(7)
		assert.Equal(t, LogTypeMatch, match2.Type)
		assert.Equal(t, "buy-2", match2.MakerOrderID)
		assert.Equal(t, int64(102), match2.MakerUserID)
	})
}

func TestMarketOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("take all orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &PlaceOrderCommand{
			ID:        "buyAll",
			Type:      Market,
			Side:      Buy,
			Price:     decimal.NewFromInt(0),
			QuoteSize: decimal.NewFromInt(110).Add(decimal.NewFromInt(120)).Add(decimal.NewFromInt(130)), // 360 USDT to spend
		}

		err := testOrderBook.AddOrder(ctx, order)
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

		order := &PlaceOrderCommand{
			ID:        "mysell",
			Type:      Market,
			Side:      Sell,
			Price:     decimal.NewFromInt(0),
			QuoteSize: decimal.NewFromInt(90), // 90 USDT worth
		}

		err := testOrderBook.AddOrder(ctx, order)
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
		order := &PlaceOrderCommand{
			ID:        "market-quote-buy",
			Type:      Market,
			Side:      Buy,
			QuoteSize: decimal.NewFromInt(230), // 110 + 120 = 230 USDT
		}
		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 8
		}, 1*time.Second, 10*time.Millisecond)

		// Verify first match: 1 BTC at 110
		match1 := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeMatch, match1.Type)
		assert.Equal(t, "110", match1.Price.String())
		assert.Equal(t, "1", match1.Size.String())
		assert.Equal(t, "110", match1.Amount.String())

		// Verify second match: 1 BTC at 120
		match2 := memoryPublishTrader.Get(7)
		assert.Equal(t, LogTypeMatch, match2.Type)
		assert.Equal(t, "120", match2.Price.String())
		assert.Equal(t, "1", match2.Size.String())
		assert.Equal(t, "120", match2.Amount.String())
	})

	t.Run("QuoteSize mode - partial fill of maker order", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Buy using 55 quote currency - should partially fill sell-1 at 110
		order := &PlaceOrderCommand{
			ID:        "market-quote-partial",
			Type:      Market,
			Side:      Buy,
			QuoteSize: decimal.NewFromInt(55), // 55 USDT = 0.5 BTC at 110
		}
		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		match := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeMatch, match.Type)
		assert.Equal(t, "110", match.Price.String())
		assert.Equal(t, "0.5", match.Size.String()) // 55 / 110 = 0.5 BTC
		assert.Equal(t, "55", match.Amount.String())

		// Verify remaining sell order
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
	})

	t.Run("QuoteSize mode - no liquidity rejection", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := NewOrderBook("BTC-USDT", publishTrader)
		go func() { _ = orderBook.Start() }()

		order := &PlaceOrderCommand{
			ID:        "market-quote-no-liq",
			Type:      Market,
			Side:      Buy,
			QuoteSize: decimal.NewFromInt(100),
		}
		err := orderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		assert.Eventually(t, func() bool {
			return publishTrader.Count() >= 1
		}, 1*time.Second, 10*time.Millisecond)
		log := publishTrader.Get(0)
		assert.Equal(t, LogTypeReject, log.Type)
		assert.Equal(t, RejectReasonNoLiquidity, log.RejectReason)
		assert.Equal(t, "100", log.Size.String()) // Remaining quote amount
	})

	t.Run("Size mode - buy with base quantity", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Buy 2 base currency units (e.g., 2 BTC)
		order := &PlaceOrderCommand{
			ID:   "market-base-buy",
			Type: Market,
			Side: Buy,
			Size: decimal.NewFromInt(2), // 2 BTC
		}
		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 8
		}, 1*time.Second, 10*time.Millisecond)

		// Verify first match: 1 BTC at 110
		match1 := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeMatch, match1.Type)
		assert.Equal(t, "110", match1.Price.String())
		assert.Equal(t, "1", match1.Size.String())
		assert.Equal(t, "110", match1.Amount.String())

		// Verify second match: 1 BTC at 120
		match2 := memoryPublishTrader.Get(7)
		assert.Equal(t, LogTypeMatch, match2.Type)
		assert.Equal(t, "120", match2.Price.String())
		assert.Equal(t, "1", match2.Size.String())
		assert.Equal(t, "120", match2.Amount.String())
	})

	t.Run("Size mode - partial fill of maker order", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Buy 0.5 base currency - should partially fill sell-1
		order := &PlaceOrderCommand{
			ID:   "market-base-partial",
			Type: Market,
			Side: Buy,
			Size: decimal.NewFromFloat(0.5), // 0.5 BTC
		}
		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		match := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeMatch, match.Type)
		assert.Equal(t, "110", match.Price.String())
		assert.Equal(t, "0.5", match.Size.String())
		assert.Equal(t, "55", match.Amount.String()) // 0.5 * 110 = 55

		// Verify remaining sell order
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
	})

	t.Run("Size mode - take all orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Buy 3 base currency units - consume all sell orders
		order := &PlaceOrderCommand{
			ID:   "market-base-all",
			Type: Market,
			Side: Buy,
			Size: decimal.NewFromInt(3), // 3 BTC
		}
		err := testOrderBook.AddOrder(ctx, order)
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

		buyAll := &PlaceOrderCommand{
			ID:    "post_only",
			Type:  PostOnly,
			Side:  Buy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}

		err := testOrderBook.AddOrder(ctx, buyAll)
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

		buyAll := &PlaceOrderCommand{
			ID:    "post_only",
			Type:  PostOnly,
			Side:  Buy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(1),
		}

		err := testOrderBook.AddOrder(ctx, buyAll)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeReject, trade.Type)
		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(3), stats.AskDepthCount)
		assert.Equal(t, int64(3), stats.BidDepthCount)
	})
}

func TestIOCOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("no match any orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &PlaceOrderCommand{
			ID:    "ioc",
			Type:  IOC,
			Side:  Buy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeReject, trade.Type)
		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(3), stats.AskDepthCount)
		assert.Equal(t, int64(3), stats.BidDepthCount)
	})

	t.Run("take all orders with no error", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &PlaceOrderCommand{
			ID:    "ioc",
			Type:  IOC,
			Side:  Buy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(3),
		}

		err := testOrderBook.AddOrder(ctx, order)
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

	t.Run("take all orders and finish as `cancel`", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &PlaceOrderCommand{
			ID:    "ioc",
			Type:  IOC,
			Side:  Sell,
			Price: decimal.NewFromInt(10),
			Size:  decimal.NewFromInt(4),
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)

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

		order := &PlaceOrderCommand{
			ID:    "ioc",
			Type:  IOC,
			Side:  Buy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(2),
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)

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

		order := &PlaceOrderCommand{
			ID:    "fok",
			Type:  FOK,
			Side:  Buy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeReject, trade.Type)
		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(3), stats.AskDepthCount)
		assert.Equal(t, int64(3), stats.BidDepthCount)
	})

	t.Run("take all orders with no error", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &PlaceOrderCommand{
			ID:    "fok",
			Type:  FOK,
			Side:  Buy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(3),
		}

		err := testOrderBook.AddOrder(ctx, order)
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

	t.Run("take all orders and finish as `cancel`", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &PlaceOrderCommand{
			ID:    "fok",
			Type:  FOK,
			Side:  Sell,
			Price: decimal.NewFromInt(10),
			Size:  decimal.NewFromInt(4),
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeReject, trade.Type)
		stats, err := testOrderBook.GetStats()
		assert.NoError(t, err)
		assert.Equal(t, int64(3), stats.AskDepthCount)
		assert.Equal(t, int64(3), stats.BidDepthCount)
	})

	t.Run("take some orders and finish as `cancel`", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &PlaceOrderCommand{
			ID:    "ioc",
			Type:  FOK,
			Side:  Buy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(2),
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeReject, trade.Type)
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
		err := orderBook.AddOrder(ctx, &PlaceOrderCommand{
			ID:     "sell-1",
			Type:   Limit,
			Side:   Sell,
			Size:   decimal.NewFromInt(3),
			Price:  decimal.NewFromInt(110),
			UserID: 201,
		})
		assert.NoError(t, err)

		err = orderBook.AddOrder(ctx, &PlaceOrderCommand{
			ID:     "sell-2",
			Type:   Limit,
			Side:   Sell,
			Size:   decimal.NewFromInt(2),
			Price:  decimal.NewFromInt(110),
			UserID: 202,
		})
		assert.NoError(t, err)

		assert.Eventually(t, func() bool {
			stats, err := orderBook.GetStats()
			return err == nil && stats.AskDepthCount == 1 // Both orders at same price 110
		}, 1*time.Second, 10*time.Millisecond)

		// FOK Buy: Price=115, Size=5
		// Expected: Should fully match since total size at price 110 is exactly 5
		fokOrder := &PlaceOrderCommand{
			ID:     "fok-buy",
			Type:   FOK,
			Side:   Buy,
			Price:  decimal.NewFromInt(115),
			Size:   decimal.NewFromInt(5),
			UserID: 301,
		}

		err = orderBook.AddOrder(ctx, fokOrder)
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
			assert.Equal(t, LogTypeMatch, log1.Type, "Third log should be Match")
			assert.Equal(t, LogTypeMatch, log2.Type, "Fourth log should be Match")
		}

		assert.Equal(t, int64(0), orderBook.askQueue.depthCount(), "All sell orders should be matched")
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
		err := orderBook.AddOrder(ctx, &PlaceOrderCommand{
			ID:     "sell-1",
			Type:   Limit,
			Side:   Sell,
			Size:   decimal.NewFromInt(2),
			Price:  decimal.NewFromInt(110),
			UserID: 201,
		})
		assert.NoError(t, err)

		err = orderBook.AddOrder(ctx, &PlaceOrderCommand{
			ID:     "sell-2",
			Type:   Limit,
			Side:   Sell,
			Size:   decimal.NewFromInt(3),
			Price:  decimal.NewFromInt(120),
			UserID: 202,
		})
		assert.NoError(t, err)

		assert.Eventually(t, func() bool {
			stats, err := orderBook.GetStats()
			return err == nil && stats.AskDepthCount == 2
		}, 1*time.Second, 10*time.Millisecond)

		// FOK Buy: Price=125, Size=5
		// Expected: Should fully match (consume 2 at 110 + 3 at 120 = 5)
		fokOrder := &PlaceOrderCommand{
			ID:     "fok-buy",
			Type:   FOK,
			Side:   Buy,
			Price:  decimal.NewFromInt(125),
			Size:   decimal.NewFromInt(5),
			UserID: 301,
		}

		err = orderBook.AddOrder(ctx, fokOrder)
		assert.NoError(t, err)

		// Expected: 2 setup + 2 matches = 4
		memoryPublishTrader, _ := orderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 4
		}, 1*time.Second, 10*time.Millisecond)

		assert.Equal(t, int64(0), orderBook.askQueue.depthCount(), "All sell orders should be matched")
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
			err := orderBook.AddOrder(ctx, &PlaceOrderCommand{
				ID:     "sell-" + decimal.NewFromInt(int64(i)).String(),
				Type:   Limit,
				Side:   Sell,
				Size:   decimal.NewFromInt(1),
				Price:  decimal.NewFromInt(110),
				UserID: int64(200 + i),
			})
			assert.NoError(t, err)
		}

		assert.Eventually(t, func() bool {
			stats, err := orderBook.GetStats()
			return err == nil && stats.AskDepthCount == 1 // 3 setup at same price should have depth 1
		}, 1*time.Second, 10*time.Millisecond)

		// FOK Buy: Price=115, Size=3
		// Expected: Should fully match
		fokOrder := &PlaceOrderCommand{
			ID:     "fok-buy",
			Type:   FOK,
			Side:   Buy,
			Price:  decimal.NewFromInt(115),
			Size:   decimal.NewFromInt(3),
			UserID: 301,
		}

		err := orderBook.AddOrder(ctx, fokOrder)
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
	ctx := context.Background()

	testOrderBook := createTestOrderBook(t)

	err := testOrderBook.CancelOrder(ctx, "sell-1")
	assert.NoError(t, err)
	assert.Eventually(t, func() bool {
		stats, err := testOrderBook.GetStats()
		return err == nil && stats.AskDepthCount == 2
	}, 1*time.Second, 10*time.Millisecond)

	err = testOrderBook.CancelOrder(ctx, "buy-1")
	assert.NoError(t, err)
	assert.Eventually(t, func() bool {
		stats, err := testOrderBook.GetStats()
		return err == nil && stats.BidDepthCount == 2
	}, 1*time.Second, 10*time.Millisecond)

	err = testOrderBook.CancelOrder(ctx, "aaaaaa")
	assert.NoError(t, err)
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
		err := testOrderBook.AmendOrder(ctx, "buy-1", decimal.NewFromInt(90), decimal.NewFromFloat(0.5))
		assert.NoError(t, err)
		// Verify Depth
		assert.Eventually(t, func() bool {
			depth, err := testOrderBook.Depth(10)
			return err == nil && len(depth.Bids) > 0 && depth.Bids[0].Size.String() == "0.5"
		}, 1*time.Second, 10*time.Millisecond)
		depth, err := testOrderBook.Depth(10)
		assert.NoError(t, err)
		assert.Equal(t, "0.5", depth.Bids[0].Size.String())

		// Verify Log
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		// 6 setup + 1 amend
		assert.Equal(t, 7, memoryPublishTrader.Count())
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeAmend, log.Type)
		assert.Equal(t, "0.5", log.Size.String())
		assert.Equal(t, "1", log.OldSize.String())

		// Verify Priority: Add another order at same price, match against them.
		// Add buy-new at 90.
		testOrderBook.AddOrder(ctx, &PlaceOrderCommand{ID: "buy-new", Type: Limit, Side: Buy, Price: decimal.NewFromInt(90), Size: decimal.NewFromInt(1), UserID: 401})

		// Sell matching order. Should match buy-1 (0.5) first, then buy-new.
		testOrderBook.AddOrder(ctx, &PlaceOrderCommand{ID: "sell-match", Type: Limit, Side: Sell, Price: decimal.NewFromInt(90), Size: decimal.NewFromFloat(0.5), UserID: 402})

		// Check logs for match
		// 6 setup + 1 amend + 1 open(buy-new) + 1 match(sell-match vs buy-1)
		// Check logs for match
		// 6 setup + 1 amend + 1 open(buy-new) + 1 match(sell-match vs buy-1)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() >= 9
		}, 1*time.Second, 10*time.Millisecond)
		matchLog := memoryPublishTrader.Get(8)
		assert.Equal(t, LogTypeMatch, matchLog.Type)
		assert.Equal(t, "buy-1", matchLog.MakerOrderID) // Priority kept!
		assert.Equal(t, int64(101), matchLog.MakerUserID)
		assert.Equal(t, "sell-match", matchLog.OrderID)
		assert.Equal(t, int64(402), matchLog.UserID)
	})

	t.Run("increase size loses priority", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		// Initial: Buy 90(1)

		// 1. Add another order at 90 to compete
		testOrderBook.AddOrder(ctx, &PlaceOrderCommand{ID: "buy-2-compete", Type: Limit, Side: Buy, Price: decimal.NewFromInt(90), Size: decimal.NewFromInt(1)})

		// 2. Amend buy-1 size from 1 to 2 (Increase) -> Should lose priority to buy-2-compete
		err := testOrderBook.AmendOrder(ctx, "buy-1", decimal.NewFromInt(90), decimal.NewFromInt(2))
		assert.NoError(t, err)

		// 3. Sell matching order. Should match buy-2-compete first.
		testOrderBook.AddOrder(ctx, &PlaceOrderCommand{ID: "sell-match", Type: Limit, Side: Sell, Price: decimal.NewFromInt(90), Size: decimal.NewFromInt(1)})

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		// Find match log
		assert.Eventually(t, func() bool {
			found := false
			for i := 0; i < memoryPublishTrader.Count(); i++ {
				log := memoryPublishTrader.Get(i)
				if log.Type == LogTypeMatch && log.OrderID == "sell-match" {
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
		err := testOrderBook.AmendOrder(ctx, "buy-1", decimal.NewFromInt(95), decimal.NewFromInt(1))
		assert.NoError(t, err)
		assert.Eventually(t, func() bool {
			depth, err := testOrderBook.Depth(10)
			return err == nil && depth.Bids[0].Price.String() == "95"
		}, 1*time.Second, 10*time.Millisecond)

		depth, err := testOrderBook.Depth(10)
		assert.NoError(t, err)
		assert.Equal(t, "95", depth.Bids[0].Price.String())
		assert.Equal(t, "1", depth.Bids[0].Size.String())

		// Old price 90 should be gone (or have other orders)
		// In createTestOrderBook, we had 90, 80, 70. Now 90 moved to 95.
		// Next bid should be 80.
		assert.Equal(t, "80", depth.Bids[1].Price.String())
	})

	t.Run("change price and size simultaneously", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		// Buy-1 at 90, Size 1.

		// Amend Price to 95 AND Size to 5
		err := testOrderBook.AmendOrder(ctx, "buy-1", decimal.NewFromInt(95), decimal.NewFromInt(5))
		assert.NoError(t, err)
		assert.Eventually(t, func() bool {
			depth, err := testOrderBook.Depth(10)
			return err == nil && depth.Bids[0].Price.String() == "95"
		}, 1*time.Second, 10*time.Millisecond)

		depth, err := testOrderBook.Depth(10)
		assert.NoError(t, err)
		assert.Equal(t, "95", depth.Bids[0].Price.String())
		assert.Equal(t, "5", depth.Bids[0].Size.String())

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		log := memoryPublishTrader.Get(6) // 6 setup + 1 amend
		assert.Equal(t, LogTypeAmend, log.Type)
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
		err := testOrderBook.AmendOrder(ctx, "buy-1", decimal.NewFromInt(115), decimal.NewFromInt(2))
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		// 6 setup + 1 amend + 1 match + 1 open (remaining)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 9
		}, 1*time.Second, 10*time.Millisecond)

		// Verify Amend Log
		amendLog := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeAmend, amendLog.Type)
		assert.Equal(t, "115", amendLog.Price.String())
		assert.Equal(t, "2", amendLog.Size.String())

		// Verify Match Log
		matchLog := memoryPublishTrader.Get(7)
		assert.Equal(t, LogTypeMatch, matchLog.Type)
		assert.Equal(t, "buy-1", matchLog.OrderID)
		assert.Equal(t, "sell-1", matchLog.MakerOrderID)
		assert.Equal(t, "110", matchLog.Price.String()) // Matched at Maker price
		assert.Equal(t, "1", matchLog.Size.String())

		// Verify Open Log (Remaining part)
		openLog := memoryPublishTrader.Get(8)
		assert.Equal(t, LogTypeOpen, openLog.Type)
		assert.Equal(t, "buy-1", openLog.OrderID)
		assert.Equal(t, "115", openLog.Price.String())
		assert.Equal(t, "1", openLog.Size.String())

		// Verify Depth: Buy-1 remaining 1 at 115
		depth, err := testOrderBook.Depth(10)
		assert.NoError(t, err)
		assert.Equal(t, "115", depth.Bids[0].Price.String())
		assert.Equal(t, "1", depth.Bids[0].Size.String())
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
	t.Run("Shutdown blocks until drain completes", func(t *testing.T) {
		ctx := context.Background()
		publishTrader := NewMemoryPublishLog()
		orderBook := NewOrderBook("BTC-USDT", publishTrader)

		// Start the order book in a goroutine
		startDone := make(chan error)
		go func() {
			startDone <- orderBook.Start()
		}()

		// Add some orders
		for i := 0; i < 10; i++ {
			order := &PlaceOrderCommand{
				ID:     "order-" + string(rune('a'+i)),
				Type:   Limit,
				Side:   Buy,
				Size:   decimal.NewFromInt(1),
				Price:  decimal.NewFromInt(int64(100 - i)),
				UserID: int64(i),
			}
			err := orderBook.AddOrder(ctx, order)
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
		ctx := context.Background()
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
		order := &PlaceOrderCommand{
			ID:     "after-shutdown",
			Type:   Limit,
			Side:   Buy,
			Size:   decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(100),
			UserID: 999,
		}
		err = orderBook.AddOrder(ctx, order)
		assert.Equal(t, ErrShutdown, err)
	})

	t.Run("Shutdown respects context timeout", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := NewOrderBook("BTC-USDT", publishTrader)

		// Do NOT start the order book - this simulates Start() not being called
		// The drain will never happen, so Shutdown should timeout

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
		ctx := context.Background()
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
		order := &PlaceOrderCommand{
			ID:    "ioc-1",
			Type:  IOC,
			Side:  Buy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}
		err := orderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		assert.Eventually(t, func() bool {
			return publishTrader.Count() >= 1
		}, 1*time.Second, 10*time.Millisecond)

		log := publishTrader.Get(0)
		assert.Equal(t, LogTypeReject, log.Type)
		assert.Equal(t, RejectReasonNoLiquidity, log.RejectReason)
	})

	t.Run("IOC price mismatch", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// IOC Buy at 100, but best ask is 110 - should reject with PriceMismatch
		order := &PlaceOrderCommand{
			ID:    "ioc-price",
			Type:  IOC,
			Side:  Buy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}
		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeReject, log.Type)
		assert.Equal(t, RejectReasonPriceMismatch, log.RejectReason)
	})

	t.Run("FOK insufficient size", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// FOK Buy for 10 units, but only 3 available - should reject with InsufficientSize
		order := &PlaceOrderCommand{
			ID:    "fok-size",
			Type:  FOK,
			Side:  Buy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(10),
		}
		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeReject, log.Type)
		assert.Equal(t, RejectReasonInsufficientSize, log.RejectReason)
	})

	t.Run("FOK price mismatch", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// FOK Buy at 100, but best ask is 110 - should reject with PriceMismatch
		order := &PlaceOrderCommand{
			ID:    "fok-price",
			Type:  FOK,
			Side:  Buy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}
		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeReject, log.Type)
		assert.Equal(t, RejectReasonPriceMismatch, log.RejectReason)
	})

	t.Run("PostOnly would cross spread", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// PostOnly Buy at 115, but best ask is 110 - would cross, should reject
		order := &PlaceOrderCommand{
			ID:    "post-only-cross",
			Type:  PostOnly,
			Side:  Buy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(1),
		}
		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeReject, log.Type)
		assert.Equal(t, RejectReasonWouldCrossSpread, log.RejectReason)
	})

	t.Run("Market no liquidity", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		orderBook := NewOrderBook("BTC-USDT", publishTrader)
		go func() { _ = orderBook.Start() }()

		// Empty order book - Market should be rejected with NoLiquidity
		order := &PlaceOrderCommand{
			ID:   "market-1",
			Type: Market,
			Side: Buy,
			Size: decimal.NewFromInt(100),
		}
		err := orderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		assert.Eventually(t, func() bool {
			return publishTrader.Count() >= 1
		}, 1*time.Second, 10*time.Millisecond)
		log := publishTrader.Get(0)
		assert.Equal(t, LogTypeReject, log.Type)
		assert.Equal(t, RejectReasonNoLiquidity, log.RejectReason)
	})
}

func TestMatchAmount(t *testing.T) {
	ctx := context.Background()

	t.Run("match amount calculation", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Buy order that matches sell-1 at price 110, size 1 -> Amount should be 110
		order := &PlaceOrderCommand{
			ID:    "buy-match",
			Type:  Limit,
			Side:  Buy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(1),
		}
		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)
		matchLog := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeMatch, matchLog.Type)
		assert.Equal(t, "110", matchLog.Price.String())
		assert.Equal(t, "1", matchLog.Size.String())
		assert.Equal(t, "110", matchLog.Amount.String()) // Price * Size = 110 * 1 = 110
	})

	t.Run("multiple matches across price levels", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Buy order that matches multiple sell orders
		order := &PlaceOrderCommand{
			ID:    "buy-all",
			Type:  Limit,
			Side:  Buy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(3),
		}
		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 9
		}, 1*time.Second, 10*time.Millisecond)

		// Match 1: sell-1 at 110, size 1 -> Amount 110
		match1 := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeMatch, match1.Type)
		assert.Equal(t, "110", match1.Amount.String())

		// Match 2: sell-2 at 120, size 1 -> Amount 120
		match2 := memoryPublishTrader.Get(7)
		assert.Equal(t, LogTypeMatch, match2.Type)
		assert.Equal(t, "120", match2.Amount.String())

		// Match 3: sell-3 at 130, size 1 -> Amount 130
		match3 := memoryPublishTrader.Get(8)
		assert.Equal(t, LogTypeMatch, match3.Type)
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
			assert.Equal(t, LogTypeOpen, log.Type)
			assert.Equal(t, uint64(0), log.TradeID, "Open event should have TradeID 0")
		}

		// Add a matching order
		order := &PlaceOrderCommand{
			ID:    "buy-match",
			Type:  Limit,
			Side:  Buy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(1),
		}
		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		// Match event should have TradeID > 0
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)
		matchLog := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeMatch, matchLog.Type)
		assert.Greater(t, matchLog.TradeID, uint64(0), "Match event should have TradeID > 0")
	})

	t.Run("trade ID sequential", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// Buy order that matches all 3 sell orders
		order := &PlaceOrderCommand{
			ID:    "buy-all",
			Type:  Limit,
			Side:  Buy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(3),
		}
		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 9
		}, 1*time.Second, 10*time.Millisecond)

		// 3 matches should have sequential TradeIDs
		match1 := memoryPublishTrader.Get(6)
		match2 := memoryPublishTrader.Get(7)
		match3 := memoryPublishTrader.Get(8)

		assert.Equal(t, LogTypeMatch, match1.Type)
		assert.Equal(t, LogTypeMatch, match2.Type)
		assert.Equal(t, LogTypeMatch, match3.Type)

		assert.Equal(t, match1.TradeID+1, match2.TradeID, "TradeIDs should be sequential")
		assert.Equal(t, match2.TradeID+1, match3.TradeID, "TradeIDs should be sequential")
	})

	t.Run("reject events have no trade ID", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		// FOK that cannot be filled
		order := &PlaceOrderCommand{
			ID:    "fok-reject",
			Type:  FOK,
			Side:  Buy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}
		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishLog)
		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 7
		}, 1*time.Second, 10*time.Millisecond)
		rejectLog := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeReject, rejectLog.Type)
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
		err := book.AddOrder(ctx, &PlaceOrderCommand{
			ID:     "bid-1",
			Type:   Limit,
			Side:   Buy,
			Price:  decimal.NewFromInt(100),
			Size:   decimal.NewFromInt(10),
			UserID: 1,
		})
		assert.NoError(t, err)

		err = book.AddOrder(ctx, &PlaceOrderCommand{
			ID:     "ask-1",
			Type:   Limit,
			Side:   Sell,
			Price:  decimal.NewFromInt(110),
			Size:   decimal.NewFromInt(5),
			UserID: 2,
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
		assert.Equal(t, decimal.NewFromInt(100), bid.Price)

		ask := restoredBook.askQueue.order("ask-1")
		assert.NotNil(t, ask)
		assert.Equal(t, decimal.NewFromInt(110), ask.Price)

		// 4. Start Restored Book and verify it functions
		go func() { _ = restoredBook.Start() }()

		assert.Eventually(t, func() bool {
			stats, _ := restoredBook.GetStats()
			return stats.BidOrderCount == 1 && stats.AskOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		// Add a matching order to prove continuity
		err = restoredBook.AddOrder(ctx, &PlaceOrderCommand{
			ID:    "buy-match",
			Type:  Limit,
			Side:  Buy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(1),
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
		assert.Equal(t, decimal.NewFromInt(4).String(), askAfter.Size.String())
	})
}
