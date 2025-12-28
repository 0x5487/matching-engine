package match

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
)

func TestMatchingEngine(t *testing.T) {
	t.Run("PlaceOrders", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine(publishTrader)

		ctx := context.Background()

		// market1
		market1 := "BTC-USDT"
		_, err := engine.AddOrderBook(market1)
		assert.NoError(t, err)

		order1 := &PlaceOrderCommand{
			MarketID: market1,
			ID:       "order1",
			Type:     Limit,
			Side:     Buy,
			Price:    udecimal.MustFromInt64(100, 0),
			Size:     udecimal.MustFromInt64(2, 0),
		}

		err = engine.AddOrder(ctx, order1)
		assert.NoError(t, err)

		orderbook := engine.OrderBook(market1)
		assert.Eventually(t, func() bool {
			stats, err := orderbook.GetStats()
			return err == nil && stats.BidOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		// market2
		market2 := "ETH-USDT"
		_, err = engine.AddOrderBook(market2)
		assert.NoError(t, err)

		order2 := &PlaceOrderCommand{
			MarketID: market2,
			ID:       "order2",
			Type:     Limit,
			Side:     Sell,
			Price:    udecimal.MustFromInt64(110, 0),
			Size:     udecimal.MustFromInt64(2, 0),
		}

		err = engine.AddOrder(ctx, order2)
		assert.NoError(t, err)

		orderbook = engine.OrderBook(market2)
		assert.Eventually(t, func() bool {
			stats, err := orderbook.GetStats()
			return err == nil && stats.AskOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)
	})

	t.Run("CancelOrder", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine(publishTrader)

		ctx := context.Background()

		market1 := "BTC-USDT"
		_, err := engine.AddOrderBook(market1)
		assert.NoError(t, err)

		order1 := &PlaceOrderCommand{
			MarketID: market1,
			ID:       "order1",
			Type:     Limit,
			Side:     Buy,
			Price:    udecimal.MustFromInt64(100, 0),
			Size:     udecimal.MustFromInt64(2, 0),
			UserID:   1,
		}

		err = engine.AddOrder(ctx, order1)
		assert.NoError(t, err)

		// Wait for order to be in book
		orderbook := engine.OrderBook(market1)
		assert.Eventually(t, func() bool {
			stats, err := orderbook.GetStats()
			return err == nil && stats.BidOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		err = engine.CancelOrder(ctx, market1, &CancelOrderCommand{OrderID: order1.ID, UserID: order1.UserID})
		assert.NoError(t, err)

		// validate
		assert.Eventually(t, func() bool {
			stats, err := orderbook.GetStats()
			return err == nil && stats.BidOrderCount == 0
		}, 1*time.Second, 10*time.Millisecond)
	})

	t.Run("MarketNotFound", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine(publishTrader)
		ctx := context.Background()

		market := "NON-EXISTENT"

		// AddOrder
		err := engine.AddOrder(ctx, &PlaceOrderCommand{MarketID: market, ID: "o1"})
		assert.Equal(t, ErrNotFound, err)

		// AmendOrder
		err = engine.AmendOrder(ctx, market, &AmendOrderCommand{OrderID: "o1"})
		assert.Equal(t, ErrNotFound, err)

		// CancelOrder
		err = engine.CancelOrder(ctx, market, &CancelOrderCommand{OrderID: "o1"})
		assert.Equal(t, ErrNotFound, err)

		// Get OrderBook
		book := engine.OrderBook(market)
		assert.Nil(t, book)
	})
}

func TestMatchingEngineShutdown(t *testing.T) {
	t.Run("ShutdownMultipleMarkets", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine(publishTrader)

		ctx := context.Background()

		// Create orders in multiple markets
		markets := []string{"BTC-USDT", "ETH-USDT", "SOL-USDT"}
		for i, market := range markets {
			_, err := engine.AddOrderBook(market)
			assert.NoError(t, err)

			order := &PlaceOrderCommand{
				MarketID: market,
				ID:       "order-" + market,
				Type:     Limit,
				Side:     Buy,
				Price:    udecimal.MustFromInt64(int64(100+i*10), 0),
				Size:     udecimal.MustFromInt64(1, 0),
			}
			err = engine.AddOrder(ctx, order)
			assert.NoError(t, err)
		}

		// Shutdown should complete successfully
		err := engine.Shutdown(ctx)
		assert.NoError(t, err)

		// After shutdown, adding orders should return ErrShutdown
		order := &PlaceOrderCommand{
			MarketID: "BTC-USDT",
			ID:       "after-shutdown",
			Type:     Limit,
			Side:     Buy,
			Price:    udecimal.MustFromInt64(100, 0),
			Size:     udecimal.MustFromInt64(1, 0),
		}
		err = engine.AddOrder(ctx, order)
		assert.Equal(t, ErrShutdown, err)
	})

	t.Run("RejectsNewMarkets", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine(publishTrader)

		ctx := context.Background()

		// Create one market first
		_, err := engine.AddOrderBook("BTC-USDT")
		assert.NoError(t, err)

		order := &PlaceOrderCommand{
			MarketID: "BTC-USDT",
			ID:       "order1",
			Type:     Limit,
			Side:     Buy,
			Price:    udecimal.MustFromInt64(100, 0),
			Size:     udecimal.MustFromInt64(1, 0),
		}
		err = engine.AddOrder(ctx, order)
		assert.NoError(t, err)

		// Shutdown
		err = engine.Shutdown(ctx)
		assert.NoError(t, err)

		// Try to create a new market after shutdown - should return ErrShutdown
		newMarketOrder := &PlaceOrderCommand{
			MarketID: "NEW-MARKET",
			ID:       "new-market-order",
			Type:     Limit,
			Side:     Buy,
			Price:    udecimal.MustFromInt64(100, 0),
			Size:     udecimal.MustFromInt64(1, 0),
		}
		err = engine.AddOrder(ctx, newMarketOrder)
		assert.Equal(t, ErrShutdown, err)

		// OrderBook for new market should return nil
		book := engine.OrderBook("NEW-MARKET")
		assert.Nil(t, book)
	})

	t.Run("RespectsContextTimeout", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine(publishTrader)

		ctx := context.Background()

		// Create an order to ensure at least one market exists
		_, err := engine.AddOrderBook("BTC-USDT")
		assert.NoError(t, err)

		order := &PlaceOrderCommand{
			MarketID: "BTC-USDT",
			ID:       "order1",
			Type:     Limit,
			Side:     Buy,
			Price:    udecimal.MustFromInt64(100, 0),
			Size:     udecimal.MustFromInt64(1, 0),
		}
		err = engine.AddOrder(ctx, order)
		assert.NoError(t, err)

		// Shutdown with a reasonable timeout should succeed
		timeoutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = engine.Shutdown(timeoutCtx)
		assert.NoError(t, err)
	})
}

