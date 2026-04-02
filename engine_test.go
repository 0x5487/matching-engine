package match

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

const (
	marketBTC = "BTC-USDT"
	marketETH = "ETH-USDT"

	testEngineID = "corr-engine"
)

func TestMatchingEngine(t *testing.T) {
	t.Run("PlaceOrders", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine", publishTrader)

		ctx := context.Background()

		// market1
		market1 := marketBTC
		err := engine.CreateMarket("admin", market1, "")
		require.NoError(t, err)

		// market2
		market2 := marketETH
		err = engine.CreateMarket("admin", market2, "")
		require.NoError(t, err)

		// Start engine event loop
		go engine.Run()

		order1 := &protocol.PlaceOrderCommand{
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
		}

		err = engine.PlaceOrder(ctx, market1, order1)
		require.NoError(t, err)

		assert.Eventually(t, func() bool {
			var stats *protocol.GetStatsResponse
			stats, err = engine.GetStats(market1)
			return err == nil && stats.BidOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		order2 := &protocol.PlaceOrderCommand{
			OrderID:   "order2",
			OrderType: Limit,
			Side:      Sell,
			Price:     udecimal.MustFromInt64(110, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
		}

		err = engine.PlaceOrder(ctx, market2, order2)
		require.NoError(t, err)

		assert.Eventually(t, func() bool {
			stats, err := engine.GetStats(market2)
			return err == nil && stats.AskOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		_ = engine.Shutdown(ctx)
	})

	t.Run("CancelOrder", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine", publishTrader)

		ctx := context.Background()

		market1 := marketBTC
		err := engine.CreateMarket("admin", market1, "")
		require.NoError(t, err)

		// Start engine event loop
		go engine.Run()

		order1 := &protocol.PlaceOrderCommand{
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
			UserID:    uint64(1),
		}

		err = engine.PlaceOrder(ctx, market1, order1)
		require.NoError(t, err)

		// Wait for order to be in book
		assert.Eventually(t, func() bool {
			var stats *protocol.GetStatsResponse
			stats, err = engine.GetStats(market1)
			return err == nil && stats.BidOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		err = engine.CancelOrder(
			ctx,
			market1,
			&protocol.CancelOrderCommand{OrderID: order1.OrderID, UserID: order1.UserID},
		)
		require.NoError(t, err)

		// validate
		assert.Eventually(t, func() bool {
			stats, err := engine.GetStats(market1)
			return err == nil && stats.BidOrderCount == 0
		}, 1*time.Second, 10*time.Millisecond)

		_ = engine.Shutdown(ctx)
	})

	t.Run("MarketNotFound", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine", publishTrader)

		// Start engine event loop
		go engine.Run()

		market := "NON-EXISTENT"

		// PlaceOrder should still enqueue (market check happens on consumer side)
		// The command goes to the RingBuffer but gets dropped silently by processCommand
		// since the market doesn't exist.
		ctx := context.Background()
		err := engine.PlaceOrder(ctx, market, &protocol.PlaceOrderCommand{OrderID: "o1"})
		require.NoError(t, err) // Enqueue succeeds

		// Get OrderBook
		book := engine.orderbooks[market]
		assert.Nil(t, book)

		_ = engine.Shutdown(ctx)
	})

	t.Run("MarketNotFoundReturnsRejectLog", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine", publishTrader)
		ctx := context.Background()

		go engine.Run()

		err := engine.PlaceOrder(ctx, "NON-EXISTENT", &protocol.PlaceOrderCommand{
			OrderID:   "missing-market-order",
			OrderType: Limit,
			Side:      Buy,
			Price:     "100",
			Size:      "1",
			UserID:    7,
			Timestamp: 123456789,
		})
		require.NoError(t, err)

		assert.Eventually(t, func() bool {
			for _, log := range publishTrader.Logs() {
				if log.OrderID == "missing-market-order" {
					return log.Type == protocol.LogTypeReject &&
						log.RejectReason == protocol.RejectReasonMarketNotFound &&
						log.Timestamp == 123456789
				}
			}
			return false
		}, time.Second, 10*time.Millisecond)

		_ = engine.Shutdown(ctx)
	})

	t.Run("QueryMissingMarketReturnsNotFound", func(t *testing.T) {
		engine := NewMatchingEngine("test-engine", NewMemoryPublishLog())
		ctx := context.Background()

		go engine.Run()

		start := time.Now()
		stats, err := engine.GetStats("NON-EXISTENT")
		assert.Nil(t, stats)
		require.ErrorIs(t, err, ErrNotFound)
		assert.Less(t, time.Since(start), 200*time.Millisecond)

		depth, err := engine.Depth("NON-EXISTENT", 10)
		assert.Nil(t, depth)
		require.ErrorIs(t, err, ErrNotFound)

		_ = engine.Shutdown(ctx)
	})
}

func TestCommandAndEngineIDPropagation(t *testing.T) {
	ctx := context.Background()
	publishTrader := NewMemoryPublishLog()
	engine := NewMatchingEngine(testEngineID, publishTrader)
	marketID := marketBTC

	err := engine.CreateMarket("admin", marketID, "")
	require.NoError(t, err)

	go engine.Run()

	// Single submit: fallback CommandID should be OrderID.
	err = engine.PlaceOrder(ctx, marketID, &protocol.PlaceOrderCommand{
		OrderID:   "single-oid",
		OrderType: Limit,
		Side:      Buy,
		Price:     "100",
		Size:      "1",
		UserID:    1,
	})
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		for _, log := range publishTrader.Logs() {
			if log.OrderID == "single-oid" && log.Type == protocol.LogTypeOpen {
				return log.CommandID == "single-oid" && log.EngineID == testEngineID
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)

	// Batch submit: each command should keep per-order fallback CommandID.
	err = engine.PlaceOrderBatch(ctx, marketID, []*protocol.PlaceOrderCommand{
		{
			OrderID:   "batch-oid-1",
			OrderType: Limit,
			Side:      Buy,
			Price:     "99",
			Size:      "1",
			UserID:    2,
		},
		{
			OrderID:   "batch-oid-2",
			OrderType: Limit,
			Side:      Sell,
			Price:     "101",
			Size:      "1",
			UserID:    3,
		},
	})
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		found1 := false
		found2 := false
		for _, log := range publishTrader.Logs() {
			if log.OrderID == "batch-oid-1" && log.Type == protocol.LogTypeOpen {
				found1 = log.CommandID == "batch-oid-1" && log.EngineID == testEngineID
			}
			if log.OrderID == "batch-oid-2" && log.Type == protocol.LogTypeOpen {
				found2 = log.CommandID == "batch-oid-2" && log.EngineID == testEngineID
			}
		}
		return found1 && found2
	}, time.Second, 10*time.Millisecond)

	_ = engine.Shutdown(ctx)
}

func TestMatchingEngineShutdown(t *testing.T) {
	t.Run("ShutdownMultipleMarkets", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine", publishTrader)

		ctx := context.Background()

		// Create orders in multiple markets
		markets := []string{"BTC-USDT", "ETH-USDT", "SOL-USDT"}
		for _, market := range markets {
			err := engine.CreateMarket("admin", market, "")
			require.NoError(t, err)
		}

		// Start engine event loop
		go engine.Run()

		for i, market := range markets {
			order := &protocol.PlaceOrderCommand{
				OrderID:   "order-" + market,
				OrderType: Limit,
				Side:      Buy,
				Price:     udecimal.MustFromInt64(int64(100+i*10), 0).String(),
				Size:      udecimal.MustFromInt64(1, 0).String(),
			}
			err := engine.PlaceOrder(ctx, market, order)
			require.NoError(t, err)
		}

		// Shutdown should complete successfully
		err := engine.Shutdown(ctx)
		require.NoError(t, err)

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
		engine := NewMatchingEngine("test-engine", publishTrader)

		ctx := context.Background()

		// Create one market first
		err := engine.CreateMarket("admin", "BTC-USDT", "")
		require.NoError(t, err)

		// Start engine event loop
		go engine.Run()

		order := &protocol.PlaceOrderCommand{
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(1, 0).String(),
		}
		err = engine.PlaceOrder(ctx, "BTC-USDT", order)
		require.NoError(t, err)

		// Shutdown
		err = engine.Shutdown(ctx)
		require.NoError(t, err)

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
		book := engine.orderbooks["NEW-MARKET"]
		assert.Nil(t, book)
	})

	t.Run("RespectsContextTimeout", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine", publishTrader)

		ctx := context.Background()

		// Create an order to ensure at least one market exists
		err := engine.CreateMarket("admin", "BTC-USDT", "")
		require.NoError(t, err)

		// Start engine event loop
		go engine.Run()

		order := &protocol.PlaceOrderCommand{
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(1, 0).String(),
		}
		err = engine.PlaceOrder(ctx, "BTC-USDT", order)
		require.NoError(t, err)

		// Shutdown with a reasonable timeout should succeed
		timeoutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = engine.Shutdown(timeoutCtx)
		require.NoError(t, err)
	})
}

