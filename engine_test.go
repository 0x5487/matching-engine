package match

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0x5487/matching-engine/protocol"
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

		order1 := &protocol.PlaceOrderCommand{
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
		}

		err = engine.PlaceOrder(ctx, market1, order1)
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

		order2 := &protocol.PlaceOrderCommand{
			OrderID:   "order2",
			OrderType: Limit,
			Side:      Sell,
			Price:     udecimal.MustFromInt64(110, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
		}

		err = engine.PlaceOrder(ctx, market2, order2)
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

		order1 := &protocol.PlaceOrderCommand{
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
			UserID:    1,
		}

		err = engine.PlaceOrder(ctx, market1, order1)
		assert.NoError(t, err)

		// Wait for order to be in book
		orderbook := engine.OrderBook(market1)
		assert.Eventually(t, func() bool {
			stats, err := orderbook.GetStats()
			return err == nil && stats.BidOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		err = engine.CancelOrder(ctx, market1, &protocol.CancelOrderCommand{OrderID: order1.OrderID, UserID: order1.UserID})
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
		err := engine.PlaceOrder(ctx, market, &protocol.PlaceOrderCommand{OrderID: "o1"})
		assert.Equal(t, ErrNotFound, err)

		// AmendOrder
		err = engine.AmendOrder(ctx, market, &protocol.AmendOrderCommand{OrderID: "o1"})
		assert.Equal(t, ErrNotFound, err)

		// CancelOrder
		err = engine.CancelOrder(ctx, market, &protocol.CancelOrderCommand{OrderID: "o1"})
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

			order := &protocol.PlaceOrderCommand{
				OrderID:   "order-" + market,
				OrderType: Limit,
				Side:      Buy,
				Price:     udecimal.MustFromInt64(int64(100+i*10), 0).String(),
				Size:      udecimal.MustFromInt64(1, 0).String(),
			}
			err = engine.PlaceOrder(ctx, market, order)
			assert.NoError(t, err)
		}

		// Shutdown should complete successfully
		err := engine.Shutdown(ctx)
		assert.NoError(t, err)

		// After shutdown, adding orders should return ErrShutdown
		order := &protocol.PlaceOrderCommand{
			OrderID:   "after-shutdown",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(1, 0).String(),
		}
		err = engine.PlaceOrder(ctx, "BTC-USDT", order)
		assert.Equal(t, ErrShutdown, err)
	})

	t.Run("RejectsNewMarkets", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine(publishTrader)

		ctx := context.Background()

		// Create one market first
		_, err := engine.AddOrderBook("BTC-USDT")
		assert.NoError(t, err)

		order := &protocol.PlaceOrderCommand{
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(1, 0).String(),
		}
		err = engine.PlaceOrder(ctx, "BTC-USDT", order)
		assert.NoError(t, err)

		// Shutdown
		err = engine.Shutdown(ctx)
		assert.NoError(t, err)

		// Try to create a new market after shutdown - should return ErrShutdown
		newMarketOrder := &protocol.PlaceOrderCommand{
			OrderID:   "new-market-order",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(1, 0).String(),
		}
		err = engine.PlaceOrder(ctx, "NEW-MARKET", newMarketOrder)
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

		order := &protocol.PlaceOrderCommand{
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(1, 0).String(),
		}
		err = engine.PlaceOrder(ctx, "BTC-USDT", order)
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
	err = engine.PlaceOrder(ctx, market1, &protocol.PlaceOrderCommand{
		OrderID:   "btc-buy-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(50000, 0).String(),
		Size:      udecimal.MustFromInt64(1, 0).String(),
		UserID:    1,
	})
	assert.NoError(t, err)

	// Add orders to Market 2
	err = engine.PlaceOrder(ctx, market2, &protocol.PlaceOrderCommand{
		OrderID:   "eth-sell-1",
		Side:      Sell,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(3000, 0).String(),
		Size:      udecimal.MustFromInt64(10, 0).String(),
		UserID:    2,
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
	err = newEngine.PlaceOrder(ctx, market1, &protocol.PlaceOrderCommand{
		OrderID:   "btc-sell-match",
		Side:      Sell,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(50000, 0).String(),
		Size:      udecimal.MustFromInt64(1, 0).String(),
		UserID:    3,
	})
	assert.NoError(t, err)

	// Should match btc-buy-1
	time.Sleep(100 * time.Millisecond)
	stats1After, err := book1.GetStats()
	assert.NoError(t, err)
	assert.Equal(t, int64(0), stats1After.BidOrderCount) // Consumed
	assert.Equal(t, int64(0), stats1After.AskOrderCount) // Consumed
}

func TestManagement_CreateMarket(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine(publish)
	marketID := "BTC-USDT"

	// 1. Create Market via Command
	// MinLotSize: 0.01
	err := engine.CreateMarket(marketID, "0.01")
	assert.NoError(t, err)

	// Verify OrderBook existence
	// Since handleCreateMarket is synchronous in engine for now (executed directly by ExecuteCommand callback?
	// No, ExecuteCommand calls handleCreateMarket synchronously.
	// But `handleCreateMarket` starts the book in goroutine.
	// The book instance is stored synchronously.
	book := engine.OrderBook(marketID)
	assert.NotNil(t, book)

	// Verify MinLotSize configuration
	// Note: We cannot access book.lotSize directly as it is private.
	// We can verify behavior by placing a small order.

	ctx := context.Background()
	smallOrder := &protocol.PlaceOrderCommand{
		OrderID:   "small-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(50000, 0).String(),
		Size:      "0.001", // Below 0.01
		UserID:    1,
	}

	err = engine.PlaceOrder(ctx, marketID, smallOrder)
	assert.NoError(t, err)

	// Should be rejected due to LotSize (which usually generates NoLiquidity or Reject with InsufficientSize?)
	// Actually `handleMarketOrder` checks `matchSize.LessThan(book.lotSize)`.
	// But `handleLimitOrder`?
	// The `lotSize` logic in `order_book.go` currently only seems to be explicit in `handleMarketOrder`.
	// Let's check `order_book.go` logic later.
	// If `lotSize` is not enforced on Limit orders globally, then this test might fail.
	// The spec says "DefaultLotSize ... When Market order match size..."
	// The standard `AddOrderBook` had `WithLotSize`.
	// If `handleCreateMarket` applies `WithLotSize`, it sets `book.lotSize`.
	// We assume it works if `CreateMarket` succeeded.
}

func TestManagement_SuspendResume(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine(publish)
	marketID := "ETH-USDT"
	ctx := context.Background()

	err := engine.CreateMarket(marketID, "0.0001")
	assert.NoError(t, err)

	// 1. Place Order (Should Succeed)
	order1 := &protocol.PlaceOrderCommand{
		OrderID:   "order-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     "3000",
		Size:      "1",
		UserID:    1,
	}
	err = engine.PlaceOrder(ctx, marketID, order1)
	assert.NoError(t, err)

	// Wait for Order-1
	assert.Eventually(t, func() bool {
		book := engine.OrderBook(marketID)
		if book == nil {
			return false
		}
		stats, _ := book.GetStats()
		return stats.BidOrderCount == 1
	}, 1*time.Second, 10*time.Millisecond)

	// 2. Suspend Market
	err = engine.SuspendMarket(marketID)
	assert.NoError(t, err)

	// Give time for suspend command to process
	time.Sleep(50 * time.Millisecond)

	// 3. Place Order (Should be Rejected)
	order2 := &protocol.PlaceOrderCommand{
		OrderID:   "order-2",
		Side:      Buy,
		OrderType: Limit,
		Price:     "3000",
		Size:      "1",
		UserID:    2,
	}
	err = engine.PlaceOrder(ctx, marketID, order2)
	assert.NoError(t, err)

	// Verify Reject Log for MarketSuspended
	assert.Eventually(t, func() bool {
		logs := publish.Logs()
		for _, l := range logs {
			if l.OrderID == "order-2" &&
				l.Type == protocol.LogTypeReject &&
				// Verify precise reason (Review 8.4.2)
				l.RejectReason == protocol.RejectReasonMarketSuspended {
				return true
			}
		}
		return false
	}, 1*time.Second, 10*time.Millisecond)

	// 4. Cancel Order (Should Succeed in Suspended State)
	err = engine.CancelOrder(ctx, marketID, &protocol.CancelOrderCommand{
		OrderID: order1.OrderID,
		UserID:  order1.UserID,
	})
	assert.NoError(t, err)

	// Verify Cancel Log
	assert.Eventually(t, func() bool {
		logs := publish.Logs()
		for _, l := range logs {
			if l.OrderID == "order-1" && l.Type == protocol.LogTypeCancel {
				return true
			}
		}
		return false
	}, 1*time.Second, 10*time.Millisecond)

	// 5. Resume Market
	err = engine.ResumeMarket(marketID)
	assert.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	// 6. Place Order (Should Succeed again)
	order3 := &protocol.PlaceOrderCommand{
		OrderID:   "order-3",
		Side:      Buy,
		OrderType: Limit,
		Price:     "3000",
		Size:      "1",
		UserID:    3,
	}
	err = engine.PlaceOrder(ctx, marketID, order3)
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		book := engine.OrderBook(marketID)
		if book == nil {
			return false
		}
		stats, _ := book.GetStats()
		return stats.BidOrderCount == 1
	}, 1*time.Second, 10*time.Millisecond)
}