func TestEngineSnapshotRestore(t *testing.T) {
	// Setup temporary directory for snapshots
	tmpDir, err := os.MkdirTemp("", "snapshot_test")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	publishTrader := NewMemoryPublishLog()
	engine := NewMatchingEngine(publishTrader)

	// 1. Setup State: Create 2 OrderBooks with Orders
	market1 := "BTC-USDT"
	market2 := "ETH-USDT"

	_, err = engine.AddOrderBook(market1)
	assert.NoError(t, err)
	_, err = engine.AddOrderBook(market2)
	assert.NoError(t, err)

	// Add orders to Market 1
	err = engine.AddOrder(ctx, &PlaceOrderCommand{
		MarketID: market1,
		ID:       "btc-buy-1",
		Side:     Buy,
		Type:     Limit,
		Price:    udecimal.MustFromInt64(50000, 0),
		Size:     udecimal.MustFromInt64(1, 0),
		UserID:   1,
	})
	assert.NoError(t, err)

	// Add orders to Market 2
	err = engine.AddOrder(ctx, &PlaceOrderCommand{
		MarketID: market2,
		ID:       "eth-sell-1",
		Side:     Sell,
		Type:     Limit,
		Price:    udecimal.MustFromInt64(3000, 0),
		Size:     udecimal.MustFromInt64(10, 0),
		UserID:   2,
	})
	assert.NoError(t, err)

	// Wait for processing
	time.Sleep(100 * time.Millisecond) // Give some time for async processing

	// 2. Take Snapshot
	meta, err := engine.TakeSnapshot(tmpDir)
	assert.NoError(t, err)
	assert.NotNil(t, meta)
	assert.NotZero(t, meta.Timestamp)

	// Verify Files Created
	assert.FileExists(t, filepath.Join(tmpDir, "snapshot.bin"))
	assert.FileExists(t, filepath.Join(tmpDir, "metadata.json"))

	// Verify Metadata Content
	metaContent, err := os.ReadFile(filepath.Join(tmpDir, "metadata.json"))
	assert.NoError(t, err)
	var readMeta SnapshotMetadata
	err = json.Unmarshal(metaContent, &readMeta)
	assert.NoError(t, err)
	assert.Equal(t, meta.Timestamp, readMeta.Timestamp)

	// 3. Restore to a NEW Engine
	newPublishTrader := NewMemoryPublishLog()
	newEngine := NewMatchingEngine(newPublishTrader)

	restoredMeta, err := newEngine.RestoreFromSnapshot(tmpDir)
	assert.NoError(t, err)
	assert.NotNil(t, restoredMeta)
	assert.Equal(t, meta.GlobalLastCmdSeqID, restoredMeta.GlobalLastCmdSeqID)

	// 4. Verify Restored State
	// Check Market 1
	book1 := newEngine.OrderBook(market1)
	assert.NotNil(t, book1)
	stats1, err := book1.GetStats()
	assert.NoError(t, err)
	assert.Equal(t, int64(1), stats1.BidOrderCount)
	assert.Equal(t, int64(0), stats1.AskOrderCount)

	// Check Market 2
	book2 := newEngine.OrderBook(market2)
	assert.NotNil(t, book2)
	stats2, err := book2.GetStats()
	assert.NoError(t, err)
	assert.Equal(t, int64(0), stats2.BidOrderCount)
	assert.Equal(t, int64(1), stats2.AskOrderCount)

	// 5. Verify Continuity (Add new orders to restored engine)
	err = newEngine.AddOrder(ctx, &PlaceOrderCommand{
		MarketID: market1,
		ID:       "btc-sell-match",
		Side:     Sell,
		Type:     Limit,
		Price:    udecimal.MustFromInt64(50000, 0),
		Size:     udecimal.MustFromInt64(1, 0),
		UserID:   3,
	})
	assert.NoError(t, err)

	// Should match btc-buy-1
	time.Sleep(100 * time.Millisecond)
	stats1After, err := book1.GetStats()
	assert.NoError(t, err)
	assert.Equal(t, int64(0), stats1After.BidOrderCount) // Consumed
	assert.Equal(t, int64(0), stats1After.AskOrderCount) // Consumed
}
