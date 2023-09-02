package match

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type OrderBookTestSuite struct {
	suite.Suite
	orderbook *OrderBook
}

func TestOrderBookTestSuite(t *testing.T) {

	orderBookTestSuite := OrderBookTestSuite{}

	suite.Run(t, &orderBookTestSuite)
}

func (suite *OrderBookTestSuite) SetupTest() {
	tradeChan := make(chan *Trade, 1000)
	orderBook := NewOrderBook(tradeChan)
	go orderBook.Start()

	orderBuy1 := Order{
		ID:    "buy-1",
		Type:  Limit,
		Side:  Buy,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(90),
	}

	err := orderBook.AddOrder(&orderBuy1)
	suite.NoError(err)

	orderBuy2 := Order{
		ID:    "buy-2",
		Type:  Limit,
		Side:  Buy,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(80),
	}

	err = orderBook.AddOrder(&orderBuy2)
	suite.NoError(err)

	orderBuy3 := Order{
		ID:    "buy-3",
		Type:  Limit,
		Side:  Buy,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(70),
	}

	err = orderBook.AddOrder(&orderBuy3)
	suite.NoError(err)

	orderSell1 := Order{
		ID:    "sell-1",
		Type:  Limit,
		Side:  Sell,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(110),
	}
	err = orderBook.AddOrder(&orderSell1)
	suite.NoError(err)

	orderSell2 := Order{
		ID:    "sell-2",
		Type:  Limit,
		Side:  Sell,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(120),
	}
	err = orderBook.AddOrder(&orderSell2)
	suite.NoError(err)

	orderSell3 := Order{
		ID:    "sell-3",
		Type:  Limit,
		Side:  Sell,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(130),
	}
	err = orderBook.AddOrder(&orderSell3)
	suite.NoError(err)

	suite.orderbook = orderBook
	time.Sleep(50 * time.Millisecond)
}

func (suite *OrderBookTestSuite) TestLimitOrders() {
	suite.Run("take all orders", func() {
		suite.SetupTest()

		order := Order{
			ID:    "buyAll",
			Type:  Limit,
			Side:  Buy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(10),
		}

		err := suite.orderbook.AddOrder(&order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)

		suite.Len(suite.orderbook.tradeChan, 3)
		suite.Equal(int64(0), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(4), suite.orderbook.bidQueue.depthCount())

	})

	suite.Run("take some orders", func() {
		suite.SetupTest()

		order := Order{
			ID:    "mysell",
			Type:  Limit,
			Side:  Sell,
			Size:  decimal.NewFromInt(5),
			Price: decimal.NewFromInt(75),
		}
		err := suite.orderbook.AddOrder(&order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)
		suite.Len(suite.orderbook.tradeChan, 2)
		suite.Equal(int64(4), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(1), suite.orderbook.bidQueue.depthCount())
	})
}

