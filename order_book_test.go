package match

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
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
	orderBook := NewOrderBook()

	orderBuy1 := Order{
		ID:    "buy-1",
		Type:  OrderTypeLimit,
		Side:  SideBuy,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(90),
	}

	trades, err := orderBook.PlaceOrder(&orderBuy1)
	suite.Nil(err)
	suite.Equal(0, len(trades))

	orderBuy2 := Order{
		ID:    "buy-2",
		Type:  OrderTypeLimit,
		Side:  SideBuy,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(80),
	}

	trades, err = orderBook.PlaceOrder(&orderBuy2)
	suite.Nil(err)
	suite.Equal(0, len(trades))

	orderBuy3 := Order{
		ID:    "buy-3",
		Type:  OrderTypeLimit,
		Side:  SideBuy,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(70),
	}

	trades, err = orderBook.PlaceOrder(&orderBuy3)
	suite.Nil(err)
	suite.Equal(0, len(trades))

	orderSell1 := Order{
		ID:    "sell-1",
		Type:  OrderTypeLimit,
		Side:  SideSell,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(110),
	}
	trades, err = orderBook.PlaceOrder(&orderSell1)
	suite.Nil(err)
	suite.Equal(0, len(trades))

	orderSell2 := Order{
		ID:    "sell-2",
		Type:  OrderTypeLimit,
		Side:  SideSell,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(120),
	}
	trades, err = orderBook.PlaceOrder(&orderSell2)
	suite.Nil(err)
	suite.Equal(0, len(trades))

	orderSell3 := Order{
		ID:    "sell-3",
		Type:  OrderTypeLimit,
		Side:  SideSell,
		Size:  decimal.NewFromInt(1),
		Price: decimal.NewFromInt(130),
	}
	trades, err = orderBook.PlaceOrder(&orderSell3)
	suite.Nil(err)
	suite.Equal(0, len(trades))

	suite.orderbook = orderBook
}

func (suite *OrderBookTestSuite) TestLimitOrders() {
	suite.Run("take all orders", func() {
		suite.SetupTest()

		order := Order{
			ID:    "buyAll",
			Type:  OrderTypeLimit,
			Side:  SideBuy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(10),
		}

		trades, err := suite.orderbook.PlaceOrder(&order)
		suite.Nil(err)
		suite.Len(trades, 3)
		suite.Equal(int64(0), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(4), suite.orderbook.bidQueue.depthCount())

	})

	suite.Run("take some orders", func() {
		suite.SetupTest()

		order := Order{
			ID:    "mysell",
			Type:  OrderTypeLimit,
			Side:  SideSell,
			Size:  decimal.NewFromInt(5),
			Price: decimal.NewFromInt(75),
		}
		trades, err := suite.orderbook.PlaceOrder(&order)
		suite.Nil(err)
		suite.Equal(2, len(trades))
		suite.Equal(int64(4), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(1), suite.orderbook.bidQueue.depthCount())
	})
}