func TestEngineSnapshotRestore(t *testing.T) {
	// Setup temporary directory for snapshots
	tmpDir := t.TempDir()

	ctx := context.Background()
	publishTrader := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine", publishTrader)

	// 1. Setup State: Create 2 OrderBooks with Orders
	market1 := marketBTC
	market2 := "ETH-USDT"

	err := engine.CreateMarket("admin", market1, "")
	require.NoError(t, err)
	err = engine.CreateMarket("admin", market2, "")
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()

	// Add orders to Market 1
	err = engine.PlaceOrder(ctx, market1, &protocol.PlaceOrderCommand{
		OrderID:   "btc-buy-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(50000, 0).String(),
		Size:      udecimal.MustFromInt64(1, 0).String(),
		UserID:    uint64(1),
	})
	require.NoError(t, err)

	// Add orders to Market 2
	err = engine.PlaceOrder(ctx, market2, &protocol.PlaceOrderCommand{
		OrderID:   "eth-sell-1",
		Side:      Sell,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(3000, 0).String(),
		Size:      udecimal.MustFromInt64(10, 0).String(),
		UserID:    uint64(2),
	})
	require.NoError(t, err)

	// Wait for processing
	assert.Eventually(t, func() bool {
		stats1, err1 := engine.GetStats(market1)
		stats2, err2 := engine.GetStats(market2)
		return err1 == nil && err2 == nil &&
			stats1.BidOrderCount == 1 && stats2.AskOrderCount == 1
	}, 1*time.Second, 10*time.Millisecond)

	// 2. Take Snapshot
	meta, err := engine.TakeSnapshot(tmpDir)
	require.NoError(t, err)
	assert.NotNil(t, meta)
	assert.NotZero(t, meta.Timestamp)

	// Verify Files Created
	assert.FileExists(t, filepath.Join(tmpDir, "snapshot.bin"))
	assert.FileExists(t, filepath.Join(tmpDir, "metadata.json"))

	// Verify Metadata Content
	metaContent, err := os.ReadFile(filepath.Join(tmpDir, "metadata.json")) //nolint:gosec // G304: test code
	require.NoError(t, err)
	var readMeta SnapshotMetadata
	err = json.Unmarshal(metaContent, &readMeta)
	require.NoError(t, err)
	assert.Equal(t, meta.Timestamp, readMeta.Timestamp)

	_ = engine.Shutdown(ctx)

	// 3. Restore to a NEW Engine
	newPublishTrader := NewMemoryPublishLog()
	newEngine := NewMatchingEngine("test-engine-restored", newPublishTrader)

	restoredMeta, err := newEngine.RestoreFromSnapshot(tmpDir)
	require.NoError(t, err)
	assert.NotNil(t, restoredMeta)
	assert.Equal(t, meta.GlobalLastCmdSeqID, restoredMeta.GlobalLastCmdSeqID)

	// Start new engine event loop
	go newEngine.Run()

	// 4. Verify Restored State
	assert.Eventually(t, func() bool {
		stats1, err1 := newEngine.GetStats(market1)
		stats2, err2 := newEngine.GetStats(market2)
		return err1 == nil && err2 == nil &&
			stats1.BidOrderCount == 1 && stats1.AskOrderCount == 0 &&
			stats2.BidOrderCount == 0 && stats2.AskOrderCount == 1
	}, 1*time.Second, 10*time.Millisecond)

	// 5. Verify Continuity (Add new orders to restored engine)
	err = newEngine.PlaceOrder(ctx, market1, &protocol.PlaceOrderCommand{
		OrderID:   "btc-sell-match",
		Side:      Sell,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(50000, 0).String(),
		Size:      udecimal.MustFromInt64(1, 0).String(),
		UserID:    uint64(3),
	})
	require.NoError(t, err)

	// Should match btc-buy-1
	assert.Eventually(t, func() bool {
		stats1, err := newEngine.GetStats(market1)
		return err == nil && stats1.BidOrderCount == 0 && stats1.AskOrderCount == 0
	}, 1*time.Second, 10*time.Millisecond)

	_ = newEngine.Shutdown(ctx)
}