func (suite *OrderBookTestSuite) TestMarketOrder() {
	suite.Run("take all orders", func() {
		suite.SetupTest()

		order := Order{
			ID:    "buyAll",
			Type:  Market,
			Side:  Buy,
			Price: decimal.NewFromInt(0),
			Size:  decimal.NewFromInt(110).Add(decimal.NewFromInt(120)).Add(decimal.NewFromInt(130)),
		}

		err := suite.orderbook.AddOrder(&order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)
		suite.Len(suite.orderbook.tradeChan, 3)
		suite.Equal(int64(0), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})

	suite.Run("take some orders", func() {
		suite.SetupTest()

		order := Order{
			ID:    "mysell",
			Type:  Market,
			Side:  Sell,
			Price: decimal.NewFromInt(0),
			Size:  decimal.NewFromInt(90),
		}

		err := suite.orderbook.AddOrder(&order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)
		suite.Len(suite.orderbook.tradeChan, 1)
		suite.Equal(int64(3), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(2), suite.orderbook.bidQueue.depthCount())
	})
}

func (suite *OrderBookTestSuite) TestPostOnlyOrder() {
	suite.Run("place a post only order", func() {
		suite.SetupTest()

		buyAll := Order{
			ID:    "post_only",
			Type:  PostOnly,
			Side:  Buy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}

		err := suite.orderbook.AddOrder(&buyAll)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)
		suite.Len(suite.orderbook.tradeChan, 0)
		suite.Equal(int64(3), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(4), suite.orderbook.bidQueue.depthCount())
	})

	suite.Run("fail to place a post only order", func() {
		suite.SetupTest()

		buyAll := Order{
			ID:    "post_only",
			Type:  PostOnly,
			Side:  Buy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(1),
		}

		err := suite.orderbook.AddOrder(&buyAll)
		suite.NoError(err)
		time.Sleep(50 * time.Millisecond)

		suite.Len(suite.orderbook.tradeChan, 1)
		trade := <-suite.orderbook.tradeChan
		suite.Equal(true, trade.IsCancel)
		suite.Equal(int64(3), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})
}

func (suite *OrderBookTestSuite) TestIOCOrder() {
	suite.Run("no match any orders", func() {
		suite.SetupTest()

		order := Order{
			ID:    "ioc",
			Type:  IOC,
			Side:  Buy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}

		err := suite.orderbook.AddOrder(&order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)
		suite.Len(suite.orderbook.tradeChan, 1)
		trade := <-suite.orderbook.tradeChan
		suite.Equal(true, trade.IsCancel)
		suite.Equal(int64(3), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})

	suite.Run("take all orders with no error", func() {
		suite.SetupTest()

		order := Order{
			ID:    "ioc",
			Type:  IOC,
			Side:  Buy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(3),
		}

		err := suite.orderbook.AddOrder(&order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)
		suite.Len(suite.orderbook.tradeChan, 3)
		suite.Equal(int64(0), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})

	suite.Run("take all orders and finish as `cancel`", func() {
		suite.SetupTest()

		order := Order{
			ID:    "ioc",
			Type:  IOC,
			Side:  Sell,
			Price: decimal.NewFromInt(10),
			Size:  decimal.NewFromInt(4),
		}

		err := suite.orderbook.AddOrder(&order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)
		suite.Len(suite.orderbook.tradeChan, 4)
		suite.Equal(int64(3), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(0), suite.orderbook.bidQueue.depthCount())
	})

	suite.Run("take some orders and finish as `cancel`", func() {
		suite.SetupTest()

		order := Order{
			ID:    "ioc",
			Type:  IOC,
			Side:  Buy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(2),
		}

		err := suite.orderbook.AddOrder(&order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)
		suite.Len(suite.orderbook.tradeChan, 2)
		suite.Equal(int64(2), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})
}

func (suite *OrderBookTestSuite) TestFOKOrder() {
	suite.Run("no match any orders", func() {
		suite.SetupTest()

		order := Order{
			ID:    "fok",
			Type:  FOK,
			Side:  Buy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}

		err := suite.orderbook.AddOrder(&order)
		suite.NoError(err)
		time.Sleep(50 * time.Millisecond)

		suite.Len(suite.orderbook.tradeChan, 1)
		trade := <-suite.orderbook.tradeChan
		suite.Equal(true, trade.IsCancel)
		suite.Equal(int64(3), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})

	suite.Run("take all orders with no error", func() {
		suite.SetupTest()

		order := Order{
			ID:    "fok",
			Type:  FOK,
			Side:  Buy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(3),
		}

		err := suite.orderbook.AddOrder(&order)
		suite.NoError(err)

		time.Sleep(50 * time.Millisecond)
		suite.Len(suite.orderbook.tradeChan, 3)
		suite.Equal(int64(0), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})

	suite.Run("take all orders and finish as `cancel`", func() {
		suite.SetupTest()

		order := Order{
			ID:    "fok",
			Type:  FOK,
			Side:  Sell,
			Price: decimal.NewFromInt(10),
			Size:  decimal.NewFromInt(4),
		}

		err := suite.orderbook.AddOrder(&order)
		suite.NoError(err)
		time.Sleep(50 * time.Millisecond)

		suite.Len(suite.orderbook.tradeChan, 1)
		trade := <-suite.orderbook.tradeChan
		suite.Equal(true, trade.IsCancel)
		suite.Equal(int64(3), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})

	suite.Run("take some orders and finish as `cancel`", func() {
		suite.SetupTest()

		order := Order{
			ID:    "ioc",
			Type:  FOK,
			Side:  Buy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(2),
		}

		err := suite.orderbook.AddOrder(&order)
		suite.NoError(err)
		time.Sleep(50 * time.Millisecond)

		suite.Len(suite.orderbook.tradeChan, 1)
		trade := <-suite.orderbook.tradeChan
		suite.Equal(true, trade.IsCancel)
		suite.Equal(int64(3), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})
}

func (suite *OrderBookTestSuite) TestCancelOrder() {
	suite.orderbook.CancelOrder("sell-1")
	time.Sleep(50 * time.Millisecond)
	suite.Equal(int64(2), suite.orderbook.askQueue.depthCount())

	suite.orderbook.CancelOrder("buy-1")
	time.Sleep(50 * time.Millisecond)
	suite.Equal(int64(2), suite.orderbook.bidQueue.depthCount())

	suite.orderbook.CancelOrder("aaaaaa")
	time.Sleep(50 * time.Millisecond)
	suite.Equal(int64(2), suite.orderbook.bidQueue.depthCount())
}

func (suite *OrderBookTestSuite) TestDepth() {
	result, err := suite.orderbook.Depth(5)
	suite.NoError(err)

	suite.Len(result.Asks, 3)
	suite.Len(result.Bids, 3)
}
