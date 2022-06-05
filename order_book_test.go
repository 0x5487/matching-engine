package engine

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestPlaceLimitedSellOrder(t *testing.T) {
	t.Run("take all limited buy orders", func(t *testing.T) {
		orderBook := NewOrderBook()

		orderBuy1 := Order{
			ID:          "buy-1",
			Side:        Side_Buy,
			Price:       decimal.NewFromInt(100),
			Size:        decimal.NewFromInt(1),
			TimeInForce: TimeInForce_GTC,
			PostOnly:    true,
		}
		trades, err := orderBook.PlaceLimitOrder(&orderBuy1)
		assert.Nil(t, err)
		assert.Len(t, trades, 0)

		orderSell1 := Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(100),
			Side:        Side_Sell,
			ID:          "sell-1",
			TimeInForce: TimeInForce_IOC,
		}
		trades, err = orderBook.PlaceLimitOrder(&orderSell1)
		assert.Nil(t, err)
		assert.Len(t, trades, 1)
	})

	t.Run("place limited sell order", func(t *testing.T) {
		orderBook := NewOrderBook()

		orderBuy1 := Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(80),
			Side:        Side_Buy,
			ID:          "buy-1",
			TimeInForce: TimeInForce_GTC,
			PostOnly:    true,
		}

		trades, err := orderBook.PlaceLimitOrder(&orderBuy1)
		assert.Nil(t, err)
		assert.Equal(t, 0, len(trades))

		orderBuy2 := Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(90),
			Side:        Side_Buy,
			ID:          "buy-2",
			TimeInForce: TimeInForce_GTC,
		}

		trades, err = orderBook.PlaceLimitOrder(&orderBuy2)
		assert.Nil(t, err)
		assert.Equal(t, 0, len(trades))

		orderBuy3 := Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(100),
			Side:        Side_Buy,
			ID:          "buy-3",
			TimeInForce: TimeInForce_GTC,
		}

		trades, err = orderBook.PlaceLimitOrder(&orderBuy3)
		assert.Nil(t, err)
		assert.Equal(t, 0, len(trades))

		orderSell1 := Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(150),
			Side:        Side_Sell,
			ID:          "sell-1",
			PostOnly:    true,
			TimeInForce: TimeInForce_GTC,
		}
		trades, err = orderBook.PlaceLimitOrder(&orderSell1)
		assert.Nil(t, err)
		assert.Equal(t, 0, len(trades))

		orderSell2 := Order{
			ID:          "sell-2",
			Side:        Side_Sell,
			Size:        decimal.NewFromInt(5),
			Price:       decimal.NewFromInt(90),
			TimeInForce: TimeInForce_IOC,
		}
		trades, err = orderBook.PlaceLimitOrder(&orderSell2)
		assert.ErrorIs(t, err, ErrCanceled)
		assert.Equal(t, 2, len(trades))
	})
}

func TestPlaceLimitedBuyOrder(t *testing.T) {
	t.Run("take all limited sell order", func(t *testing.T) {
		orderBook := NewOrderBook()

		orderSell1 := Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(100),
			Side:        Side_Sell,
			ID:          "sell-1",
			TimeInForce: TimeInForce_GTC,
			PostOnly:    false,
		}
		trades, err := orderBook.PlaceLimitOrder(&orderSell1)
		assert.Nil(t, err)
		assert.Equal(t, 0, len(trades))

		orderBuy1 := Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(100),
			Side:        Side_Buy,
			ID:          "buy-1",
			TimeInForce: TimeInForce_GTC,
			PostOnly:    true,
		}
		trades, err = orderBook.PlaceLimitOrder(&orderBuy1)
		assert.Error(t, err, ErrCanceled)
		assert.Len(t, trades, 0)

		orderBuy2 := Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(100),
			Side:        Side_Buy,
			ID:          "buy-2",
			TimeInForce: TimeInForce_GTC,
			PostOnly:    false,
		}
		trades, err = orderBook.PlaceLimitOrder(&orderBuy2)
		assert.Nil(t, err)
		assert.Len(t, trades, 1)
	})

	t.Run("place limited buy order", func(t *testing.T) {
		orderBook := NewOrderBook()

		orderSell1 := Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(80),
			Side:        Side_Sell,
			ID:          "sell-1",
			TimeInForce: TimeInForce_GTC,
		}
		trades, err := orderBook.PlaceLimitOrder(&orderSell1)
		assert.Nil(t, err)
		assert.Equal(t, 0, len(trades))

		orderSell2 := Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(90),
			Side:        Side_Sell,
			ID:          "sell-2",
			TimeInForce: TimeInForce_GTC,
		}
		trades, err = orderBook.PlaceLimitOrder(&orderSell2)
		assert.Nil(t, err)
		assert.Equal(t, 0, len(trades))

		orderSell3 := Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(100),
			Side:        Side_Sell,
			ID:          "sell-3",
			TimeInForce: TimeInForce_GTC,
		}
		trades, err = orderBook.PlaceLimitOrder(&orderSell3)
		assert.Nil(t, err)
		assert.Equal(t, 0, len(trades))

		orderBuy1 := Order{
			Size:        decimal.NewFromInt(5),
			Price:       decimal.NewFromInt(30),
			Side:        Side_Buy,
			ID:          "buy-1",
			TimeInForce: TimeInForce_GTC,
		}
		trades, err = orderBook.PlaceLimitOrder(&orderBuy1)
		assert.Nil(t, err)
		assert.Equal(t, 0, len(trades))

		orderBuy2 := Order{
			Size:        decimal.NewFromInt(5),
			Price:       decimal.NewFromInt(95),
			Side:        Side_Buy,
			ID:          "buy-2",
			TimeInForce: TimeInForce_IOC,
		}
		trades, err = orderBook.PlaceLimitOrder(&orderBuy2)
		assert.ErrorIs(t, err, ErrCanceled)
		assert.Equal(t, 2, len(trades))
	})
}