func TestEngineRestoreFromSnapshotRejectsInvalidBounds(t *testing.T) {
	tmpDir := t.TempDir()

	footer := SnapshotFileFooter{
		Markets: []MarketSegment{
			{
				MarketID: "BTC-USDT",
				Offset:   0,
				Length:   1 << 20,
				Checksum: 0,
			},
		},
	}
	footerBytes, err := json.Marshal(footer)
	require.NoError(t, err)

	snapshotBytes := make([]byte, 0, len(footerBytes)+footerLenSize)
	snapshotBytes = append(snapshotBytes, footerBytes...)
	footerLenBytes := make([]byte, footerLenSize)
	require.LessOrEqual(t, len(footerBytes), int(footerSizeLimit))
	//nolint:gosec // Length checked above in test setup.
	binary.BigEndian.PutUint32(footerLenBytes, uint32(len(footerBytes)))
	snapshotBytes = append(snapshotBytes, footerLenBytes...)

	meta := SnapshotMetadata{
		SchemaVersion:    SnapshotSchemaVersion,
		Timestamp:        1,
		EngineVersion:    EngineVersion,
		SnapshotChecksum: crc32.ChecksumIEEE(snapshotBytes),
	}
	metaBytes, err := json.Marshal(meta)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "snapshot.bin"), snapshotBytes, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "metadata.json"), metaBytes, 0o600))

	engine := NewMatchingEngine("test-engine", NewMemoryPublishLog())

	require.NotPanics(t, func() {
		_, restoreErr := engine.RestoreFromSnapshot(tmpDir)
		require.Error(t, restoreErr)
	})
}