func (suite *OrderBookTestSuite) TestMarketOrder() {
	suite.Run("take all orders", func() {
		suite.SetupTest()

		order := Order{
			ID:    "buyAll",
			Type:  OrderTypeMarket,
			Side:  SideBuy,
			Price: decimal.NewFromInt(0),
			Size:  decimal.NewFromInt(110).Add(decimal.NewFromInt(120)).Add(decimal.NewFromInt(130)),
		}

		trades, err := suite.orderbook.PlaceOrder(&order)
		suite.Nil(err)
		suite.Len(trades, 3)
		suite.Equal(int64(0), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})

	suite.Run("take some orders", func() {
		suite.SetupTest()

		order := Order{
			ID:    "mysell",
			Type:  OrderTypeMarket,
			Side:  SideSell,
			Price: decimal.NewFromInt(0),
			Size:  decimal.NewFromInt(90),
		}

		trades, err := suite.orderbook.PlaceOrder(&order)
		suite.Nil(err)
		suite.Len(trades, 1)
		suite.Equal(int64(3), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(2), suite.orderbook.bidQueue.depthCount())
	})
}

func (suite *OrderBookTestSuite) TestPostOnlyOrder() {
	suite.Run("place a post only order", func() {
		suite.SetupTest()

		buyAll := Order{
			ID:    "post_only",
			Type:  OrderTypePostOnly,
			Side:  SideBuy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}

		trades, err := suite.orderbook.PlaceOrder(&buyAll)
		suite.Nil(err)
		suite.Len(trades, 0)
		suite.Equal(int64(3), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(4), suite.orderbook.bidQueue.depthCount())
	})

	suite.Run("fail to place a post only order", func() {
		suite.SetupTest()

		buyAll := Order{
			ID:    "post_only",
			Type:  OrderTypePostOnly,
			Side:  SideBuy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(1),
		}

		trades, err := suite.orderbook.PlaceOrder(&buyAll)
		suite.Error(err)
		suite.Len(trades, 0)
		suite.Equal(int64(3), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})
}

func (suite *OrderBookTestSuite) TestIOCOrder() {
	suite.Run("no match any orders", func() {
		suite.SetupTest()

		order := Order{
			ID:    "ioc",
			Type:  OrderTypeIOC,
			Side:  SideBuy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}

		trades, err := suite.orderbook.PlaceOrder(&order)
		suite.Error(err)
		suite.Len(trades, 0)
		suite.Equal(int64(3), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})

	suite.Run("take all orders with no error", func() {
		suite.SetupTest()

		order := Order{
			ID:    "ioc",
			Type:  OrderTypeIOC,
			Side:  SideBuy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(3),
		}

		trades, err := suite.orderbook.PlaceOrder(&order)
		suite.Nil(err)
		suite.Len(trades, 3)
		suite.Equal(int64(0), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})

	suite.Run("take all orders and finish as `cancel`", func() {
		suite.SetupTest()

		order := Order{
			ID:    "ioc",
			Type:  OrderTypeIOC,
			Side:  SideSell,
			Price: decimal.NewFromInt(10),
			Size:  decimal.NewFromInt(4),
		}

		trades, err := suite.orderbook.PlaceOrder(&order)
		suite.Error(err)
		suite.Len(trades, 3)
		suite.Equal(int64(3), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(0), suite.orderbook.bidQueue.depthCount())
	})

	suite.Run("take some orders and finish as `cancel`", func() {
		suite.SetupTest()

		order := Order{
			ID:    "ioc",
			Type:  OrderTypeIOC,
			Side:  SideBuy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(2),
		}

		trades, err := suite.orderbook.PlaceOrder(&order)
		suite.Error(err)
		suite.Len(trades, 1)
		suite.Equal(int64(2), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})
}

func (suite *OrderBookTestSuite) TestFOKOrder() {
	suite.Run("no match any orders", func() {
		suite.SetupTest()

		order := Order{
			ID:    "fok",
			Type:  OrderTypeFOK,
			Side:  SideBuy,
			Price: decimal.NewFromInt(100),
			Size:  decimal.NewFromInt(1),
		}

		trades, err := suite.orderbook.PlaceOrder(&order)
		suite.Error(err)
		suite.Len(trades, 0)
		suite.Equal(int64(3), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})

	suite.Run("take all orders with no error", func() {
		suite.SetupTest()

		order := Order{
			ID:    "fok",
			Type:  OrderTypeFOK,
			Side:  SideBuy,
			Price: decimal.NewFromInt(1000),
			Size:  decimal.NewFromInt(3),
		}

		trades, err := suite.orderbook.PlaceOrder(&order)
		suite.Nil(err)
		suite.Len(trades, 3)
		suite.Equal(int64(0), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})

	suite.Run("take all orders and finish as `cancel`", func() {
		suite.SetupTest()

		order := Order{
			ID:    "fok",
			Type:  OrderTypeFOK,
			Side:  SideSell,
			Price: decimal.NewFromInt(10),
			Size:  decimal.NewFromInt(4),
		}

		trades, err := suite.orderbook.PlaceOrder(&order)
		suite.Error(err)
		suite.Len(trades, 0)
		suite.Equal(int64(3), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})

	suite.Run("take some orders and finish as `cancel`", func() {
		suite.SetupTest()

		order := Order{
			ID:    "ioc",
			Type:  OrderTypeFOK,
			Side:  SideBuy,
			Price: decimal.NewFromInt(115),
			Size:  decimal.NewFromInt(2),
		}

		trades, err := suite.orderbook.PlaceOrder(&order)
		suite.Error(err)
		suite.Len(trades, 0)
		suite.Equal(int64(3), suite.orderbook.askQueue.depthCount())
		suite.Equal(int64(3), suite.orderbook.bidQueue.depthCount())
	})
}

func (suite *OrderBookTestSuite) TestCancelOrder() {
	suite.orderbook.CancelOrder("sell-1")
	suite.Equal(int64(2), suite.orderbook.askQueue.depthCount())

	suite.orderbook.CancelOrder("buy-1")
	suite.Equal(int64(2), suite.orderbook.bidQueue.depthCount())
}

func TestOrderBookUpdateEvents(t *testing.T) {
	updateEventChan := make(chan *OrderBookUpdateEvent, 1000)

	orderBook := NewOrderBook()
	orderBook.RegisterUpdateEventChan(updateEventChan)

	orderBuy1 := Order{
		ID:    "buy-1",
		Type:  OrderTypeLimit,
		Price: decimal.NewFromInt(100),
		Size:  decimal.NewFromInt(1),
		Side:  SideBuy,
	}
	orderBook.PlaceOrder(&orderBuy1)

	orderSell1 := Order{
		ID:    "sell-1",
		Type:  OrderTypeLimit,
		Price: decimal.NewFromInt(101),
		Size:  decimal.NewFromInt(2),
		Side:  SideSell,
	}
	orderBook.PlaceOrder(&orderSell1)

	time.Sleep(1 * time.Second)

	bookEvt := <-updateEventChan
	assert.Equal(t, 1, len(bookEvt.Bids))

	bidEvt := bookEvt.Bids[0]
	assert.Equal(t, "100", bidEvt.Price)
	assert.Equal(t, "1", bidEvt.Size)

	assert.Equal(t, 1, len(bookEvt.Asks))
	askEvt := bookEvt.Asks[0]
	assert.Equal(t, "101", askEvt.Price)
	assert.Equal(t, "2", askEvt.Size)
}
