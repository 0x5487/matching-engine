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
		ID:    "buy-1",
		Type:  Limit,
		Side:  Buy,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(90),
	}

	err := orderBook.AddOrder(ctx, orderBuy1)
	assert.NoError(t, err)

	orderBuy2 := &Order{
		ID:    "buy-2",
		Type:  Limit,
		Side:  Buy,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(80),
	}

	err = orderBook.AddOrder(ctx, orderBuy2)
	assert.NoError(t, err)

	orderBuy3 := &Order{
		ID:    "buy-3",
		Type:  Limit,
		Side:  Buy,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(70),
	}

	err = orderBook.AddOrder(ctx, orderBuy3)
	assert.NoError(t, err)

	orderSell1 := &Order{
		ID:    "sell-1",
		Type:  Limit,
		Side:  Sell,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(110),
	}
	err = orderBook.AddOrder(ctx, orderSell1)
	assert.NoError(t, err)

	orderSell2 := &Order{
		ID:    "sell-2",
		Type:  Limit,
		Side:  Sell,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(120),
	}
	err = orderBook.AddOrder(ctx, orderSell2)
	assert.NoError(t, err)

	orderSell3 := &Order{
		ID:    "sell-3",
		Type:  Limit,
		Side:  Sell,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(130),
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
			ID:    "buyAll",
			Type:  Limit,
			Side:  Buy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(10),
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

	})

	t.Run("take some orders", func(t *testing.T) {
		testOrderBook := createTestOrderBook(t)

		order := &Order{
			ID:    "mysell",
			Type:  Limit,
			Side:  Sell,
			Size:  decimal.NewFromInt(5),
			Price: decimal.NewFromInt(75),
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

func TestDepth(t *testing.T) {
	testOrderBook := createTestOrderBook(t)

	result, err := testOrderBook.Depth(5)
	assert.NoError(t, err)

	assert.Len(t, result.Asks, 3)
	assert.Len(t, result.Bids, 3)
}