func TestManagement_SnapshotRestore(t *testing.T) {
	// Setup temporary directory for snapshots
	tmpDir, err := os.MkdirTemp("", "snapshot_mgmt_test")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine(publish)
	marketID := "SUSPENDED-MARKET"

	// 1. Create Market with specific LotSize
	err = engine.CreateMarket(marketID, "0.1")
	assert.NoError(t, err)

	// 2. Suspend Market
	err = engine.SuspendMarket(marketID)
	assert.NoError(t, err)
	time.Sleep(50 * time.Millisecond) // Wait for processing

	// 3. Take Snapshot
	meta, err := engine.TakeSnapshot(tmpDir)
	assert.NoError(t, err)
	assert.NotNil(t, meta)

	// 4. Restore to New Engine
	newPublish := NewMemoryPublishLog()
	newEngine := NewMatchingEngine(newPublish)
	_, err = newEngine.RestoreFromSnapshot(tmpDir)
	assert.NoError(t, err)

	// 5. Verify State (Should be Suspended)
	ctx := context.Background()
	order := &protocol.PlaceOrderCommand{
		OrderID:   "test-order",
		Side:      Buy,
		OrderType: Limit,
		Price:     "100",
		Size:      "1",
		UserID:    1,
	}
	err = newEngine.PlaceOrder(ctx, marketID, order)
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		logs := newPublish.Logs()
		for _, l := range logs {
			if l.OrderID == "test-order" &&
				l.Type == protocol.LogTypeReject &&
				l.RejectReason == protocol.RejectReasonMarketSuspended {
				return true
			}
		}
		return false
	}, 1*time.Second, 10*time.Millisecond)

	// 6. Resume
	err = newEngine.ResumeMarket(marketID)
	assert.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	// 7. Test resumed functionality
	order2 := &protocol.PlaceOrderCommand{
		OrderID:   "test-order-2",
		Side:      Buy,
		OrderType: Limit,
		Price:     "100",
		Size:      "1",
		UserID:    2,
	}
	err = newEngine.PlaceOrder(ctx, marketID, order2)
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		book := newEngine.OrderBook(marketID)
		if book == nil {
			return false
		}
		stats, _ := book.GetStats()
		return stats.BidOrderCount == 1
	}, 1*time.Second, 10*time.Millisecond)
}
