package match

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

type MatchingEngineTestSuite struct {
	suite.Suite
	engine *MatchingEngine
}

func TestMatchingEngineTestSuite(t *testing.T) {

	matchingEngineTestSuite := MatchingEngineTestSuite{
		engine: NewMatchingEngine(),
	}
	suite.Run(t, &matchingEngineTestSuite)
}

func (suite *MatchingEngineTestSuite) TestPlaceOrders() {
	suite.engine = NewMatchingEngine()

	// market1
	market1 := "BTC-USDT"
	order1 := Order{
		ID:       "order1",
		MarketID: market1,
		Type:     Limit,
		Side:     Buy,
		Price:    decimal.NewFromInt(100),
		Size:     decimal.NewFromInt(2),
	}

	_, err := suite.engine.PlaceOrder(&order1)
	suite.NoError(err)
	orderbook := suite.engine.orderBook(market1)
	suite.Equal(int64(1), orderbook.bidQueue.orderCount())

	// market2
	market2 := "ETH-USDT"
	order2 := Order{
		ID:       "order2",
		MarketID: market2,
		Type:     Limit,
		Side:     Sell,
		Price:    decimal.NewFromInt(110),
		Size:     decimal.NewFromInt(2),
	}

	_, err = suite.engine.PlaceOrder(&order2)
	suite.NoError(err)
	orderbook = suite.engine.orderBook(market2)
	suite.Equal(int64(1), orderbook.askQueue.orderCount())
}

func (suite *MatchingEngineTestSuite) TestCancelOrder() {
	suite.engine = NewMatchingEngine()

	market1 := "BTC-USDT"

	order1 := Order{
		ID:       "order1",
		MarketID: market1,
		Type:     Limit,
		Side:     Buy,
		Price:    decimal.NewFromInt(100),
		Size:     decimal.NewFromInt(2),
	}

	_, err := suite.engine.PlaceOrder(&order1)
	suite.NoError(err)

	order2 := Order{
		ID:       "order2",
		MarketID: market1,
		Type:     Limit,
		Side:     Sell,
		Price:    decimal.NewFromInt(110),
		Size:     decimal.NewFromInt(2),
	}

	_, err = suite.engine.PlaceOrder(&order2)
	suite.NoError(err)

	suite.engine.CancelOrder(market1, order1.ID)

	// validate
	orderbook := suite.engine.orderBook(market1)
	suite.Equal(int64(0), orderbook.bidQueue.orderCount())
	suite.Equal(int64(1), orderbook.askQueue.orderCount())
}