func TestPlaceMarketSellOrder(t *testing.T) {
	orderBook := NewOrderBook()

	t.Run("take all market buy order", func(t *testing.T) {
		orderBuy1 := &Order{
			ID:          "buy-1",
			Side:        Side_Buy,
			Price:       decimal.NewFromInt(90),
			Size:        decimal.NewFromInt(1),
			TimeInForce: TimeInForce_GTC,
		}
		trades, err := orderBook.PlaceLimitOrder(orderBuy1)
		assert.Nil(t, err)
		assert.Len(t, trades, 0)

		orderSell1 := &Order{
			ID:   "sell-1",
			Side: Side_Sell,
			Size: decimal.NewFromInt(1),
		}

		trades, err = orderBook.PlaceMarketOrder(orderSell1)
		assert.Nil(t, err)
		assert.Equal(t, 1, len(trades))
	})

	t.Run("place market sell order", func(t *testing.T) {
		orderBuy1 := &Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(90),
			Side:        Side_Buy,
			ID:          "buy-1",
			TimeInForce: TimeInForce_GTC,
		}
		trades, err := orderBook.PlaceLimitOrder(orderBuy1)
		assert.Nil(t, err)
		assert.Len(t, trades, 0)

		orderBuy2 := &Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(80),
			Side:        Side_Buy,
			ID:          "buy-2",
			TimeInForce: TimeInForce_GTC,
		}

		trades, err = orderBook.PlaceLimitOrder(orderBuy2)
		assert.Nil(t, err)
		assert.Len(t, trades, 0)

		orderSell := &Order{
			Size: decimal.NewFromInt(10),
			Side: Side_Sell,
			ID:   "sell-1",
		}

		trades, err = orderBook.PlaceMarketOrder(orderSell)
		assert.ErrorIs(t, err, ErrCanceled)
		assert.Equal(t, 2, len(trades))
	})
}

func TestPlaceMarketBuyOrder(t *testing.T) {
	orderBook := NewOrderBook()

	t.Run("take all market sell orders", func(t *testing.T) {
		orderSell1 := &Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(90),
			Side:        Side_Sell,
			ID:          "sell-1",
			TimeInForce: TimeInForce_GTC,
		}
		trades, err := orderBook.PlaceLimitOrder(orderSell1)
		assert.Nil(t, err)
		assert.Len(t, trades, 0)

		orderBuy := &Order{
			Size: decimal.NewFromInt(1),
			Side: Side_Buy,
			ID:   "buy-1",
		}

		trades, err = orderBook.PlaceMarketOrder(orderBuy)
		assert.Nil(t, err)
		assert.Equal(t, 1, len(trades))
	})

	t.Run("place market buy order", func(t *testing.T) {
		orderSell1 := &Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(90),
			Side:        Side_Sell,
			ID:          "sell-1",
			TimeInForce: TimeInForce_GTC,
		}
		trades, err := orderBook.PlaceLimitOrder(orderSell1)
		assert.Nil(t, err)
		assert.Len(t, trades, 0)

		orderSell2 := &Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(80),
			Side:        Side_Sell,
			ID:          "sell-2",
			TimeInForce: TimeInForce_GTC,
		}

		trades, err = orderBook.PlaceLimitOrder(orderSell2)
		assert.Nil(t, err)
		assert.Len(t, trades, 0)

		orderSell3 := &Order{
			Size:        decimal.NewFromInt(1),
			Price:       decimal.NewFromInt(80),
			Side:        Side_Sell,
			ID:          "sell-3",
			TimeInForce: TimeInForce_GTC,
		}

		trades, err = orderBook.PlaceLimitOrder(orderSell3)
		assert.Nil(t, err)
		assert.Len(t, trades, 0)

		orderBuy := &Order{
			Size: decimal.NewFromInt(10),
			Side: Side_Buy,
			ID:   "buy-1",
		}

		trades, err = orderBook.PlaceMarketOrder(orderBuy)
		assert.ErrorIs(t, err, ErrCanceled)
		assert.Equal(t, 3, len(trades))
	})
}

func TestOrderBookUpdateEvents(t *testing.T) {
	updateEventChan := make(chan *OrderBookUpdateEvent, 1000)

	orderBook := NewOrderBook()
	orderBook.RegisterUpdateEventChan(updateEventChan)

	orderBuy1 := Order{
		Price:       decimal.NewFromInt(100),
		Size:        decimal.NewFromInt(1),
		Side:        Side_Buy,
		ID:          "buy-1",
		TimeInForce: TimeInForce_GTC,
	}
	orderBook.PlaceLimitOrder(&orderBuy1)

	orderSell1 := Order{
		Price:       decimal.NewFromInt(101),
		Size:        decimal.NewFromInt(2),
		Side:        Side_Sell,
		ID:          "sell-1",
		TimeInForce: TimeInForce_GTC,
	}
	orderBook.PlaceLimitOrder(&orderSell1)

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