func TestManagement_CreateMarket(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine", publish)
	marketID := "BTC-USDT"

	// Start engine event loop
	go engine.Run()

	// 1. Create Market via Command
	// MinLotSize: 0.01
	err := engine.CreateMarket("admin", marketID, "0.01")
	require.NoError(t, err)

	// Verify OrderBook existence (CreateMarket is asynchronous)
	assert.Eventually(t, func() bool {
		_, err = engine.GetStats(marketID)
		return err == nil
	}, 1*time.Second, 10*time.Millisecond)

	// Verify MinLotSize configuration by placing a small order
	ctx := context.Background()
	smallOrder := &protocol.PlaceOrderCommand{
		OrderID:   "small-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(50000, 0).String(),
		Size:      "0.001", // Below 0.01
		UserID:    uint64(1),
	}

	err = engine.PlaceOrder(ctx, marketID, smallOrder)
	require.NoError(t, err)

	_ = engine.Shutdown(ctx)
}

func TestManagement_CreateMarketRejectsInvalidConfig(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine", publish)
	ctx := context.Background()

	go engine.Run()

	err := engine.CreateMarket("admin", "BAD-MARKET", "not-a-decimal")
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		for _, log := range publish.Logs() {
			if log.CommandID != "" && log.RejectReason == protocol.RejectReasonInvalidPayload {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)

	stats, err := engine.GetStats("BAD-MARKET")
	assert.Nil(t, stats)
	require.ErrorIs(t, err, ErrNotFound)

	_ = engine.Shutdown(ctx)
}

func TestManagement_CreateMarketRejectsDuplicateMarket(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine", publish)
	ctx := context.Background()

	go engine.Run()

	err := engine.CreateMarket("admin", "DUP-MARKET", "0.1")
	require.NoError(t, err)
	assert.Eventually(t, func() bool {
		_, getErr := engine.GetStats("DUP-MARKET")
		return getErr == nil
	}, time.Second, 10*time.Millisecond)

	err = engine.CreateMarket("admin", "DUP-MARKET", "0.1")
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		for _, log := range publish.Logs() {
			if log.RejectReason == protocol.RejectReasonMarketAlreadyExists {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)

	_ = engine.Shutdown(ctx)
}

func TestManagement_SuspendResume(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine", publish)
	marketID := "ETH-USDT"
	ctx := context.Background()

	err := engine.CreateMarket("admin", marketID, "0.0001")
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()

	// 1. Place Order (Should Succeed)
	order1 := &protocol.PlaceOrderCommand{
		OrderID:   "order-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     "3000",
		Size:      "1",
		UserID:    uint64(1),
	}
	err = engine.PlaceOrder(ctx, marketID, order1)
	require.NoError(t, err)

	// Wait for Order-1
	assert.Eventually(t, func() bool {
		var stats *protocol.GetStatsResponse
		stats, err = engine.GetStats(marketID)
		return err == nil && stats.BidOrderCount == 1
	}, 1*time.Second, 10*time.Millisecond)

	// 2. Suspend Market
	err = engine.SuspendMarket("admin", marketID)
	require.NoError(t, err)

	// Give time for suspend command to process
	time.Sleep(50 * time.Millisecond)

	// 3. Place Order (Should be Rejected)
	order2 := &protocol.PlaceOrderCommand{
		OrderID:   "order-2",
		Side:      Buy,
		OrderType: Limit,
		Price:     "3000",
		Size:      "1",
		UserID:    uint64(2),
	}
	err = engine.PlaceOrder(ctx, marketID, order2)
	require.NoError(t, err)

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
	require.NoError(t, err)

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
	err = engine.ResumeMarket("admin", marketID)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	// 6. Place Order (Should Succeed again)
	order3 := &protocol.PlaceOrderCommand{
		OrderID:   "order-3",
		Side:      Buy,
		OrderType: Limit,
		Price:     "3000",
		Size:      "1",
		UserID:    uint64(3),
	}
	err = engine.PlaceOrder(ctx, marketID, order3)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		stats, err := engine.GetStats(marketID)
		return err == nil && stats.BidOrderCount == 1
	}, 1*time.Second, 10*time.Millisecond)

	_ = engine.Shutdown(ctx)
}

