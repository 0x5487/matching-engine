package match

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type OrderBookTestSuite struct {
	suite.Suite
}

func TestOrderBookTestSuite(t *testing.T) {
	orderBookTestSuite := &OrderBookTestSuite{}
	suite.Run(t, orderBookTestSuite)
}

func (suite *OrderBookTestSuite) createTestOrderBook() *OrderBook {
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
	suite.NoError(err)

	orderBuy2 := &Order{
		ID:    "buy-2",
		Type:  Limit,
		Side:  Buy,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(80),
	}

	err = orderBook.AddOrder(ctx, orderBuy2)
	suite.NoError(err)

	orderBuy3 := &Order{
		ID:    "buy-3",
		Type:  Limit,
		Side:  Buy,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(70),
	}

	err = orderBook.AddOrder(ctx, orderBuy3)
	suite.NoError(err)

	orderSell1 := &Order{
		ID:    "sell-1",
		Type:  Limit,
		Side:  Sell,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(110),
	}
	err = orderBook.AddOrder(ctx, orderSell1)
	suite.NoError(err)

	orderSell2 := &Order{
		ID:    "sell-2",
		Type:  Limit,
		Side:  Sell,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(120),
	}
	err = orderBook.AddOrder(ctx, orderSell2)
	suite.NoError(err)

	orderSell3 := &Order{
		ID:    "sell-3",
		Type:  Limit,
		Side:  Sell,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(130),
	}
	err = orderBook.AddOrder(ctx, orderSell3)
	suite.NoError(err)

	time.Sleep(50 * time.Millisecond)

	return orderBook
}

