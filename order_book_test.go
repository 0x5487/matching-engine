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

	publishTrader := NewMemoryPublishTrader()
	orderBook := NewOrderBook(publishTrader)
	go func() {
		_ = orderBook.Start()
	}()

	orderBuy1 := &Order{
		ID:     "buy-1",
		Type:   Limit,
		Side:   Buy,
		Size:   decimal.NewFromInt(1),
		Price:  decimal.NewFromInt(90),
		UserID: 101,
	}

	err := orderBook.AddOrder(ctx, orderBuy1)
	assert.NoError(t, err)

	orderBuy2 := &Order{
		ID:     "buy-2",
		Type:   Limit,
		Side:   Buy,
		Size:   decimal.NewFromInt(1),
		Price:  decimal.NewFromInt(80),
		UserID: 102,
	}

	err = orderBook.AddOrder(ctx, orderBuy2)
	assert.NoError(t, err)

	orderBuy3 := &Order{
		ID:     "buy-3",
		Type:   Limit,
		Side:   Buy,
		Size:   decimal.NewFromInt(1),
		Price:  decimal.NewFromInt(70),
		UserID: 103,
	}

	err = orderBook.AddOrder(ctx, orderBuy3)
	assert.NoError(t, err)

	orderSell1 := &Order{
		ID:     "sell-1",
		Type:   Limit,
		Side:   Sell,
		Size:   decimal.NewFromInt(1),
		Price:  decimal.NewFromInt(110),
		UserID: 201,
	}
	err = orderBook.AddOrder(ctx, orderSell1)
	assert.NoError(t, err)

	orderSell2 := &Order{
		ID:     "sell-2",
		Type:   Limit,
		Side:   Sell,
		Size:   decimal.NewFromInt(1),
		Price:  decimal.NewFromInt(120),
		UserID: 202,
	}
	err = orderBook.AddOrder(ctx, orderSell2)
	assert.NoError(t, err)

	orderSell3 := &Order{
		ID:     "sell-3",
		Type:   Limit,
		Side:   Sell,
		Size:   decimal.NewFromInt(1),
		Price:  decimal.NewFromInt(130),
		UserID: 203,
	}
	err = orderBook.AddOrder(ctx, orderSell3)
	assert.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	return orderBook
}

func TestLimitOrders(t *testing.T) {
	ctx := context.Background()

	t.Run("take all orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &Order{
			ID:     "buyAll",
			Type:   Limit,
			Side:   Buy,
			Price:  decimal.NewFromInt(1000),
			Size:   decimal.NewFromInt(10),
			UserID: 300,
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		assert.Equal(t, memoryPublishTrader.Count(), 10) // 6 setup + 3 matches + 1 open (remaining)
		// Sell orders: 1@110, 1@120, 1@130. Total 3. Buy order size 10.
		// Matches: 3. Remaining 7 enters book. So 1 Open log. Total 4 logs.

		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 10
		}, 1*time.Second, 10*time.Millisecond)

		assert.Equal(t, int64(0), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(4), testOrderBook.bidQueue.depthCount())

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

		order := &Order{
			ID:     "mysell",
			Type:   Limit,
			Side:   Sell,
			Size:   decimal.NewFromInt(5),
			Price:  decimal.NewFromInt(75),
			UserID: 301,
		}
		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		assert.Equal(t, memoryPublishTrader.Count(), 9) // 6 setup + 2 matches + 1 open (remaining)

		assert.Eventually(t, func() bool {
			return memoryPublishTrader.Count() == 9
		}, 1*time.Second, 10*time.Millisecond)

		assert.Equal(t, int64(4), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(1), testOrderBook.bidQueue.depthCount())

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

		order := &Order{
			ID:    "buyAll",
			Type:  Market,
			Side:  Buy,
			Price: decimal.NewFromInt(0),
			Size:  decimal.NewFromInt(110).Add(decimal.NewFromInt(120)).Add(decimal.NewFromInt(130)),
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		assert.Equal(t, memoryPublishTrader.Count(), 9) // 6 setup + 3 matches

		assert.Equal(t, int64(0), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})

	t.Run("take some orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &Order{
			ID:    "mysell",
			Type:  Market,
			Side:  Sell,
			Price: decimal.NewFromInt(0),
			Size:  decimal.NewFromInt(90),
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		assert.Equal(t, memoryPublishTrader.Count(), 7) // 6 setup + 1 match

		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(2), testOrderBook.bidQueue.depthCount())
	})
}

func TestPostOnlyOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("place a post only order", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		buyAll := &Order{
			ID:    "post_only",
			Type:  PostOnly,
			Side:  Buy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}

		err := testOrderBook.AddOrder(ctx, buyAll)
		assert.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		assert.Equal(t, memoryPublishTrader.Count(), 7) // 6 setup + 1 open

		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(4), testOrderBook.bidQueue.depthCount())
	})

	t.Run("fail to place a post only order", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		buyAll := &Order{
			ID:    "post_only",
			Type:  PostOnly,
			Side:  Buy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(1),
		}

		err := testOrderBook.AddOrder(ctx, buyAll)
		assert.NoError(t, err)
		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		assert.Equal(t, memoryPublishTrader.Count(), 7) // 6 setup + 1 cancel

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeCancel, trade.Type)
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})
}

func TestIOCOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("no match any orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &Order{
			ID:    "ioc",
			Type:  IOC,
			Side:  Buy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		assert.Equal(t, memoryPublishTrader.Count(), 7) // 6 setup + 1 cancel

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeCancel, trade.Type)
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})

	t.Run("take all orders with no error", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &Order{
			ID:    "ioc",
			Type:  IOC,
			Side:  Buy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(3),
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		assert.Equal(t, memoryPublishTrader.Count(), 9) // 6 setup + 3 matches

		assert.Equal(t, int64(0), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})

	t.Run("take all orders and finish as `cancel`", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &Order{
			ID:    "ioc",
			Type:  IOC,
			Side:  Sell,
			Price: decimal.NewFromInt(10),
			Size:  decimal.NewFromInt(4),
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		assert.Equal(t, memoryPublishTrader.Count(), 10) // 6 setup + 3 matches + 1 cancel (remaining)

		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(0), testOrderBook.bidQueue.depthCount())
	})

	t.Run("take some orders and finish as `cancel`", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &Order{
			ID:    "ioc",
			Type:  IOC,
			Side:  Buy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(2),
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		assert.Equal(t, memoryPublishTrader.Count(), 8) // 6 setup + 1 match + 1 cancel (remaining)

		assert.Equal(t, int64(2), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})
}

func TestFOKOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("no match any orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &Order{
			ID:    "fok",
			Type:  FOK,
			Side:  Buy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		assert.Equal(t, memoryPublishTrader.Count(), 7) // 6 setup + 1 cancel

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeCancel, trade.Type)
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})

	t.Run("take all orders with no error", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &Order{
			ID:    "fok",
			Type:  FOK,
			Side:  Buy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(3),
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		assert.Equal(t, memoryPublishTrader.Count(), 9) // 6 setup + 3 matches

		assert.Equal(t, int64(0), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})

	t.Run("take all orders and finish as `cancel`", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &Order{
			ID:    "fok",
			Type:  FOK,
			Side:  Sell,
			Price: decimal.NewFromInt(10),
			Size:  decimal.NewFromInt(4),
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		assert.Equal(t, memoryPublishTrader.Count(), 7) // 6 setup + 1 cancel

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeCancel, trade.Type)
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})

	t.Run("take some orders and finish as `cancel`", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &Order{
			ID:    "ioc",
			Type:  FOK,
			Side:  Buy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(2),
		}

		err := testOrderBook.AddOrder(ctx, order)
		assert.NoError(t, err)
		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		assert.Equal(t, memoryPublishTrader.Count(), 7) // 6 setup + 1 cancel

		trade := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeCancel, trade.Type)
		assert.Equal(t, int64(3), testOrderBook.askQueue.depthCount())
		assert.Equal(t, int64(3), testOrderBook.bidQueue.depthCount())
	})
}

func TestCancelOrder(t *testing.T) {
	ctx := context.Background()

	testOrderBook := createTestOrderBook(t)

	err := testOrderBook.CancelOrder(ctx, "sell-1")
	assert.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int64(2), testOrderBook.askQueue.depthCount())

	err = testOrderBook.CancelOrder(ctx, "buy-1")
	assert.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int64(2), testOrderBook.bidQueue.depthCount())

	err = testOrderBook.CancelOrder(ctx, "aaaaaa")
	assert.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int64(2), testOrderBook.bidQueue.depthCount())
}

func TestAmendOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("decrease size preserves priority", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		// Initial: Buy 90(1), 80(1), 70(1). Sell 110(1), 120(1), 130(1)

		// 1. Amend buy-1 (Price 90) size from 1 to 0.5
		err := testOrderBook.AmendOrder(ctx, "buy-1", decimal.NewFromInt(90), decimal.NewFromFloat(0.5))
		assert.NoError(t, err)
		time.Sleep(50 * time.Millisecond)

		// Verify Depth
		depth := testOrderBook.depth(10)
		assert.Equal(t, "0.5", depth.Bids[0].Size.String())

		// Verify Log
		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		// 6 setup + 1 amend
		assert.Equal(t, 7, memoryPublishTrader.Count())
		log := memoryPublishTrader.Get(6)
		assert.Equal(t, LogTypeAmend, log.Type)
		assert.Equal(t, "0.5", log.Size.String())
		assert.Equal(t, "1", log.OldSize.String())

		// Verify Priority: Add another order at same price, match against them.
		// Add buy-new at 90.
		testOrderBook.AddOrder(ctx, &Order{ID: "buy-new", Type: Limit, Side: Buy, Price: decimal.NewFromInt(90), Size: decimal.NewFromInt(1), UserID: 401})
		time.Sleep(20 * time.Millisecond)

		// Sell matching order. Should match buy-1 (0.5) first, then buy-new.
		testOrderBook.AddOrder(ctx, &Order{ID: "sell-match", Type: Limit, Side: Sell, Price: decimal.NewFromInt(90), Size: decimal.NewFromFloat(0.5), UserID: 402})
		time.Sleep(20 * time.Millisecond)

		// Check logs for match
		// 6 setup + 1 amend + 1 open(buy-new) + 1 match(sell-match vs buy-1)
		assert.True(t, memoryPublishTrader.Count() >= 9)
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
		testOrderBook.AddOrder(ctx, &Order{ID: "buy-2-compete", Type: Limit, Side: Buy, Price: decimal.NewFromInt(90), Size: decimal.NewFromInt(1)})
		time.Sleep(20 * time.Millisecond)

		// 2. Amend buy-1 size from 1 to 2 (Increase) -> Should lose priority to buy-2-compete
		err := testOrderBook.AmendOrder(ctx, "buy-1", decimal.NewFromInt(90), decimal.NewFromInt(2))
		assert.NoError(t, err)
		time.Sleep(20 * time.Millisecond)

		// 3. Sell matching order. Should match buy-2-compete first.
		testOrderBook.AddOrder(ctx, &Order{ID: "sell-match", Type: Limit, Side: Sell, Price: decimal.NewFromInt(90), Size: decimal.NewFromInt(1)})
		time.Sleep(20 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		// Find match log
		found := false
		for i := 0; i < memoryPublishTrader.Count(); i++ {
			log := memoryPublishTrader.Get(i)
			if log.Type == LogTypeMatch && log.OrderID == "sell-match" {
				assert.Equal(t, "buy-2-compete", log.MakerOrderID) // Priority lost!
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("change price moves level", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)
		// Buy-1 at 90.

		// Amend price to 95
		err := testOrderBook.AmendOrder(ctx, "buy-1", decimal.NewFromInt(95), decimal.NewFromInt(1))
		assert.NoError(t, err)
		time.Sleep(50 * time.Millisecond)

		depth := testOrderBook.depth(10)
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
		time.Sleep(50 * time.Millisecond)

		depth := testOrderBook.depth(10)
		assert.Equal(t, "95", depth.Bids[0].Price.String())
		assert.Equal(t, "5", depth.Bids[0].Size.String())

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		log := memoryPublishTrader.Get(6) // 6 setup + 1 amend
		assert.Equal(t, LogTypeAmend, log.Type)
		assert.Equal(t, "95", log.Price.String())
		assert.Equal(t, "5", log.Size.String())
		assert.Equal(t, "90", log.OldPrice.String())
		assert.Equal(t, "1", log.OldSize.String())
	})
}

func TestDepth(t *testing.T) {
	testOrderBook := createTestOrderBook(t)

	result, err := testOrderBook.Depth(5)
	assert.NoError(t, err)

	assert.Len(t, result.Asks, 3)
	assert.Len(t, result.Bids, 3)
}
