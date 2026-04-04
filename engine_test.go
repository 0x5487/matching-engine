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
		engine := NewMatchingEngine("engine-"+t.Name(), NewMemoryPublishLog())
		assert.NotNil(t, engine)
		assert.Equal(t, "engine-"+t.Name(), engine.engineID)
		assert.NotNil(t, engine.orderbooks)
		assert.NotNil(t, engine.ring)
	})

	t.Run("CreateMarketRequiresCommandID", func(t *testing.T) {
		engine := NewMatchingEngine("test-engine-"+t.Name(), NewMemoryPublishLog())
		marketID := "BTC-USDT-" + t.Name()
		_, err := engine.CreateMarket(context.Background(), &protocol.CreateMarketParams{
			CommandID:  "",
			UserID:     1,
			MarketID:   marketID,
			MinLotSize: "0.01",
			Timestamp:  1,
		})
		require.ErrorIs(t, err, ErrInvalidParam)
	})

	t.Run("PlaceOrders", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine-"+t.Name(), publishTrader)

		ctx := context.Background()

		// market1
		market1 := "BTC-USDT-" + t.Name() + "-1"
		future1, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
			CommandID:  "place-orders-market-1",
			UserID:     1,
			MarketID:   market1,
			MinLotSize: "",
			Timestamp:  time.Now().UnixNano(),
		})
		require.NoError(t, err)

		// market2
		market2 := "ETH-USDT-" + t.Name() + "-2"
		future2, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
			CommandID:  "place-orders-market-2",
			UserID:     1,
			MarketID:   market2,
			MinLotSize: "",
			Timestamp:  time.Now().UnixNano(),
		})
		require.NoError(t, err)

		// Start engine event loop
		go engine.Run()

		_, err = future1.Wait(ctx)
		require.NoError(t, err)
		_, err = future2.Wait(ctx)
		require.NoError(t, err)

		order1 := &protocol.PlaceOrderParams{
			CommandID: "place-orders-order-1",
			MarketID:  market1,
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
			Timestamp: 1,
		}

		err = engine.PlaceOrder(ctx, order1)
		require.NoError(t, err)

		assert.Eventually(t, func() bool {
			future, e := engine.GetStats(ctx, market1)
			if e != nil {
				return false
			}
			stats, e := future.Wait(ctx)
			return e == nil && stats.BidOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		order2 := &protocol.PlaceOrderParams{
			CommandID: "place-orders-order-2",
			MarketID:  market2,
			OrderID:   "order2",
			OrderType: Limit,
			Side:      Sell,
			Price:     udecimal.MustFromInt64(110, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
			Timestamp: 2,
		}

		err = engine.PlaceOrder(ctx, order2)
		require.NoError(t, err)

		assert.Eventually(t, func() bool {
			future, e := engine.GetStats(ctx, market2)
			if e != nil {
				return false
			}
			stats, e := future.Wait(ctx)
			return e == nil && stats.AskOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		_ = engine.Shutdown(ctx)
	})

	t.Run("CancelOrder", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("engine-"+t.Name(), publishTrader)

		ctx := context.Background()

		market1 := "BTC-USDT-" + t.Name()
		future, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
			CommandID:  "cancel-order-market-1",
			UserID:     1,
			MarketID:   market1,
			MinLotSize: "",
			Timestamp:  time.Now().UnixNano(),
		})
		require.NoError(t, err)

		// Start engine event loop
		go engine.Run()

		_, err = future.Wait(ctx)
		require.NoError(t, err)

		order1 := &protocol.PlaceOrderParams{
			CommandID: "cancel-order-1",
			MarketID:  market1,
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
			UserID:    uint64(1),
			Timestamp: 1,
		}

		err = engine.PlaceOrder(ctx, order1)
		require.NoError(t, err)

		// Wait for order to be in book
		assert.Eventually(t, func() bool {
			future, e := engine.GetStats(ctx, market1)
			if e != nil {
				return false
			}
			stats, e := future.Wait(ctx)
			return e == nil && stats.BidOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		err = engine.CancelOrder(
			ctx,
			&protocol.CancelOrderParams{
				CommandID: "cancel-order-1-cancel",
				MarketID:  market1,
				OrderID:   order1.OrderID,
				UserID:    order1.UserID,
				Timestamp: 2,
			},
		)
		require.NoError(t, err)

		// validate
		assert.Eventually(t, func() bool {
			future, err := engine.GetStats(ctx, market1)
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
		engine := NewMatchingEngine("engine-"+t.Name(), publishTrader)

		// Start engine event loop
		go engine.Run()

		market := "NON-EXISTENT"

		// PlaceOrder should still enqueue (market check happens on consumer side)
		// The command goes to the RingBuffer but gets dropped silently by processCommand
		// since the market doesn't exist.
		ctx := context.Background()
		err := engine.PlaceOrder(ctx, &protocol.PlaceOrderParams{
			CommandID: "missing-market-o1",
			MarketID:  market,
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
		engine := NewMatchingEngine("engine-"+t.Name(), publishTrader)
		ctx := context.Background()

		go engine.Run()

		err := engine.PlaceOrder(ctx, &protocol.PlaceOrderParams{
			CommandID: "missing-market-order-cmd",
			MarketID:  "NON-EXISTENT",
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
		engine := NewMatchingEngine("engine-"+t.Name(), NewMemoryPublishLog())
		ctx := context.Background()

		go engine.Run()

		start := time.Now()
		future, err := engine.GetStats(ctx, "NON-EXISTENT")
		require.NoError(t, err)
		stats, err := future.Wait(ctx)
		assert.Nil(t, stats)
		require.ErrorIs(t, err, ErrNotFound)
		assert.Less(t, time.Since(start), 200*time.Millisecond)

		futureDepth, err := engine.Depth(ctx, "NON-EXISTENT", 10)
		require.NoError(t, err)
		depth, err := futureDepth.Wait(ctx)
		assert.Nil(t, depth)
		require.ErrorIs(t, err, ErrNotFound)

		_ = engine.Shutdown(ctx)
	})

	t.Run("CreateMarketRejectIncludesManagementUserID", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("engine-"+t.Name(), publishTrader)
		ctx := context.Background()
		marketID := "BTC-USDT--" + "CreateMarketRejectIncludesManagementUserID" + t.Name()

		go engine.Run()

		future1, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
			CommandID:  "create-market-existing-1",
			UserID:     42,
			MarketID:   marketID,
			MinLotSize: "0.01",
			Timestamp:  1001,
		})
		require.NoError(t, err)
		_, err = future1.Wait(ctx)
		require.NoError(t, err)

		future2, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
			CommandID:  "create-market-existing-2",
			UserID:     42,
			MarketID:   marketID,
			MinLotSize: "0.01",
			Timestamp:  1002,
		})
		require.NoError(t, err)
		_, err = future2.Wait(ctx)
		require.Error(t, err)

		assert.Eventually(t, func() bool {
			for _, log := range publishTrader.Logs() {
				if log.Type == protocol.LogTypeReject &&
					log.MarketID == marketID &&
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
		engine := NewMatchingEngine("engine-"+t.Name(), publishTrader)
		ctx := context.Background()
		marketID := "BTC-USDT--" + "CreateMarketRequiresPositiveTimestamp" + t.Name()

		go engine.Run()

		future, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
			CommandID:  "create-market-zero-ts",
			UserID:     77,
			MarketID:   marketID,
			MinLotSize: "0.01",
			Timestamp:  0,
		})
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
				log.MarketID == marketID &&
				log.UserID == 77 &&
				log.RejectReason == protocol.RejectReasonInvalidPayload &&
				log.Timestamp == 0
		}, time.Second, 10*time.Millisecond)

		statsFuture, err := engine.GetStats(ctx, marketID)
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
	marketID := "BTC-USDT--" + "CreateMarketRequiresPositiveTimestamp" + t.Name()

	future, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
		CommandID:  "prop-market-create",
		UserID:     1,
		MarketID:   marketID,
		MinLotSize: "",
		Timestamp:  time.Now().UnixNano(),
	})
	require.NoError(t, err)

	go engine.Run()

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// Single submit: explicit CommandID should be propagated to emitted logs.
	err = engine.PlaceOrder(ctx, &protocol.PlaceOrderParams{
		CommandID: "single-oid-cmd",
		MarketID:  marketID,
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
	err = engine.PlaceOrderBatch(ctx, marketID, []*protocol.PlaceOrderParams{
		{
			CommandID: "batch-oid-cmd-1",
			MarketID:  marketID,
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
			MarketID:  marketID,
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
		engine := NewMatchingEngine("engine-"+t.Name(), publishTrader)

		ctx := context.Background()

		// Create orders in multiple markets
		markets := []string{"BTC-USDT-" + t.Name(), "ETH-USDT-" + t.Name(), "SOL-USDT-" + t.Name()}
		futures := make([]*Future[bool], 0, len(markets))
		for _, market := range markets {
			future, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
				CommandID:  "shutdown-market-" + market,
				UserID:     1,
				MarketID:   market,
				MinLotSize: "",
				Timestamp:  time.Now().UnixNano(),
			})
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
			order := &protocol.PlaceOrderParams{
				CommandID: "shutdown-order-" + market,
				MarketID:  market,
				OrderID:   "order-" + market,
				OrderType: Limit,
				Side:      Buy,
				Price:     udecimal.MustFromInt64(int64(100+i*10), 0).String(),
				Size:      udecimal.MustFromInt64(1, 0).String(),
				Timestamp: int64(i + 1),
			}
			err := engine.PlaceOrder(ctx, order)
			require.NoError(t, err)
		}

		// Shutdown should complete successfully
		err := engine.Shutdown(ctx)
		require.NoError(t, err)

		// After shutdown, adding orders should return ErrShutdown
		order := &protocol.PlaceOrderParams{
			CommandID: "after-shutdown-order",
			MarketID:  markets[0],
			OrderID:   "after-shutdown",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(1, 0).String(),
			Timestamp: 1,
		}
		err = engine.PlaceOrder(ctx, order)
		assert.Equal(t, ErrShutdown, err)
	})

	t.Run("RejectsNewMarkets", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("engine-"+t.Name(), publishTrader)

		ctx := context.Background()
		marketID := "BTC-USDT--" + "RejectsNewMarkets" + t.Name()

		// Create one market first
		future, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
			CommandID:  "reject-new-market-create",
			UserID:     1,
			MarketID:   marketID,
			MinLotSize: "",
			Timestamp:  time.Now().UnixNano(),
		})
		require.NoError(t, err)

		// Start engine event loop
		go engine.Run()

		_, err = future.Wait(ctx)
		require.NoError(t, err)

		order := &protocol.PlaceOrderParams{
			CommandID: "reject-new-market-order-1",
			MarketID:  marketID,
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(1, 0).String(),
			Timestamp: 1,
		}
		err = engine.PlaceOrder(ctx, order)
		require.NoError(t, err)

		// Shutdown
		err = engine.Shutdown(ctx)
		require.NoError(t, err)

		// Try to create a new market after shutdown - should return ErrShutdown
		newMarketID := "NEW-MARKET-" + t.Name()
		newMarketOrder := &protocol.PlaceOrderParams{
			CommandID: "reject-new-market-order-2",
			MarketID:  newMarketID,
			OrderID:   "new-market-order",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(1, 0).String(),
			Timestamp: 2,
		}
		err = engine.PlaceOrder(ctx, newMarketOrder)
		assert.Equal(t, ErrShutdown, err)

		// OrderBook for new market should return nil
		book := engine.orderbooks[newMarketID]
		assert.Nil(t, book)
	})

	t.Run("RespectsContextTimeout", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("engine-"+t.Name(), publishTrader)

		ctx := context.Background()
		marketID := "BTC-USDT--" + "RespectsContextTimeout" + t.Name()

		// Create an order to ensure at least one market exists
		future, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
			CommandID:  "timeout-market-create",
			UserID:     1,
			MarketID:   marketID,
			MinLotSize: "",
			Timestamp:  time.Now().UnixNano(),
		})
		require.NoError(t, err)

		// Start engine event loop
		go engine.Run()

		_, err = future.Wait(ctx)
		require.NoError(t, err)

		order := &protocol.PlaceOrderParams{
			CommandID: "timeout-order-1",
			MarketID:  marketID,
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(1, 0).String(),
			Timestamp: 1,
		}
		err = engine.PlaceOrder(ctx, order)
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
	engine := NewMatchingEngine("engine-"+t.Name(), publishTrader)

	// 1. Setup State: Create 2 OrderBooks with Orders
	market1 := "BTC-USDT-" + t.Name() + "-1"
	market2 := "ETH-USDT-" + t.Name() + "-2"

	future1, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
		CommandID:  "snapshot-market-1-create",
		UserID:     1,
		MarketID:   market1,
		MinLotSize: "",
		Timestamp:  time.Now().UnixNano(),
	})
	require.NoError(t, err)
	future2, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
		CommandID:  "snapshot-market-2-create",
		UserID:     1,
		MarketID:   market2,
		MinLotSize: "",
		Timestamp:  time.Now().UnixNano(),
	})
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()

	_, err = future1.Wait(ctx)
	require.NoError(t, err)
	_, err = future2.Wait(ctx)
	require.NoError(t, err)

	// Add orders to Market 1
	err = engine.PlaceOrder(ctx, &protocol.PlaceOrderParams{
		CommandID: "snapshot-btc-buy-1",
		MarketID:  market1,
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
	err = engine.PlaceOrder(ctx, &protocol.PlaceOrderParams{
		CommandID: "snapshot-eth-sell-1",
		MarketID:  market2,
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
		f1, err1 := engine.GetStats(ctx, market1)
		f2, err2 := engine.GetStats(ctx, market2)
		if err1 != nil || err2 != nil {
			return false
		}
		s1, err1 := f1.Wait(ctx)
		s2, err2 := f2.Wait(ctx)
		return err1 == nil && err2 == nil &&
			s1.BidOrderCount == 1 && s2.AskOrderCount == 1
	}, 1*time.Second, 10*time.Millisecond)

	// 2. Take Snapshot
	meta, err := engine.TakeSnapshot(ctx, tmpDir)
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
	newEngine := NewMatchingEngine("engine-"+t.Name(), newPublishTrader)

	restoredMeta, err := newEngine.RestoreFromSnapshot(tmpDir)
	require.NoError(t, err)
	assert.NotNil(t, restoredMeta)
	assert.Equal(t, meta.GlobalLastCmdSeqID, restoredMeta.GlobalLastCmdSeqID)

	// Start new engine event loop
	go newEngine.Run()

	// 4. Verify Restored State
	assert.Eventually(t, func() bool {
		f1, err1 := newEngine.GetStats(ctx, market1)
		f2, err2 := newEngine.GetStats(ctx, market2)
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
	err = newEngine.PlaceOrder(ctx, &protocol.PlaceOrderParams{
		CommandID: "snapshot-btc-sell-match",
		MarketID:  market1,
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
		f1, err := newEngine.GetStats(ctx, market1)
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

	engine := NewMatchingEngine("engine-"+t.Name(), NewMemoryPublishLog())

	require.NotPanics(t, func() {
		_, restoreErr := engine.RestoreFromSnapshot(tmpDir)
		require.Error(t, restoreErr)
	})
}

func TestManagement_CreateMarket(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("engine-"+t.Name(), publish)
	marketID := "BTC-USDT--" + "RespectsContextTimeout" + t.Name()
	ctx := context.Background()

	// Start engine event loop
	go engine.Run()

	// 1. Create Market via Command
	// MinLotSize: 0.01
	future, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
		CommandID:  "create-market-1",
		UserID:     1,
		MarketID:   marketID,
		MinLotSize: "0.01",
		Timestamp:  time.Now().UnixNano(),
	})
	require.NoError(t, err)

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// Verify OrderBook existence
	assert.Eventually(t, func() bool {
		f, e := engine.GetStats(ctx, marketID)
		if e != nil {
			return false
		}
		_, e = f.Wait(ctx)
		return e == nil
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
	smallOrder := &protocol.PlaceOrderParams{
		CommandID: "small-order-1",
		MarketID:  marketID,
		OrderID:   "small-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(50000, 0).String(),
		Size:      "0.001", // Below 0.01
		UserID:    uint64(1),
		Timestamp: 1,
	}

	err = engine.PlaceOrder(ctx, smallOrder)
	require.NoError(t, err)

	_ = engine.Shutdown(ctx)
}

func TestManagement_CreateMarketRejectsInvalidConfig(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("engine-"+t.Name(), publish)
	ctx := context.Background()
	marketID := "BAD-MARKET--" + "RespectsContextTimeout" + t.Name()

	go engine.Run()

	future, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
		CommandID:  "bad-market-create",
		UserID:     1,
		MarketID:   marketID,
		MinLotSize: "not-a-decimal",
		Timestamp:  time.Now().UnixNano(),
	})
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

	f, err := engine.GetStats(ctx, marketID)
	require.NoError(t, err)
	stats, err := f.Wait(ctx)
	assert.Nil(t, stats)
	require.ErrorIs(t, err, ErrNotFound)

	_ = engine.Shutdown(ctx)
}

func TestManagement_CreateMarketRejectsDuplicateMarket(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("engine-"+t.Name(), publish)
	ctx := context.Background()
	marketID := "DUP-MARKET--" + "RespectsContextTimeout" + t.Name()

	go engine.Run()

	future1, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
		CommandID:  "dup-market-1",
		UserID:     1,
		MarketID:   marketID,
		MinLotSize: "0.1",
		Timestamp:  time.Now().UnixNano(),
	})
	require.NoError(t, err)
	_, err = future1.Wait(ctx)
	require.NoError(t, err)

	future2, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
		CommandID:  "dup-market-2",
		UserID:     1,
		MarketID:   marketID,
		MinLotSize: "0.1",
		Timestamp:  time.Now().UnixNano(),
	})
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
	engine := NewMatchingEngine("engine-"+t.Name(), publish)
	marketID := "ETH-USDT--" + "RespectsContextTimeout" + t.Name()
	ctx := context.Background()

	future, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
		CommandID:  "suspend-market-create",
		UserID:     1,
		MarketID:   marketID,
		MinLotSize: "0.0001",
		Timestamp:  time.Now().UnixNano(),
	})
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// 1. Place Order (Should Succeed)
	order1 := &protocol.PlaceOrderParams{
		CommandID: "suspend-order-1",
		MarketID:  marketID,
		OrderID:   "order-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     "3000",
		Size:      "1",
		UserID:    uint64(1),
		Timestamp: 1,
	}
	err = engine.PlaceOrder(ctx, order1)
	require.NoError(t, err)

	// Wait for Order-1
	assert.Eventually(t, func() bool {
		f, e := engine.GetStats(ctx, marketID)
		if e != nil {
			return false
		}
		s, e := f.Wait(ctx)
		return e == nil && s.BidOrderCount == 1
	}, 1*time.Second, 10*time.Millisecond)

	// 2. Suspend Market
	futureSuspend, err := engine.SuspendMarket(ctx, &protocol.SuspendMarketParams{
		CommandID: "suspend-market-1",
		UserID:    1,
		MarketID:  marketID,
		Timestamp: time.Now().UnixNano(),
	})
	require.NoError(t, err)
	_, err = futureSuspend.Wait(ctx)
	require.NoError(t, err)

	// 3. Place Order (Should be Rejected)
	order2 := &protocol.PlaceOrderParams{
		CommandID: "suspend-order-2",
		MarketID:  marketID,
		OrderID:   "order-2",
		Side:      Buy,
		OrderType: Limit,
		Price:     "3000",
		Size:      "1",
		UserID:    uint64(2),
		Timestamp: 2,
	}
	err = engine.PlaceOrder(ctx, order2)
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
	err = engine.CancelOrder(ctx, &protocol.CancelOrderParams{
		CommandID: "suspend-order-1-cancel",
		OrderID:   order1.OrderID,
		MarketID:  marketID,
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
	resumeFuture, err := engine.ResumeMarket(ctx, &protocol.ResumeMarketParams{
		CommandID: "resume-market-1",
		UserID:    1,
		MarketID:  marketID,
		Timestamp: time.Now().UnixNano(),
	})
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
	order3 := &protocol.PlaceOrderParams{
		CommandID: "suspend-order-3",
		MarketID:  marketID,
		OrderID:   "order-3",
		Side:      Buy,
		OrderType: Limit,
		Price:     "3000",
		Size:      "1",
		UserID:    uint64(3),
		Timestamp: 4,
	}
	err = engine.PlaceOrder(ctx, order3)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		future, err := engine.GetStats(ctx, marketID)
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
	engine := NewMatchingEngine("engine-"+t.Name(), publish)
	marketID := "SUSPENDED-MARKET--" + "RespectsContextTimeout" + t.Name()
	ctx := context.Background()

	// 1. Create Market with specific LotSize
	future, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
		CommandID:  "snapshot-market-create",
		UserID:     1,
		MarketID:   marketID,
		MinLotSize: "0.1",
		Timestamp:  time.Now().UnixNano(),
	})
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// 2. Suspend Market
	futureSuspend, err := engine.SuspendMarket(ctx, &protocol.SuspendMarketParams{
		CommandID: "snapshot-market-suspend",
		UserID:    1,
		MarketID:  marketID,
		Timestamp: time.Now().UnixNano(),
	})
	require.NoError(t, err)
	_, err = futureSuspend.Wait(ctx)
	require.NoError(t, err)

	// 3. Take Snapshot
	meta, err := engine.TakeSnapshot(ctx, tmpDir)
	require.NoError(t, err)
	assert.NotNil(t, meta)

	_ = engine.Shutdown(context.Background())

	// 4. Restore to New Engine
	newPublish := NewMemoryPublishLog()
	newEngine := NewMatchingEngine("engine-"+t.Name(), newPublish)
	_, err = newEngine.RestoreFromSnapshot(tmpDir)
	require.NoError(t, err)

	// Start new engine event loop
	go newEngine.Run()

	// 5. Verify State (Should be Suspended)
	order := &protocol.PlaceOrderParams{
		CommandID: "snapshot-test-order",
		MarketID:  marketID,
		OrderID:   "test-order",
		Side:      Buy,
		OrderType: Limit,
		Price:     "100",
		Size:      "1",
		UserID:    uint64(1),
		Timestamp: 1,
	}
	err = newEngine.PlaceOrder(ctx, order)
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
	futureResume, err := newEngine.ResumeMarket(ctx, &protocol.ResumeMarketParams{
		CommandID: "snapshot-market-resume",
		UserID:    1,
		MarketID:  marketID,
		Timestamp: time.Now().UnixNano(),
	})
	require.NoError(t, err)
	_, err = futureResume.Wait(ctx)
	require.NoError(t, err)

	// 7. Test resumed functionality
	order2 := &protocol.PlaceOrderParams{
		CommandID: "snapshot-test-order-2",
		MarketID:  marketID,
		OrderID:   "test-order-2",
		Side:      Buy,
		OrderType: Limit,
		Price:     "100",
		Size:      "1",
		UserID:    uint64(2),
		Timestamp: 2,
	}
	err = newEngine.PlaceOrder(ctx, order2)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		future, err := newEngine.GetStats(ctx, marketID)
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
	engine := NewMatchingEngine("engine-"+t.Name(), publish)
	marketID := "CONFIG-TEST--" + "RespectsContextTimeout" + t.Name()
	ctx := context.Background()

	future, err := engine.CreateMarket(ctx, &protocol.CreateMarketParams{
		CommandID:  "config-market-create",
		UserID:     1,
		MarketID:   marketID,
		MinLotSize: "1.0",
		Timestamp:  time.Now().UnixNano(),
	})
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// 1. Update MinLotSize
	updateFuture, err := engine.UpdateConfig(ctx, &protocol.UpdateConfigParams{
		CommandID:  "config-market-update",
		UserID:     1,
		MarketID:   marketID,
		MinLotSize: "0.1",
		Timestamp:  time.Now().UnixNano(),
	})
	require.NoError(t, err)

	_, err = updateFuture.Wait(ctx)
	require.NoError(t, err)

	// 2. Verify new LotSize by placing an order
	order := &protocol.PlaceOrderParams{
		CommandID: "config-order",
		OrderID:   "cfg-order",
		MarketID:  marketID,
		Side:      Buy,
		OrderType: Market,
		QuoteSize: "5.0",
		Price:     "0",
		Size:      "0",
		UserID:    1,
		Timestamp: 2,
	}

	// We'll need some liquidity to test Market order matching
	err = engine.PlaceOrder(ctx, &protocol.PlaceOrderParams{
		CommandID: "config-maker-order",
		OrderID:   "maker",
		MarketID:  marketID,
		Side:      Sell,
		OrderType: Limit,
		Price:     "10",
		Size:      "10",
		UserID:    uint64(2),
		Timestamp: 1,
	})
	require.NoError(t, err)

	err = engine.PlaceOrder(ctx, order)
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
	engine := NewMatchingEngine("engine-"+t.Name(), publish)
	marketID1 := "M1-" + t.Name()
	marketID2 := "M2-" + t.Name()

	ctxShort, cancelShort := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancelShort()

	// 1. Issue the first command with a very short timeout
	future1, err := engine.CreateMarket(context.Background(), &protocol.CreateMarketParams{
		CommandID:  "cmd-1",
		UserID:     1,
		MarketID:   marketID1,
		MinLotSize: "0.01",
		Timestamp:  time.Now().UnixNano(),
	})
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
	future2, err := engine.CreateMarket(ctxLong, &protocol.CreateMarketParams{
		CommandID:  "cmd-2",
		UserID:     1,
		MarketID:   marketID2,
		MinLotSize: "0.01",
		Timestamp:  time.Now().UnixNano(),
	})
	require.NoError(t, err)

	res, err := future2.Wait(ctxLong)
	require.NoError(t, err)
	require.True(t, res)

	// Check if M2 was actually created successfully (proves future2.Wait did not read the old cmd-1 success response)
	assert.Eventually(t, func() bool {
		future, err := engine.GetStats(ctxLong, marketID2)
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
	engine := NewMatchingEngine("engine-"+t.Name(), publish)
	go engine.Run()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	marketID := "UNKNOWN-MARKET--" + "RespectsContextTimeout" + t.Name()

	// Try to suspend a non-existent market
	future, err := engine.SuspendMarket(ctx, &protocol.SuspendMarketParams{
		CommandID: "cmd-unknown",
		UserID:    1,
		MarketID:  marketID,
		Timestamp: time.Now().UnixNano(),
	})
	require.NoError(t, err)

	// If fixed correctly, this should return an error immediately instead of waiting for ctx timeout
	start := time.Now()
	_, err = future.Wait(ctx)
	elapsed := time.Since(start)

	require.Error(t, err)
	// If elapsed time is close to 200ms, it means it hung until timeout
	require.Less(t, elapsed, 100*time.Millisecond, "Future should return immediately for unknown market")
	require.ErrorIs(t, err, ErrNotFound)

	_ = engine.Shutdown(ctx)
}
