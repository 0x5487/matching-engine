package engine

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestPlaceLimitedSellOrder(t *testing.T) {
	orderBook := NewOrderBook()

	t.Run("take all limited buy orders", func(t *testing.T) {
		orderBuy1 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(100),
			Side:   Side_Buy,
			ID:     "buy-1",
		}

		trades := orderBook.PlaceLimitOrder(orderBuy1)
		assert.Nil(t, trades)

		orderSell1 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(100),
			Side:   Side_Sell,
			ID:     "sell-1",
		}
		trades = orderBook.PlaceLimitOrder(orderSell1)
		assert.Equal(t, 1, len(trades))
	})

	t.Run("place limited sell order", func(t *testing.T) {
		orderBuy1 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(100),
			Side:   Side_Buy,
			ID:     "buy-1",
		}

		trades := orderBook.PlaceLimitOrder(orderBuy1)
		assert.Nil(t, trades)

		orderBuy2 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(90),
			Side:   Side_Buy,
			ID:     "buy-2",
		}

		trades = orderBook.PlaceLimitOrder(orderBuy2)
		assert.Nil(t, trades)

		orderBuy3 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(100),
			Side:   Side_Buy,
			ID:     "buy-3",
		}

		trades = orderBook.PlaceLimitOrder(orderBuy3)
		assert.Nil(t, trades)

		orderSell1 := Order{
			Size: decimal.NewFromInt(5),
			Price:  decimal.NewFromInt(90),
			Side:   Side_Sell,
			ID:     "sell-1",
		}
		trades = orderBook.PlaceLimitOrder(orderSell1)
		assert.Equal(t, 3, len(trades))
	})
}

func TestPlaceLimitedBuyOrder(t *testing.T) {
	orderBook := NewOrderBook()

	t.Run("take all limited sell order", func(t *testing.T) {
		orderSell1 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(100),
			Side:   Side_Sell,
			ID:     "sell-1",
		}
		trades := orderBook.PlaceLimitOrder(orderSell1)
		assert.Nil(t, trades)

		orderBuy1 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(100),
			Side:   Side_Buy,
			ID:     "buy-1",
		}
		trades = orderBook.PlaceLimitOrder(orderBuy1)
		assert.Equal(t, 1, len(trades))
	})

	t.Run("place limited buy order", func(t *testing.T) {
		orderSell1 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(100),
			Side:   Side_Sell,
			ID:     "sell-1",
		}
		trades := orderBook.PlaceLimitOrder(orderSell1)
		assert.Nil(t, trades)

		orderSell2 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(90),
			Side:   Side_Sell,
			ID:     "sell-2",
		}
		trades = orderBook.PlaceLimitOrder(orderSell2)
		assert.Nil(t, trades)

		orderSell3 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(100),
			Side:   Side_Sell,
			ID:     "sell-3",
		}
		trades = orderBook.PlaceLimitOrder(orderSell3)
		assert.Nil(t, trades)

		orderBuy1 := Order{
			Size: decimal.NewFromInt(5),
			Price:  decimal.NewFromInt(101),
			Side:   Side_Buy,
			ID:     "buy-1",
		}
		trades = orderBook.PlaceLimitOrder(orderBuy1)

		assert.Equal(t, 3, len(trades))
		assert.Equal(t, 0, len(orderBook.SellOrders))
		assert.Equal(t, 1, len(orderBook.BuyOrders))
	})
}

func TestPlaceMarketSellOrder(t *testing.T) {
	orderBook := NewOrderBook()

	t.Run("take all market buy order", func(t *testing.T) {
		orderBuy1 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(90),
			Side:   Side_Buy,
			ID:     "buy-1",
		}
		trades := orderBook.PlaceMarketOrder(orderBuy1)
		assert.Nil(t, trades)

		orderSell1 := Order{
			Size: decimal.NewFromInt(1),
			Side:   Side_Sell,
			ID:     "sell-1",
		}

		trades = orderBook.PlaceMarketOrder(orderSell1)
		assert.Equal(t, 1, len(trades))
	})

	t.Run("place market sell order", func(t *testing.T) {
		orderBuy1 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(90),
			Side:   Side_Buy,
			ID:     "buy-1",
		}
		trades := orderBook.PlaceMarketOrder(orderBuy1)
		assert.Nil(t, trades)

		orderBuy2 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(80),
			Side:   Side_Buy,
			ID:     "buy-2",
		}

		trades = orderBook.PlaceMarketOrder(orderBuy2)
		assert.Nil(t, trades)

		orderSell := Order{
			Size: decimal.NewFromInt(10),
			Side:   Side_Sell,
			ID:     "sell-1",
		}

		trades = orderBook.PlaceMarketOrder(orderSell)
		assert.Equal(t, 2, len(trades))
	})
}

func TestPlaceMarketBuyOrder(t *testing.T) {
	orderBook := NewOrderBook()

	t.Run("take all market sell order", func(t *testing.T) {
		orderSell1 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(90),
			Side:   Side_Sell,
			ID:     "sell-1",
		}
		trades := orderBook.PlaceMarketOrder(orderSell1)
		assert.Nil(t, trades)

		orderBuy := Order{
			Size: decimal.NewFromInt(1),
			Side:   Side_Buy,
			ID:     "buy-1",
		}

		trades = orderBook.PlaceMarketOrder(orderBuy)
		assert.Equal(t, 1, len(trades))
	})

	t.Run("place market buy order", func(t *testing.T) {
		orderSell1 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(90),
			Side:   Side_Sell,
			ID:     "sell-1",
		}
		trades := orderBook.PlaceMarketOrder(orderSell1)
		assert.Nil(t, trades)

		orderSell2 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(80),
			Side:   Side_Sell,
			ID:     "sell-2",
		}

		trades = orderBook.PlaceMarketOrder(orderSell2)
		assert.Nil(t, trades)

		orderSell3 := Order{
			Size: decimal.NewFromInt(1),
			Price:  decimal.NewFromInt(80),
			Side:   Side_Sell,
			ID:     "sell-3",
		}

		trades = orderBook.PlaceMarketOrder(orderSell3)
		assert.Nil(t, trades)

		orderBuy := Order{
			Size: decimal.NewFromInt(10),
			Side:   Side_Buy,
			ID:     "buy-1",
		}

		trades = orderBook.PlaceMarketOrder(orderBuy)
		assert.Equal(t, 3, len(trades))
	})
}