func (suite *OrderBookTestSuite) TestLimitOrders() {
	ctx := context.Background()

	suite.Run("take all orders", func() {
		testOrderBook := suite.createTestOrderBook()

		order := &Order{
			ID:    "buyAll",
			Type:  Limit,
			Side:  Buy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(10),
		}

		err := testOrderBook.AddOrder(ctx, order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		suite.Equal(memoryPublishTrader.Count(), 3)

		suite.Equal(int64(0), testOrderBook.askQueue.depthCount())
		suite.Equal(int64(4), testOrderBook.bidQueue.depthCount())

	})

	suite.Run("take some orders", func() {
		testOrderBook := suite.createTestOrderBook()

		order := &Order{
			ID:    "mysell",
			Type:  Limit,
			Side:  Sell,
			Size:  decimal.NewFromInt(5),
			Price: decimal.NewFromInt(75),
		}
		err := testOrderBook.AddOrder(ctx, order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		suite.Equal(memoryPublishTrader.Count(), 2)

		suite.Equal(int64(4), testOrderBook.askQueue.depthCount())
		suite.Equal(int64(1), testOrderBook.bidQueue.depthCount())
	})
}

func (suite *OrderBookTestSuite) TestMarketOrder() {
	ctx := context.Background()

	suite.Run("take all orders", func() {
		testOrderBook := suite.createTestOrderBook()

		order := &Order{
			ID:    "buyAll",
			Type:  Market,
			Side:  Buy,
			Price: decimal.NewFromInt(0),
			Size:  decimal.NewFromInt(110).Add(decimal.NewFromInt(120)).Add(decimal.NewFromInt(130)),
		}

		err := testOrderBook.AddOrder(ctx, order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		suite.Equal(memoryPublishTrader.Count(), 3)

		suite.Equal(int64(0), testOrderBook.askQueue.depthCount())
		suite.Equal(int64(3), testOrderBook.bidQueue.depthCount())
	})

	suite.Run("take some orders", func() {
		testOrderBook := suite.createTestOrderBook()

		order := &Order{
			ID:    "mysell",
			Type:  Market,
			Side:  Sell,
			Price: decimal.NewFromInt(0),
			Size:  decimal.NewFromInt(90),
		}

		err := testOrderBook.AddOrder(ctx, order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		suite.Equal(memoryPublishTrader.Count(), 1)

		suite.Equal(int64(3), testOrderBook.askQueue.depthCount())
		suite.Equal(int64(2), testOrderBook.bidQueue.depthCount())
	})
}

func (suite *OrderBookTestSuite) TestPostOnlyOrder() {
	ctx := context.Background()

	suite.Run("place a post only order", func() {
		testOrderBook := suite.createTestOrderBook()

		buyAll := &Order{
			ID:    "post_only",
			Type:  PostOnly,
			Side:  Buy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}

		err := testOrderBook.AddOrder(ctx, buyAll)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		suite.Equal(memoryPublishTrader.Count(), 0)

		suite.Equal(int64(3), testOrderBook.askQueue.depthCount())
		suite.Equal(int64(4), testOrderBook.bidQueue.depthCount())
	})

	suite.Run("fail to place a post only order", func() {
		testOrderBook := suite.createTestOrderBook()

		buyAll := &Order{
			ID:    "post_only",
			Type:  PostOnly,
			Side:  Buy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(1),
		}

		err := testOrderBook.AddOrder(ctx, buyAll)
		suite.NoError(err)
		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		suite.Equal(memoryPublishTrader.Count(), 1)

		trade := memoryPublishTrader.Trades[0]
		suite.Equal(true, trade.IsCancel)
		suite.Equal(int64(3), testOrderBook.askQueue.depthCount())
		suite.Equal(int64(3), testOrderBook.bidQueue.depthCount())
	})
}

func (suite *OrderBookTestSuite) TestIOCOrder() {
	ctx := context.Background()

	suite.Run("no match any orders", func() {
		testOrderBook := suite.createTestOrderBook()

		order := &Order{
			ID:    "ioc",
			Type:  IOC,
			Side:  Buy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}

		err := testOrderBook.AddOrder(ctx, order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		suite.Equal(memoryPublishTrader.Count(), 1)

		trade := memoryPublishTrader.Trades[0]
		suite.Equal(true, trade.IsCancel)
		suite.Equal(int64(3), testOrderBook.askQueue.depthCount())
		suite.Equal(int64(3), testOrderBook.bidQueue.depthCount())
	})

	suite.Run("take all orders with no error", func() {
		testOrderBook := suite.createTestOrderBook()

		order := &Order{
			ID:    "ioc",
			Type:  IOC,
			Side:  Buy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(3),
		}

		err := testOrderBook.AddOrder(ctx, order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		suite.Equal(memoryPublishTrader.Count(), 3)

		suite.Equal(int64(0), testOrderBook.askQueue.depthCount())
		suite.Equal(int64(3), testOrderBook.bidQueue.depthCount())
	})

	suite.Run("take all orders and finish as `cancel`", func() {
		testOrderBook := suite.createTestOrderBook()

		order := &Order{
			ID:    "ioc",
			Type:  IOC,
			Side:  Sell,
			Price: decimal.NewFromInt(10),
			Size:  decimal.NewFromInt(4),
		}

		err := testOrderBook.AddOrder(ctx, order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		suite.Equal(memoryPublishTrader.Count(), 4)

		suite.Equal(int64(3), testOrderBook.askQueue.depthCount())
		suite.Equal(int64(0), testOrderBook.bidQueue.depthCount())
	})

	suite.Run("take some orders and finish as `cancel`", func() {
		testOrderBook := suite.createTestOrderBook()

		order := &Order{
			ID:    "ioc",
			Type:  IOC,
			Side:  Buy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(2),
		}

		err := testOrderBook.AddOrder(ctx, order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		suite.Equal(memoryPublishTrader.Count(), 2)

		suite.Equal(int64(2), testOrderBook.askQueue.depthCount())
		suite.Equal(int64(3), testOrderBook.bidQueue.depthCount())
	})
}

func (suite *OrderBookTestSuite) TestFOKOrder() {
	ctx := context.Background()

	suite.Run("no match any orders", func() {
		testOrderBook := suite.createTestOrderBook()

		order := &Order{
			ID:    "fok",
			Type:  FOK,
			Side:  Buy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}

		err := testOrderBook.AddOrder(ctx, order)
		suite.NoError(err)
		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		suite.Equal(memoryPublishTrader.Count(), 1)

		trade := memoryPublishTrader.Get(0)
		suite.Equal(true, trade.IsCancel)
		suite.Equal(int64(3), testOrderBook.askQueue.depthCount())
		suite.Equal(int64(3), testOrderBook.bidQueue.depthCount())
	})

	suite.Run("take all orders with no error", func() {
		testOrderBook := suite.createTestOrderBook()

		order := &Order{
			ID:    "fok",
			Type:  FOK,
			Side:  Buy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(3),
		}

		err := testOrderBook.AddOrder(ctx, order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		suite.Equal(memoryPublishTrader.Count(), 3)

		suite.Equal(int64(0), testOrderBook.askQueue.depthCount())
		suite.Equal(int64(3), testOrderBook.bidQueue.depthCount())
	})

	suite.Run("take all orders and finish as `cancel`", func() {
		testOrderBook := suite.createTestOrderBook()

		order := &Order{
			ID:    "fok",
			Type:  FOK,
			Side:  Sell,
			Price: decimal.NewFromInt(10),
			Size:  decimal.NewFromInt(4),
		}

		err := testOrderBook.AddOrder(ctx, order)
		suite.NoError(err)
		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		suite.Equal(memoryPublishTrader.Count(), 1)

		trade := memoryPublishTrader.Get(0)
		suite.Equal(true, trade.IsCancel)
		suite.Equal(int64(3), testOrderBook.askQueue.depthCount())
		suite.Equal(int64(3), testOrderBook.bidQueue.depthCount())
	})

	suite.Run("take some orders and finish as `cancel`", func() {
		testOrderBook := suite.createTestOrderBook()

		order := &Order{
			ID:    "ioc",
			Type:  FOK,
			Side:  Buy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(2),
		}

		err := testOrderBook.AddOrder(ctx, order)
		suite.NoError(err)
		time.Sleep(50 * time.Millisecond)

		memoryPublishTrader, _ := testOrderBook.publishTrader.(*MemoryPublishTrader)
		suite.Equal(memoryPublishTrader.Count(), 1)

		trade := memoryPublishTrader.Get(0)
		suite.Equal(true, trade.IsCancel)
		suite.Equal(int64(3), testOrderBook.askQueue.depthCount())
		suite.Equal(int64(3), testOrderBook.bidQueue.depthCount())
	})
}

func (suite *OrderBookTestSuite) TestCancelOrder() {
	ctx := context.Background()

	testOrderBook := suite.createTestOrderBook()

	err := testOrderBook.CancelOrder(ctx, "sell-1")
	suite.NoError(err)
	time.Sleep(50 * time.Millisecond)
	suite.Equal(int64(2), testOrderBook.askQueue.depthCount())

	err = testOrderBook.CancelOrder(ctx, "buy-1")
	suite.NoError(err)
	time.Sleep(50 * time.Millisecond)
	suite.Equal(int64(2), testOrderBook.bidQueue.depthCount())

	err = testOrderBook.CancelOrder(ctx, "aaaaaa")
	suite.NoError(err)
	time.Sleep(50 * time.Millisecond)
	suite.Equal(int64(2), testOrderBook.bidQueue.depthCount())
}

func (suite *OrderBookTestSuite) TestDepth() {
	testOrderBook := suite.createTestOrderBook()

	result, err := testOrderBook.Depth(5)
	suite.NoError(err)

	suite.Len(result.Asks, 3)
	suite.Len(result.Bids, 3)
}