func TestManagement_SnapshotRestore(t *testing.T) {
	// Setup temporary directory for snapshots
	tmpDir := t.TempDir()

	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine", publish)
	marketID := "SUSPENDED-MARKET"

	// 1. Create Market with specific LotSize
	err := engine.CreateMarket("admin", marketID, "0.1")
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()

	// 2. Suspend Market
	err = engine.SuspendMarket("admin", marketID)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond) // Wait for processing

	// 3. Take Snapshot
	meta, err := engine.TakeSnapshot(tmpDir)
	require.NoError(t, err)
	assert.NotNil(t, meta)

	_ = engine.Shutdown(context.Background())

	// 4. Restore to New Engine
	newPublish := NewMemoryPublishLog()
	newEngine := NewMatchingEngine("test-engine-restored", newPublish)
	_, err = newEngine.RestoreFromSnapshot(tmpDir)
	require.NoError(t, err)

	// Start new engine event loop
	go newEngine.Run()

	// 5. Verify State (Should be Suspended)
	ctx := context.Background()
	order := &protocol.PlaceOrderCommand{
		OrderID:   "test-order",
		Side:      Buy,
		OrderType: Limit,
		Price:     "100",
		Size:      "1",
		UserID:    uint64(1),
	}
	err = newEngine.PlaceOrder(ctx, marketID, order)
	require.NoError(t, err)

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
	err = newEngine.ResumeMarket("admin", marketID)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	// 7. Test resumed functionality
	order2 := &protocol.PlaceOrderCommand{
		OrderID:   "test-order-2",
		Side:      Buy,
		OrderType: Limit,
		Price:     "100",
		Size:      "1",
		UserID:    uint64(2),
	}
	err = newEngine.PlaceOrder(ctx, marketID, order2)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		stats, err := newEngine.GetStats(marketID)
		return err == nil && stats.BidOrderCount == 1
	}, 1*time.Second, 10*time.Millisecond)

	_ = newEngine.Shutdown(ctx)
}

func TestManagement_UpdateConfig(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine", publish)
	marketID := "CONFIG-TEST"

	err := engine.CreateMarket("admin", marketID, "1.0")
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()

	// 1. Update MinLotSize
	err = engine.UpdateConfig("admin", marketID, "0.1")
	require.NoError(t, err)

	// Wait for async processing
	time.Sleep(50 * time.Millisecond)

	// 2. Verify new LotSize by placing an order
	ctx := context.Background()
	order := &protocol.PlaceOrderCommand{
		OrderID:   "cfg-order",
		Side:      Buy,
		OrderType: Market,
		QuoteSize: "5.0",
		Price:     "0",
		Size:      "0",
		UserID:    1,
	}

	// We'll need some liquidity to test Market order matching
	err = engine.PlaceOrder(ctx, marketID, &protocol.PlaceOrderCommand{
		OrderID:   "maker",
		Side:      Sell,
		OrderType: Limit,
		Price:     "10",
		Size:      "10",
		UserID:    uint64(2),
	})
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	err = engine.PlaceOrder(ctx, marketID, order)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		logs := publish.Logs()
		for _, l := range logs {
			if l.OrderID == "cfg-order" && l.Type == protocol.LogTypeMatch {
				return true
			}
		}
		return false
	}, 1*time.Second, 10*time.Millisecond)

	_ = engine.Shutdown(ctx)
}
