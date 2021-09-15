package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlaceLimitedSellOrder(t *testing.T) {
	orderBook := NewOrderBook()

	t.Run("take all limited buy order", func(t *testing.T) {
		orderBuy1 := Order{
			Amount: 1,
			Price:  100,
			Side:   1,
			ID:     "buy-1",
		}

		trades := orderBook.PlaceLimitOrder(orderBuy1)
		assert.Nil(t, trades)

		orderSell1 := Order{
			Amount: 1,
			Price:  100,
			Side:   0,
			ID:     "sell-1",
		}
		trades = orderBook.PlaceLimitOrder(orderSell1)
		assert.Equal(t, 1, len(trades))
	})

	t.Run("place limited sell order", func(t *testing.T) {
		orderBuy1 := Order{
			Amount: 1,
			Price:  100,
			Side:   1,
			ID:     "buy-1",
		}

		trades := orderBook.PlaceLimitOrder(orderBuy1)
		assert.Nil(t, trades)

		orderBuy2 := Order{
			Amount: 1,
			Price:  90,
			Side:   1,
			ID:     "buy-2",
		}

		trades = orderBook.PlaceLimitOrder(orderBuy2)
		assert.Nil(t, trades)

		orderBuy3 := Order{
			Amount: 1,
			Price:  100,
			Side:   1,
			ID:     "buy-3",
		}

		trades = orderBook.PlaceLimitOrder(orderBuy3)
		assert.Nil(t, trades)

		orderSell1 := Order{
			Amount: 5,
			Price:  90,
			Side:   0,
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
			Amount: 1,
			Price:  100,
			Side:   0,
			ID:     "sell-1",
		}
		trades := orderBook.PlaceLimitOrder(orderSell1)
		assert.Nil(t, trades)

		orderBuy1 := Order{
			Amount: 1,
			Price:  100,
			Side:   1,
			ID:     "buy-1",
		}
		trades = orderBook.PlaceLimitOrder(orderBuy1)
		assert.Equal(t, 1, len(trades))
	})

	t.Run("place limited buy order", func(t *testing.T) {
		orderSell1 := Order{
			Amount: 1,
			Price:  100,
			Side:   0,
			ID:     "sell-1",
		}
		trades := orderBook.PlaceLimitOrder(orderSell1)
		assert.Nil(t, trades)

		orderSell2 := Order{
			Amount: 1,
			Price:  90,
			Side:   0,
			ID:     "sell-2",
		}
		trades = orderBook.PlaceLimitOrder(orderSell2)
		assert.Nil(t, trades)

		orderSell3 := Order{
			Amount: 1,
			Price:  100,
			Side:   0,
			ID:     "sell-3",
		}
		trades = orderBook.PlaceLimitOrder(orderSell3)
		assert.Nil(t, trades)

		orderBuy1 := Order{
			Amount: 5,
			Price:  101,
			Side:   1,
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
			Amount: 1,
			Price:  90,
			Side:   1,
			ID:     "buy-1",
		}
		trades := orderBook.PlaceMarketOrder(orderBuy1)
		assert.Nil(t, trades)

		orderSell1 := Order{
			Amount: 1,
			Side:   0,
			ID:     "sell-1",
		}

		trades = orderBook.PlaceMarketOrder(orderSell1)
		assert.Equal(t, 1, len(trades))
	})

	t.Run("place market sell order", func(t *testing.T) {
		orderBuy1 := Order{
			Amount: 1,
			Price:  90,
			Side:   1,
			ID:     "buy-1",
		}
		trades := orderBook.PlaceMarketOrder(orderBuy1)
		assert.Nil(t, trades)

		orderBuy2 := Order{
			Amount: 1,
			Price:  80,
			Side:   1,
			ID:     "buy-2",
		}

		trades = orderBook.PlaceMarketOrder(orderBuy2)
		assert.Nil(t, trades)

		orderSell := Order{
			Amount: 10,
			Side:   0,
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
			Amount: 1,
			Price:  90,
			Side:   0,
			ID:     "sell-1",
		}
		trades := orderBook.PlaceMarketOrder(orderSell1)
		assert.Nil(t, trades)

		orderBuy := Order{
			Amount: 1,
			Side:   1,
			ID:     "buy-1",
		}

		trades = orderBook.PlaceMarketOrder(orderBuy)
		assert.Equal(t, 1, len(trades))
	})

	t.Run("place market buy order", func(t *testing.T) {
		orderSell1 := Order{
			Amount: 1,
			Price:  90,
			Side:   0,
			ID:     "sell-1",
		}
		trades := orderBook.PlaceMarketOrder(orderSell1)
		assert.Nil(t, trades)

		orderSell2 := Order{
			Amount: 1,
			Price:  80,
			Side:   0,
			ID:     "sell-2",
		}

		trades = orderBook.PlaceMarketOrder(orderSell2)
		assert.Nil(t, trades)

		orderSell3 := Order{
			Amount: 1,
			Price:  80,
			Side:   0,
			ID:     "sell-3",
		}

		trades = orderBook.PlaceMarketOrder(orderSell3)
		assert.Nil(t, trades)

		orderBuy := Order{
			Amount: 10,
			Side:   1,
			ID:     "buy-1",
		}

		trades = orderBook.PlaceMarketOrder(orderBuy)
		assert.Equal(t, 3, len(trades))
	})
}
