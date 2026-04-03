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
	marketBTC    = "BTC-USDT"
	marketETH    = "ETH-USDT"
	testEngineID = "test-engine-1"
)

func TestMatchingEngineInitialization(t *testing.T) {
	t.Run("NewMatchingEngine", func(t *testing.T) {
		engine := NewMatchingEngine("test-engine", NewMemoryPublishLog())
		assert.NotNil(t, engine)
		assert.Equal(t, "test-engine", engine.engineID)
		assert.NotNil(t, engine.orderbooks)
		assert.NotNil(t, engine.ring)
	})

	t.Run("CreateMarketRequiresCommandID", func(t *testing.T) {
		engine := NewMatchingEngine("test-engine", NewMemoryPublishLog())
		_, err := engine.CreateMarket(context.Background(), "", 1, marketBTC, "0.01", 1)
		require.ErrorIs(t, err, ErrInvalidParam)
	})

	t.Run("PlaceOrders", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine", publishTrader)

		ctx := context.Background()

		// market1
		market1 := marketBTC
		future1, err := engine.CreateMarket(ctx, "place-orders-market-1", 1, market1, "", time.Now().UnixNano())
		require.NoError(t, err)

		// market2
		market2 := marketETH
		future2, err := engine.CreateMarket(ctx, "place-orders-market-2", 1, market2, "", time.Now().UnixNano())
		require.NoError(t, err)

		// Start engine event loop
		go engine.Run()

		_, err = future1.Wait(ctx)
		require.NoError(t, err)
		_, err = future2.Wait(ctx)
		require.NoError(t, err)

		order1 := &protocol.PlaceOrderCommand{
			CommandID: "place-orders-order-1",
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
			Timestamp: 1,
		}

		err = engine.PlaceOrder(ctx, market1, order1)
		require.NoError(t, err)

		assert.Eventually(t, func() bool {
			future, err := engine.GetStats(market1)
			if err != nil {
				return false
			}
			stats, err := future.Wait(ctx)
			return err == nil && stats.BidOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		order2 := &protocol.PlaceOrderCommand{
			CommandID: "place-orders-order-2",
			OrderID:   "order2",
			OrderType: Limit,
			Side:      Sell,
			Price:     udecimal.MustFromInt64(110, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
			Timestamp: 2,
		}

		err = engine.PlaceOrder(ctx, market2, order2)
		require.NoError(t, err)

		assert.Eventually(t, func() bool {
			future, err := engine.GetStats(market2)
			if err != nil {
				return false
			}
			stats, err := future.Wait(ctx)
			return err == nil && stats.AskOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		_ = engine.Shutdown(ctx)
	})

	t.Run("CancelOrder", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine", publishTrader)

		ctx := context.Background()

		market1 := marketBTC
		future, err := engine.CreateMarket(ctx, "cancel-order-market-1", 1, market1, "", time.Now().UnixNano())
		require.NoError(t, err)

		// Start engine event loop
		go engine.Run()

		_, err = future.Wait(ctx)
		require.NoError(t, err)

		order1 := &protocol.PlaceOrderCommand{
			CommandID: "cancel-order-1",
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
			UserID:    uint64(1),
			Timestamp: 1,
		}

		err = engine.PlaceOrder(ctx, market1, order1)
		require.NoError(t, err)

		// Wait for order to be in book
		assert.Eventually(t, func() bool {
			future, err := engine.GetStats(market1)
			if err != nil {
				return false
			}
			stats, err := future.Wait(ctx)
			return err == nil && stats.BidOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		err = engine.CancelOrder(
			ctx,
			market1,
			&protocol.CancelOrderCommand{
				CommandID: "cancel-order-1-cancel",
				OrderID:   order1.OrderID,
				UserID:    order1.UserID,
				Timestamp: 2,
			},
		)
		require.NoError(t, err)

		// validate
		assert.Eventually(t, func() bool {
			future, err := engine.GetStats(market1)
			if err != nil {
				return false
			}
			stats, err := future.Wait(ctx)
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
		err := engine.PlaceOrder(ctx, market, &protocol.PlaceOrderCommand{
			CommandID: "missing-market-o1",
			OrderID:   "o1",
			Timestamp: 1,
		})
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
			CommandID: "missing-market-order-cmd",
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
		future, err := engine.GetStats("NON-EXISTENT")
		require.NoError(t, err)
		stats, err := future.Wait(ctx)
		assert.Nil(t, stats)
		require.ErrorIs(t, err, ErrNotFound)
		assert.Less(t, time.Since(start), 200*time.Millisecond)

		depth, err := engine.Depth("NON-EXISTENT", 10)
		assert.Nil(t, depth)
		require.ErrorIs(t, err, ErrNotFound)

		_ = engine.Shutdown(ctx)
	})

	t.Run("CreateMarketRejectIncludesManagementUserID", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine", publishTrader)
		ctx := context.Background()

		go engine.Run()

		future1, err := engine.CreateMarket(ctx, "create-market-existing-1", 42, "BTC-USDT", "0.01", 1001)
		require.NoError(t, err)
		_, err = future1.Wait(ctx)
		require.NoError(t, err)

		future2, err := engine.CreateMarket(ctx, "create-market-existing-2", 42, "BTC-USDT", "0.01", 1002)
		require.NoError(t, err)
		_, err = future2.Wait(ctx)
		require.Error(t, err)

		assert.Eventually(t, func() bool {
			for _, log := range publishTrader.Logs() {
				if log.Type == protocol.LogTypeReject &&
					log.MarketID == marketBTC &&
					log.RejectReason == protocol.RejectReasonMarketAlreadyExists {
					return log.UserID == 42 && log.Timestamp == 1002
				}
			}
			return false
		}, time.Second, 10*time.Millisecond)

		_ = engine.Shutdown(ctx)
	})

	t.Run("CreateMarketRequiresPositiveTimestamp", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine", publishTrader)
		ctx := context.Background()

		go engine.Run()

		future, err := engine.CreateMarket(ctx, "create-market-zero-ts", 77, "BTC-USDT", "0.01", 0)
		require.NoError(t, err)
		_, err = future.Wait(ctx)
		require.Error(t, err)

		assert.Eventually(t, func() bool {
			logs := publishTrader.Logs()
			if len(logs) != 1 {
				return false
			}
			log := logs[0]
			return log.Type == protocol.LogTypeReject &&
				log.MarketID == "BTC-USDT" &&
				log.UserID == 77 &&
				log.RejectReason == protocol.RejectReasonInvalidPayload &&
				log.Timestamp == 0
		}, time.Second, 10*time.Millisecond)

		statsFuture, err := engine.GetStats("BTC-USDT")
		require.NoError(t, err)
		_, statsErr := statsFuture.Wait(ctx)
		require.ErrorIs(t, statsErr, ErrNotFound)

		_ = engine.Shutdown(ctx)
	})
}

func TestCommandAndEngineIDPropagation(t *testing.T) {
	ctx := context.Background()
	publishTrader := NewMemoryPublishLog()
	engine := NewMatchingEngine(testEngineID, publishTrader)
	marketID := marketBTC

	future, err := engine.CreateMarket(ctx, "prop-market-create", 1, marketID, "", time.Now().UnixNano())
	require.NoError(t, err)

	go engine.Run()

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// Single submit: explicit CommandID should be propagated to emitted logs.
	err = engine.PlaceOrder(ctx, marketID, &protocol.PlaceOrderCommand{
		CommandID: "single-oid-cmd",
		OrderID:   "single-oid",
		OrderType: Limit,
		Side:      Buy,
		Price:     "100",
		Size:      "1",
		UserID:    1,
		Timestamp: 1,
	})
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		for _, log := range publishTrader.Logs() {
			if log.OrderID == "single-oid" && log.Type == protocol.LogTypeOpen {
				return log.CommandID == "single-oid-cmd" && log.EngineID == testEngineID
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)

	// Batch submit: each command should keep its explicit CommandID.
	err = engine.PlaceOrderBatch(ctx, marketID, []*protocol.PlaceOrderCommand{
		{
			CommandID: "batch-oid-cmd-1",
			OrderID:   "batch-oid-1",
			OrderType: Limit,
			Side:      Buy,
			Price:     "99",
			Size:      "1",
			UserID:    2,
			Timestamp: 2,
		},
		{
			CommandID: "batch-oid-cmd-2",
			OrderID:   "batch-oid-2",
			OrderType: Limit,
			Side:      Sell,
			Price:     "101",
			Size:      "1",
			UserID:    3,
			Timestamp: 3,
		},
	})
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		found1 := false
		found2 := false
		for _, log := range publishTrader.Logs() {
			if log.OrderID == "batch-oid-1" && log.Type == protocol.LogTypeOpen {
				found1 = log.CommandID == "batch-oid-cmd-1" && log.EngineID == testEngineID
			}
			if log.OrderID == "batch-oid-2" && log.Type == protocol.LogTypeOpen {
				found2 = log.CommandID == "batch-oid-cmd-2" && log.EngineID == testEngineID
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
		futures := make([]*Future[bool], 0, len(markets))
		for _, market := range markets {
			future, err := engine.CreateMarket(ctx, "shutdown-market-"+market, 1, market, "", time.Now().UnixNano())
			require.NoError(t, err)
			futures = append(futures, future)
		}

		// Start engine event loop
		go engine.Run()

		for _, future := range futures {
			_, err := future.Wait(ctx)
			require.NoError(t, err)
		}

		for i, market := range markets {
			order := &protocol.PlaceOrderCommand{
				CommandID: "shutdown-order-" + market,
				OrderID:   "order-" + market,
				OrderType: Limit,
				Side:      Buy,
				Price:     udecimal.MustFromInt64(int64(100+i*10), 0).String(),
				Size:      udecimal.MustFromInt64(1, 0).String(),
				Timestamp: int64(i + 1),
			}
			err := engine.PlaceOrder(ctx, market, order)
			require.NoError(t, err)
		}

		// Shutdown should complete successfully
		err := engine.Shutdown(ctx)
		require.NoError(t, err)

		// After shutdown, adding orders should return ErrShutdown
		order := &protocol.PlaceOrderCommand{
			CommandID: "after-shutdown-order",
			OrderID:   "after-shutdown",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(1, 0).String(),
			Timestamp: 1,
		}
		err = engine.PlaceOrder(ctx, "BTC-USDT", order)
		assert.Equal(t, ErrShutdown, err)
	})

	t.Run("RejectsNewMarkets", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine", publishTrader)

		ctx := context.Background()

		// Create one market first
		future, err := engine.CreateMarket(ctx, "reject-new-market-create", 1, "BTC-USDT", "", time.Now().UnixNano())
		require.NoError(t, err)

		// Start engine event loop
		go engine.Run()

		_, err = future.Wait(ctx)
		require.NoError(t, err)

		order := &protocol.PlaceOrderCommand{
			CommandID: "reject-new-market-order-1",
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(1, 0).String(),
			Timestamp: 1,
		}
		err = engine.PlaceOrder(ctx, "BTC-USDT", order)
		require.NoError(t, err)

		// Shutdown
		err = engine.Shutdown(ctx)
		require.NoError(t, err)

		// Try to create a new market after shutdown - should return ErrShutdown
		newMarketOrder := &protocol.PlaceOrderCommand{
			CommandID: "reject-new-market-order-2",
			OrderID:   "new-market-order",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(1, 0).String(),
			Timestamp: 2,
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
		future, err := engine.CreateMarket(ctx, "timeout-market-create", 1, "BTC-USDT", "", time.Now().UnixNano())
		require.NoError(t, err)

		// Start engine event loop
		go engine.Run()

		_, err = future.Wait(ctx)
		require.NoError(t, err)

		order := &protocol.PlaceOrderCommand{
			CommandID: "timeout-order-1",
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(1, 0).String(),
			Timestamp: 1,
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

	future1, err := engine.CreateMarket(ctx, "snapshot-market-1-create", 1, market1, "", time.Now().UnixNano())
	require.NoError(t, err)
	future2, err := engine.CreateMarket(ctx, "snapshot-market-2-create", 1, market2, "", time.Now().UnixNano())
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()

	_, err = future1.Wait(ctx)
	require.NoError(t, err)
	_, err = future2.Wait(ctx)
	require.NoError(t, err)

	// Add orders to Market 1
	err = engine.PlaceOrder(ctx, market1, &protocol.PlaceOrderCommand{
		CommandID: "snapshot-btc-buy-1",
		OrderID:   "btc-buy-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(50000, 0).String(),
		Size:      udecimal.MustFromInt64(1, 0).String(),
		UserID:    uint64(1),
		Timestamp: 1,
	})
	require.NoError(t, err)

	// Add orders to Market 2
	err = engine.PlaceOrder(ctx, market2, &protocol.PlaceOrderCommand{
		CommandID: "snapshot-eth-sell-1",
		OrderID:   "eth-sell-1",
		Side:      Sell,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(3000, 0).String(),
		Size:      udecimal.MustFromInt64(10, 0).String(),
		UserID:    uint64(2),
		Timestamp: 2,
	})
	require.NoError(t, err)

	// Wait for processing
	assert.Eventually(t, func() bool {
		f1, err1 := engine.GetStats(market1)
		f2, err2 := engine.GetStats(market2)
		if err1 != nil || err2 != nil {
			return false
		}
		s1, err1 := f1.Wait(ctx)
		s2, err2 := f2.Wait(ctx)
		return err1 == nil && err2 == nil &&
			s1.BidOrderCount == 1 && s2.AskOrderCount == 1
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
		f1, err1 := newEngine.GetStats(market1)
		f2, err2 := newEngine.GetStats(market2)
		if err1 != nil || err2 != nil {
			return false
		}
		s1, err1 := f1.Wait(ctx)
		s2, err2 := f2.Wait(ctx)
		return err1 == nil && err2 == nil &&
			s1.BidOrderCount == 1 && s1.AskOrderCount == 0 &&
			s2.BidOrderCount == 0 && s2.AskOrderCount == 1
	}, 1*time.Second, 10*time.Millisecond)

	// 5. Verify Continuity (Add new orders to restored engine)
	err = newEngine.PlaceOrder(ctx, market1, &protocol.PlaceOrderCommand{
		CommandID: "snapshot-btc-sell-match",
		OrderID:   "btc-sell-match",
		Side:      Sell,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(50000, 0).String(),
		Size:      udecimal.MustFromInt64(1, 0).String(),
		UserID:    uint64(3),
		Timestamp: 3,
	})
	require.NoError(t, err)

	// Should match btc-buy-1
	assert.Eventually(t, func() bool {
		f1, err := newEngine.GetStats(market1)
		if err != nil {
			return false
		}
		s1, err := f1.Wait(ctx)
		return err == nil && s1.BidOrderCount == 0 && s1.AskOrderCount == 0
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
	ctx := context.Background()

	// Start engine event loop
	go engine.Run()

	// 1. Create Market via Command
	// MinLotSize: 0.01
	future, err := engine.CreateMarket(ctx, "create-market-1", 1, marketID, "0.01", time.Now().UnixNano())
	require.NoError(t, err)

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// Verify OrderBook existence
	assert.Eventually(t, func() bool {
		f, err := engine.GetStats(marketID)
		if err != nil {
			return false
		}
		_, err = f.Wait(ctx)
		return err == nil
	}, 1*time.Second, 10*time.Millisecond)

	assert.Eventually(t, func() bool {
		for _, log := range publish.Logs() {
			if log.Type == protocol.LogTypeAdmin &&
				log.CommandID == "create-market-1" &&
				log.MarketID == marketID &&
				log.UserID == 1 &&
				log.EventType == "market_created" {
				return true
			}
		}
		return false
	}, 1*time.Second, 10*time.Millisecond)

	// Verify MinLotSize configuration by placing a small order
	smallOrder := &protocol.PlaceOrderCommand{
		CommandID: "small-order-1",
		OrderID:   "small-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(50000, 0).String(),
		Size:      "0.001", // Below 0.01
		UserID:    uint64(1),
		Timestamp: 1,
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

	future, err := engine.CreateMarket(
		ctx,
		"bad-market-create",
		1,
		"BAD-MARKET",
		"not-a-decimal",
		time.Now().UnixNano(),
	)
	require.NoError(t, err)
	_, err = future.Wait(ctx)
	require.Error(t, err)

	assert.Eventually(t, func() bool {
		for _, log := range publish.Logs() {
			if log.CommandID != "" && log.RejectReason == protocol.RejectReasonInvalidPayload {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)

	f, err := engine.GetStats("BAD-MARKET")
	require.NoError(t, err)
	stats, err := f.Wait(ctx)
	assert.Nil(t, stats)
	require.ErrorIs(t, err, ErrNotFound)

	_ = engine.Shutdown(ctx)
}

func TestManagement_CreateMarketRejectsDuplicateMarket(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine", publish)
	ctx := context.Background()

	go engine.Run()

	future1, err := engine.CreateMarket(ctx, "dup-market-1", 1, "DUP-MARKET", "0.1", time.Now().UnixNano())
	require.NoError(t, err)
	_, err = future1.Wait(ctx)
	require.NoError(t, err)

	future2, err := engine.CreateMarket(ctx, "dup-market-2", 1, "DUP-MARKET", "0.1", time.Now().UnixNano())
	require.NoError(t, err)
	_, err = future2.Wait(ctx)
	require.Error(t, err)

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

	future, err := engine.CreateMarket(ctx, "suspend-market-create", 1, marketID, "0.0001", time.Now().UnixNano())
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// 1. Place Order (Should Succeed)
	order1 := &protocol.PlaceOrderCommand{
		CommandID: "suspend-order-1",
		OrderID:   "order-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     "3000",
		Size:      "1",
		UserID:    uint64(1),
		Timestamp: 1,
	}
	err = engine.PlaceOrder(ctx, marketID, order1)
	require.NoError(t, err)

	// Wait for Order-1
	assert.Eventually(t, func() bool {
		f, err := engine.GetStats(marketID)
		if err != nil {
			return false
		}
		s, err := f.Wait(ctx)
		return err == nil && s.BidOrderCount == 1
	}, 1*time.Second, 10*time.Millisecond)

	// 2. Suspend Market
	futureSuspend, err := engine.SuspendMarket(ctx, "suspend-market-1", 1, marketID, time.Now().UnixNano())
	require.NoError(t, err)
	_, err = futureSuspend.Wait(ctx)
	require.NoError(t, err)

	// 3. Place Order (Should be Rejected)
	order2 := &protocol.PlaceOrderCommand{
		CommandID: "suspend-order-2",
		OrderID:   "order-2",
		Side:      Buy,
		OrderType: Limit,
		Price:     "3000",
		Size:      "1",
		UserID:    uint64(2),
		Timestamp: 2,
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
		CommandID: "suspend-order-1-cancel",
		OrderID:   order1.OrderID,
		UserID:    order1.UserID,
		Timestamp: 3,
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
	resumeFuture, err := engine.ResumeMarket(ctx, "resume-market-1", 1, marketID, time.Now().UnixNano())
	require.NoError(t, err)
	_, err = resumeFuture.Wait(ctx)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		for _, l := range publish.Logs() {
			if l.Type == protocol.LogTypeAdmin &&
				l.CommandID == "suspend-market-1" &&
				l.MarketID == marketID &&
				l.UserID == 1 &&
				l.EventType == "market_suspended" {
				return true
			}
		}
		return false
	}, 1*time.Second, 10*time.Millisecond)

	assert.Eventually(t, func() bool {
		for _, l := range publish.Logs() {
			if l.Type == protocol.LogTypeAdmin &&
				l.CommandID == "resume-market-1" &&
				l.MarketID == marketID &&
				l.UserID == 1 &&
				l.EventType == "market_resumed" {
				return true
			}
		}
		return false
	}, 1*time.Second, 10*time.Millisecond)

	// 6. Place Order (Should Succeed again)
	order3 := &protocol.PlaceOrderCommand{
		CommandID: "suspend-order-3",
		OrderID:   "order-3",
		Side:      Buy,
		OrderType: Limit,
		Price:     "3000",
		Size:      "1",
		UserID:    uint64(3),
		Timestamp: 4,
	}
	err = engine.PlaceOrder(ctx, marketID, order3)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		future, err := engine.GetStats(marketID)
		if err != nil {
			return false
		}
		stats, err := future.Wait(ctx)
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
	ctx := context.Background()

	// 1. Create Market with specific LotSize
	future, err := engine.CreateMarket(ctx, "snapshot-market-create", 1, marketID, "0.1", time.Now().UnixNano())
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// 2. Suspend Market
	futureSuspend, err := engine.SuspendMarket(ctx, "snapshot-market-suspend", 1, marketID, time.Now().UnixNano())
	require.NoError(t, err)
	_, err = futureSuspend.Wait(ctx)
	require.NoError(t, err)

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
	order := &protocol.PlaceOrderCommand{
		CommandID: "snapshot-test-order",
		OrderID:   "test-order",
		Side:      Buy,
		OrderType: Limit,
		Price:     "100",
		Size:      "1",
		UserID:    uint64(1),
		Timestamp: 1,
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
	futureResume, err := newEngine.ResumeMarket(ctx, "snapshot-market-resume", 1, marketID, time.Now().UnixNano())
	require.NoError(t, err)
	_, err = futureResume.Wait(ctx)
	require.NoError(t, err)

	// 7. Test resumed functionality
	order2 := &protocol.PlaceOrderCommand{
		CommandID: "snapshot-test-order-2",
		OrderID:   "test-order-2",
		Side:      Buy,
		OrderType: Limit,
		Price:     "100",
		Size:      "1",
		UserID:    uint64(2),
		Timestamp: 2,
	}
	err = newEngine.PlaceOrder(ctx, marketID, order2)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		future, err := newEngine.GetStats(marketID)
		if err != nil {
			return false
		}
		stats, err := future.Wait(ctx)
		return err == nil && stats.BidOrderCount == 1
	}, 1*time.Second, 10*time.Millisecond)

	_ = newEngine.Shutdown(ctx)
}

func TestManagement_UpdateConfig(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine", publish)
	marketID := "CONFIG-TEST"
	ctx := context.Background()

	future, err := engine.CreateMarket(ctx, "config-market-create", 1, marketID, "1.0", time.Now().UnixNano())
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// 1. Update MinLotSize
	updateFuture, err := engine.UpdateConfig(ctx, "config-market-update", 1, marketID, "0.1", time.Now().UnixNano())
	require.NoError(t, err)

	_, err = updateFuture.Wait(ctx)
	require.NoError(t, err)

	// 2. Verify new LotSize by placing an order
	order := &protocol.PlaceOrderCommand{
		CommandID: "config-order",
		OrderID:   "cfg-order",
		Side:      Buy,
		OrderType: Market,
		QuoteSize: "5.0",
		Price:     "0",
		Size:      "0",
		UserID:    1,
		Timestamp: 2,
	}

	// We'll need some liquidity to test Market order matching
	err = engine.PlaceOrder(ctx, marketID, &protocol.PlaceOrderCommand{
		CommandID: "config-maker-order",
		OrderID:   "maker",
		Side:      Sell,
		OrderType: Limit,
		Price:     "10",
		Size:      "10",
		UserID:    uint64(2),
		Timestamp: 1,
	})
	require.NoError(t, err)

	err = engine.PlaceOrder(ctx, marketID, order)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		for _, l := range publish.Logs() {
			if l.Type == protocol.LogTypeAdmin &&
				l.CommandID == "config-market-update" &&
				l.MarketID == marketID &&
				l.UserID == 1 &&
				l.EventType == "market_config_updated" {
				return true
			}
		}
		return false
	}, 1*time.Second, 10*time.Millisecond)

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

func TestManagement_LateResponsePollution(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("engine-1", publish)

	ctxShort, cancelShort := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancelShort()

	// 1. Issue the first command with a very short timeout
	future1, err := engine.CreateMarket(context.Background(), "cmd-1", 1, "M1", "0.01", time.Now().UnixNano())
	require.NoError(t, err)

	// Simulate timeout exit
	_, err = future1.Wait(ctxShort)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	// 2. Start the engine and process cmd-1
	go engine.Run()

	// Wait for a short time to ensure cmd-1 is processed and response is sent to the abandoned channel
	time.Sleep(50 * time.Millisecond)

	// 3. Issue the second command
	ctxLong := context.Background()
	// At this point, the engine should acquire a new clean channel from the pool
	future2, err := engine.CreateMarket(ctxLong, "cmd-2", 1, "M2", "0.01", time.Now().UnixNano())
	require.NoError(t, err)

	res, err := future2.Wait(ctxLong)
	require.NoError(t, err)
	require.True(t, res)

	// Check if M2 was actually created successfully (proves future2.Wait did not read the old cmd-1 success response)
	assert.Eventually(t, func() bool {
		future, err := engine.GetStats("M2")
		if err != nil {
			return false
		}
		stats, err := future.Wait(ctxLong)
		return err == nil && stats != nil
	}, 1*time.Second, 10*time.Millisecond)

	_ = engine.Shutdown(ctxLong)
}

func TestManagement_UnknownMarketFuture(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("engine-1", publish)
	go engine.Run()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Try to suspend a non-existent market
	future, err := engine.SuspendMarket(ctx, "cmd-unknown", 1, "UNKNOWN-MARKET", time.Now().UnixNano())
	require.NoError(t, err)

	// If fixed correctly, this should return an error immediately instead of waiting for ctx timeout
	start := time.Now()
	_, err = future.Wait(ctx)
	elapsed := time.Since(start)

	require.Error(t, err)
	// If elapsed time is close to 200ms, it means it hung until timeout
	require.Less(t, elapsed, 100*time.Millisecond, "Future should return immediately for unknown market")
	require.Contains(t, err.Error(), string(protocol.RejectReasonMarketNotFound))

	_ = engine.Shutdown(ctx)
}
