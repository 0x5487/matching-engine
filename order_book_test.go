package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlaceLimitedOrder(t *testing.T) {
	orderBook := NewOrderBook()

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

	orderSell := Order{
		Amount: 1,
		Price:  95,
		Side:   0,
		ID:     "sell-1",
	}

	trades = orderBook.PlaceLimitOrder(orderSell)
	assert.Equal(t, 1, len(trades))
}

func TestPlaceMarketOrder(t *testing.T) {
	orderBook := NewOrderBook()

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
}
