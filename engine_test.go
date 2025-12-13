package match

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestMatchingEngine(t *testing.T) {
	t.Run("PlaceOrders", func(t *testing.T) {
		publishTrader := NewMemoryPublishTrader()
		engine := NewMatchingEngine(publishTrader)

		ctx := context.Background()

		// market1
		market1 := "BTC-USDT"
		order1 := &Order{
			ID:       "order1",
			MarketID: market1,
			Type:     Limit,
			Side:     Buy,
			Price:    decimal.NewFromInt(100),
			Size:     decimal.NewFromInt(2),
		}

		err := engine.AddOrder(ctx, order1)
		assert.NoError(t, err)

		time.Sleep(50 * time.Millisecond)
		orderbook := engine.OrderBook(market1)
		assert.Equal(t, int64(1), orderbook.bidQueue.orderCount())

		// market2
		market2 := "ETH-USDT"
		order2 := &Order{
			ID:       "order2",
			MarketID: market2,
			Type:     Limit,
			Side:     Sell,
			Price:    decimal.NewFromInt(110),
			Size:     decimal.NewFromInt(2),
		}

		err = engine.AddOrder(ctx, order2)
		assert.NoError(t, err)

		time.Sleep(50 * time.Millisecond)
		orderbook = engine.OrderBook(market2)
		assert.Equal(t, int64(1), orderbook.askQueue.orderCount())
	})

	t.Run("CancelOrder", func(t *testing.T) {
		publishTrader := NewMemoryPublishTrader()
		engine := NewMatchingEngine(publishTrader)

		ctx := context.Background()

		market1 := "BTC-USDT"

		order1 := &Order{
			ID:       "order1",
			MarketID: market1,
			Type:     Limit,
			Side:     Buy,
			Price:    decimal.NewFromInt(100),
			Size:     decimal.NewFromInt(2),
		}

		err := engine.AddOrder(ctx, order1)
		assert.NoError(t, err)

		order2 := &Order{
			ID:       "order2",
			MarketID: market1,
			Type:     Limit,
			Side:     Sell,
			Price:    decimal.NewFromInt(110),
			Size:     decimal.NewFromInt(2),
		}

		err = engine.AddOrder(ctx, order2)
		assert.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		err = engine.CancelOrder(ctx, market1, order1.ID)
		assert.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		// validate
		orderbook := engine.OrderBook(market1)
		assert.Equal(t, int64(0), orderbook.bidQueue.orderCount())
		assert.Equal(t, int64(1), orderbook.askQueue.orderCount())
	})
}

func TestMatchingEngineShutdown(t *testing.T) {
	t.Run("ShutdownMultipleMarkets", func(t *testing.T) {
		publishTrader := NewMemoryPublishTrader()
		engine := NewMatchingEngine(publishTrader)

		ctx := context.Background()

		// Create orders in multiple markets
		markets := []string{"BTC-USDT", "ETH-USDT", "SOL-USDT"}
		for i, market := range markets {
			order := &Order{
				ID:       "order-" + market,
				MarketID: market,
				Type:     Limit,
				Side:     Buy,
				Price:    decimal.NewFromInt(int64(100 + i*10)),
				Size:     decimal.NewFromInt(1),
			}
			err := engine.AddOrder(ctx, order)
			assert.NoError(t, err)
		}

		time.Sleep(50 * time.Millisecond)

		// Shutdown should complete successfully
		err := engine.Shutdown(ctx)
		assert.NoError(t, err)

		// After shutdown, adding orders should return ErrShutdown
		order := &Order{
			ID:       "after-shutdown",
			MarketID: "BTC-USDT",
			Type:     Limit,
			Side:     Buy,
			Price:    decimal.NewFromInt(100),
			Size:     decimal.NewFromInt(1),
		}
		err = engine.AddOrder(ctx, order)
		assert.Equal(t, ErrShutdown, err)
	})

	t.Run("RejectsNewMarkets", func(t *testing.T) {
		publishTrader := NewMemoryPublishTrader()
		engine := NewMatchingEngine(publishTrader)

		ctx := context.Background()

		// Create one market first
		order := &Order{
			ID:       "order1",
			MarketID: "BTC-USDT",
			Type:     Limit,
			Side:     Buy,
			Price:    decimal.NewFromInt(100),
			Size:     decimal.NewFromInt(1),
		}
		err := engine.AddOrder(ctx, order)
		assert.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		// Shutdown
		err = engine.Shutdown(ctx)
		assert.NoError(t, err)

		// Try to create a new market after shutdown - should return ErrShutdown
		newMarketOrder := &Order{
			ID:       "new-market-order",
			MarketID: "NEW-MARKET",
			Type:     Limit,
			Side:     Buy,
			Price:    decimal.NewFromInt(100),
			Size:     decimal.NewFromInt(1),
		}
		err = engine.AddOrder(ctx, newMarketOrder)
		assert.Equal(t, ErrShutdown, err)

		// OrderBook for new market should return nil
		book := engine.OrderBook("NEW-MARKET")
		assert.Nil(t, book)
	})

	t.Run("RespectsContextTimeout", func(t *testing.T) {
		publishTrader := NewMemoryPublishTrader()
		engine := NewMatchingEngine(publishTrader)

		ctx := context.Background()

		// Create an order to ensure at least one market exists
		order := &Order{
			ID:       "order1",
			MarketID: "BTC-USDT",
			Type:     Limit,
			Side:     Buy,
			Price:    decimal.NewFromInt(100),
			Size:     decimal.NewFromInt(1),
		}
		err := engine.AddOrder(ctx, order)
		assert.NoError(t, err)

		time.Sleep(50 * time.Millisecond)

		// Shutdown with a reasonable timeout should succeed
		timeoutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = engine.Shutdown(timeoutCtx)
		assert.NoError(t, err)
	})
}
