package match

import (
	"context"
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
		marketID := "BTC-USDT--" + "CreateMarketRequiresCommandID" + t.Name()
		_, err := submitCreateMarket(context.Background(), engine, &protocol.CreateMarketParams{
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
		future1, err := submitCreateMarket(ctx, engine, &protocol.CreateMarketParams{
			CommandID:  "place-orders-market-1",
			UserID:     1,
			MarketID:   market1,
			MinLotSize: "",
			Timestamp:  time.Now().UnixNano(),
		})
		require.NoError(t, err)

		// market2
		market2 := "ETH-USDT-" + t.Name() + "-2"
		future2, err := submitCreateMarket(ctx, engine, &protocol.CreateMarketParams{
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

		err = submitPlaceOrder(ctx, engine, order1)
		require.NoError(t, err)

		assert.Eventually(t, func() bool {
			future, e := engine.Query(ctx, &protocol.GetStatsRequest{MarketID: market1})
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

		err = submitPlaceOrder(ctx, engine, order2)
		require.NoError(t, err)

		assert.Eventually(t, func() bool {
			future, e := engine.Query(ctx, &protocol.GetStatsRequest{MarketID: market2})
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
		engine := NewMatchingEngine("test-engine-"+t.Name(), publishTrader)

		ctx := context.Background()

		market1 := "BTC-USDT-" + t.Name()
		future, err := submitCreateMarket(ctx, engine, &protocol.CreateMarketParams{
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

		err = submitPlaceOrder(ctx, engine, order1)
		require.NoError(t, err)

		// Wait for order to be in book
		assert.Eventually(t, func() bool {
			future, e := engine.Query(ctx, &protocol.GetStatsRequest{MarketID: market1})
			if e != nil {
				return false
			}
			stats, e := future.Wait(ctx)
			return e == nil && stats.BidOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		err = submitCancelOrder(
			ctx, engine,
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
			future, err := engine.Query(ctx, &protocol.GetStatsRequest{MarketID: market1})
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
		engine := NewMatchingEngine("test-engine-"+t.Name(), publishTrader)

		// Start engine event loop
		go engine.Run()

		market := "NON-EXISTENT"

		// PlaceOrder should still enqueue (market check happens on consumer side)
		ctx := context.Background()
		err := submitPlaceOrder(ctx, engine, &protocol.PlaceOrderParams{
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
		engine := NewMatchingEngine("test-engine-"+t.Name(), publishTrader)
		ctx := context.Background()

		go engine.Run()

		err := submitPlaceOrder(ctx, engine, &protocol.PlaceOrderParams{
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
		engine := NewMatchingEngine("test-engine-"+t.Name(), NewMemoryPublishLog())
		ctx := context.Background()

		go engine.Run()

		start := time.Now()
		future, err := engine.Query(ctx, &protocol.GetStatsRequest{MarketID: "NON-EXISTENT"})
		require.NoError(t, err)
		stats, err := future.Wait(ctx)
		assert.Nil(t, stats)
		require.ErrorIs(t, err, ErrNotFound)
		assert.Less(t, time.Since(start), 200*time.Millisecond)

		futureDepth, err := engine.Query(ctx, &protocol.GetDepthRequest{MarketID: "NON-EXISTENT", Limit: 10})
		require.NoError(t, err)
		depth, err := futureDepth.Wait(ctx)
		assert.Nil(t, depth)
		require.ErrorIs(t, err, ErrNotFound)

		_ = engine.Shutdown(ctx)
	})

	t.Run("CreateMarketRejectIncludesManagementUserID", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine-"+t.Name(), publishTrader)
		ctx := context.Background()
		marketID := "BTC-USDT--" + "CreateMarketRejectIncludesManagementUserID" + t.Name()

		go engine.Run()

		future1, err := submitCreateMarket(ctx, engine, &protocol.CreateMarketParams{
			CommandID:  "create-market-existing-1",
			UserID:     42,
			MarketID:   marketID,
			MinLotSize: "0.01",
			Timestamp:  1001,
		})
		require.NoError(t, err)
		_, err = future1.Wait(ctx)
		require.NoError(t, err)

		future2, err := submitCreateMarket(ctx, engine, &protocol.CreateMarketParams{
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
		engine := NewMatchingEngine("test-engine-"+t.Name(), publishTrader)
		ctx := context.Background()
		marketID := "BTC-USDT--" + "CreateMarketRequiresPositiveTimestamp" + t.Name()

		go engine.Run()

		future, err := submitCreateMarket(ctx, engine, &protocol.CreateMarketParams{
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

		statsFuture, err := engine.Query(ctx, &protocol.GetStatsRequest{MarketID: marketID})
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

	future, err := submitCreateMarket(ctx, engine, &protocol.CreateMarketParams{
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
	err = submitPlaceOrder(ctx, engine, &protocol.PlaceOrderParams{
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

	_ = engine.Shutdown(ctx)
}

func TestMatchingEngineShutdown(t *testing.T) {
	t.Run("ShutdownMultipleMarkets", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine-"+t.Name(), publishTrader)

		ctx := context.Background()

		// Create orders in multiple markets
		markets := []string{"BTC-USDT-" + t.Name(), "ETH-USDT-" + t.Name(), "SOL-USDT-" + t.Name()}
		futures := make([]*Future[any], 0, len(markets))
		for _, market := range markets {
			future, err := submitCreateMarket(ctx, engine, &protocol.CreateMarketParams{
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
			err := submitPlaceOrder(ctx, engine, order)
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
		err = submitPlaceOrder(ctx, engine, order)
		assert.Equal(t, ErrShutdown, err)
	})
}

func TestManagement_SuspendResume(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine-"+t.Name(), publish)
	marketID := "ETH-USDT--" + "ShutdownMultipleMarkets" + t.Name()
	ctx := context.Background()

	future, err := submitCreateMarket(ctx, engine, &protocol.CreateMarketParams{
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
	err = submitPlaceOrder(ctx, engine, order1)
	require.NoError(t, err)

	// Wait for Order-1
	assert.Eventually(t, func() bool {
		f, e := engine.Query(ctx, &protocol.GetStatsRequest{MarketID: marketID})
		if e != nil {
			return false
		}
		s, e := f.Wait(ctx)
		return e == nil && s.BidOrderCount == 1
	}, 1*time.Second, 10*time.Millisecond)

	// 2. Suspend Market
	futureSuspend, err := submitSuspendMarket(ctx, engine, &protocol.SuspendMarketParams{
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
	err = submitPlaceOrder(ctx, engine, order2)
	require.NoError(t, err)

	// Verify Reject Log
	assert.Eventually(t, func() bool {
		logs := publish.Logs()
		for _, l := range logs {
			if l.OrderID == "order-2" &&
				l.Type == protocol.LogTypeReject &&
				l.RejectReason == protocol.RejectReasonMarketSuspended {
				return true
			}
		}
		return false
	}, 1*time.Second, 10*time.Millisecond)

	// 4. Resume Market
	futureResume, err := submitResumeMarket(ctx, engine, &protocol.ResumeMarketParams{
		CommandID: "resume-market-1",
		UserID:    1,
		MarketID:  marketID,
		Timestamp: time.Now().UnixNano(),
	})
	require.NoError(t, err)
	res, err := futureResume.Wait(ctx)
	require.NoError(t, err)
	val, ok := res.(bool)
	require.True(t, ok)
	require.True(t, val)

	_ = engine.Shutdown(ctx)
}

func TestManagement_LateResponsePollution(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine-"+t.Name(), publish)
	marketID1 := "M1-" + t.Name()
	marketID2 := "M2-" + t.Name()

	ctxShort, cancelShort := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancelShort()

	future1, err := submitCreateMarket(context.Background(), engine, &protocol.CreateMarketParams{
		CommandID:  "cmd-1",
		UserID:     1,
		MarketID:   marketID1,
		MinLotSize: "0.01",
		Timestamp:  time.Now().UnixNano(),
	})
	require.NoError(t, err)

	_, err = future1.Wait(ctxShort)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	go engine.Run()
	time.Sleep(50 * time.Millisecond)

	ctxLong := context.Background()
	future2, err := submitCreateMarket(ctxLong, engine, &protocol.CreateMarketParams{
		CommandID:  "cmd-2",
		UserID:     1,
		MarketID:   marketID2,
		MinLotSize: "0.01",
		Timestamp:  time.Now().UnixNano(),
	})
	require.NoError(t, err)

	res, err := future2.Wait(ctxLong)
	require.NoError(t, err)
	val, ok := res.(bool)
	require.True(t, ok)
	require.True(t, val)

	_ = engine.Shutdown(ctxLong)
}
