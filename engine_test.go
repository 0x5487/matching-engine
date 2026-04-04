package match

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

const (
	marketBTC    = "BTC-USDT"
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
		cmd := &protocol.Command{
			Type:      protocol.CmdCreateMarket,
			UserID:    1,
			MarketID:  marketID,
			CommandID: "",
			Timestamp: 1,
		}
		_ = cmd.SetPayload(&protocol.CreateMarketParams{
			MinLotSize: "0.01",
		})
		_, err := engine.Submit(context.Background(), cmd)
		require.ErrorIs(t, err, ErrInvalidParam)
	})

	t.Run("PlaceOrders", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine-"+t.Name(), publishTrader)

		ctx := context.Background()

		// market1
		market1 := "BTC-USDT-" + t.Name() + "-1"
		now := time.Now().UnixNano()
		cmd1 := &protocol.Command{
			Type:      protocol.CmdCreateMarket,
			UserID:    1,
			MarketID:  market1,
			CommandID: "place-orders-market-1",
			Timestamp: now,
		}
		_ = cmd1.SetPayload(&protocol.CreateMarketParams{
			MinLotSize: "",
		})
		future1, err := engine.Submit(ctx, cmd1)
		require.NoError(t, err)

		// market2
		market2 := "ETH-USDT-" + t.Name() + "-2"
		now2 := time.Now().UnixNano()
		cmd2 := &protocol.Command{
			Type:      protocol.CmdCreateMarket,
			UserID:    1,
			MarketID:  market2,
			CommandID: "place-orders-market-2",
			Timestamp: now2,
		}
		_ = cmd2.SetPayload(&protocol.CreateMarketParams{
			MinLotSize: "",
		})
		future2, err := engine.Submit(ctx, cmd2)
		require.NoError(t, err)

		// Start engine event loop
		go engine.Run()

		_, err = future1.Wait(ctx)
		require.NoError(t, err)
		_, err = future2.Wait(ctx)
		require.NoError(t, err)

		order1 := &protocol.PlaceOrderParams{
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
		}

		cmdOrder1 := &protocol.Command{
			Type:      protocol.CmdPlaceOrder,
			UserID:    0,
			MarketID:  market1,
			CommandID: "place-orders-order-1",
			Timestamp: 1,
		}
		_ = cmdOrder1.SetPayload(order1)
		err = engine.SubmitAsync(ctx, cmdOrder1)
		require.NoError(t, err)

		assert.Eventually(t, func() bool {
			future, e := engine.Query(ctx, &protocol.GetStatsRequest{MarketID: market1})
			if e != nil {
				return false
			}
			res, e := future.Wait(ctx)
			stats, ok := res.(*protocol.GetStatsResponse)
			return e == nil && ok && stats.BidOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		order2 := &protocol.PlaceOrderParams{
			OrderID:   "order2",
			OrderType: Limit,
			Side:      Sell,
			Price:     udecimal.MustFromInt64(110, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
		}

		cmdOrder2 := &protocol.Command{
			Type:      protocol.CmdPlaceOrder,
			UserID:    0,
			MarketID:  market2,
			CommandID: "place-orders-order-2",
			Timestamp: 2,
		}
		_ = cmdOrder2.SetPayload(order2)
		err = engine.SubmitAsync(ctx, cmdOrder2)
		require.NoError(t, err)

		assert.Eventually(t, func() bool {
			future, e := engine.Query(ctx, &protocol.GetStatsRequest{MarketID: market2})
			if e != nil {
				return false
			}
			res, e := future.Wait(ctx)
			stats, ok := res.(*protocol.GetStatsResponse)
			return e == nil && ok && stats.AskOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		_ = engine.Shutdown(ctx)
	})

	t.Run("CancelOrder", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine-"+t.Name(), publishTrader)

		ctx := context.Background()

		market1 := "BTC-USDT-" + t.Name()
		cmdMarket1 := &protocol.Command{
			Type:      protocol.CmdCreateMarket,
			UserID:    1,
			MarketID:  market1,
			CommandID: "cancel-order-market-1",
			Timestamp: time.Now().UnixNano(),
		}
		_ = cmdMarket1.SetPayload(&protocol.CreateMarketParams{
			MinLotSize: "",
		})
		future, err := engine.Submit(ctx, cmdMarket1)
		require.NoError(t, err)

		// Start engine event loop
		go engine.Run()

		_, err = future.Wait(ctx)
		require.NoError(t, err)

		order1 := &protocol.PlaceOrderParams{
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
		}

		cmdPlace1 := &protocol.Command{
			Type:      protocol.CmdPlaceOrder,
			UserID:    1,
			MarketID:  market1,
			CommandID: "cancel-order-1",
			Timestamp: 1,
		}
		_ = cmdPlace1.SetPayload(order1)
		err = engine.SubmitAsync(ctx, cmdPlace1)
		require.NoError(t, err)

		// Wait for order to be in book
		assert.Eventually(t, func() bool {
			future, e := engine.Query(ctx, &protocol.GetStatsRequest{MarketID: market1})
			if e != nil {
				return false
			}
			res, e := future.Wait(ctx)
			stats, ok := res.(*protocol.GetStatsResponse)
			return e == nil && ok && stats.BidOrderCount == 1
		}, 1*time.Second, 10*time.Millisecond)

		cmdCancel1 := &protocol.Command{
			Type:      protocol.CmdCancelOrder,
			UserID:    1,
			MarketID:  market1,
			CommandID: "cancel-order-1-cancel",
			Timestamp: 2,
		}
		_ = cmdCancel1.SetPayload(&protocol.CancelOrderParams{
			OrderID: order1.OrderID,
		})
		err = engine.SubmitAsync(ctx, cmdCancel1)
		require.NoError(t, err)

		// validate
		assert.Eventually(t, func() bool {
			future, err := engine.Query(ctx, &protocol.GetStatsRequest{MarketID: market1})
			if err != nil {
				return false
			}
			res, err := future.Wait(ctx)
			stats, ok := res.(*protocol.GetStatsResponse)
			return err == nil && ok && stats.BidOrderCount == 0
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
		cmdPlace2 := &protocol.Command{
			Type:      protocol.CmdPlaceOrder,
			UserID:    0,
			MarketID:  market,
			CommandID: "missing-market-o1",
			Timestamp: 1,
		}
		_ = cmdPlace2.SetPayload(&protocol.PlaceOrderParams{
			OrderID: "o1",
		})
		err := engine.SubmitAsync(ctx, cmdPlace2)
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

		cmdPlace3 := &protocol.Command{
			Type:      protocol.CmdPlaceOrder,
			UserID:    7,
			MarketID:  "NON-EXISTENT",
			CommandID: "missing-market-order-cmd",
			Timestamp: 123456789,
		}
		_ = cmdPlace3.SetPayload(&protocol.PlaceOrderParams{
			OrderID:   "missing-market-order",
			OrderType: Limit,
			Side:      Buy,
			Price:     "100",
			Size:      "1",
		})
		err := engine.SubmitAsync(ctx, cmdPlace3)
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

		futureDepth, err := engine.Query(
			ctx,
			&protocol.GetDepthRequest{MarketID: "NON-EXISTENT", Limit: 10},
		)
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

		cmd1 := &protocol.Command{
			Type:      protocol.CmdCreateMarket,
			UserID:    42,
			MarketID:  marketID,
			CommandID: "create-market-existing-1",
			Timestamp: 1001,
		}
		_ = cmd1.SetPayload(&protocol.CreateMarketParams{
			MinLotSize: "0.01",
		})
		future1, err := engine.Submit(ctx, cmd1)
		require.NoError(t, err)
		_, err = future1.Wait(ctx)
		require.NoError(t, err)

		cmd2 := &protocol.Command{
			Type:      protocol.CmdCreateMarket,
			UserID:    42,
			MarketID:  marketID,
			CommandID: "create-market-existing-2",
			Timestamp: 1002,
		}
		_ = cmd2.SetPayload(&protocol.CreateMarketParams{
			MinLotSize: "0.01",
		})
		future2, err := engine.Submit(ctx, cmd2)
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

		cmd := &protocol.Command{
			Type:      protocol.CmdCreateMarket,
			UserID:    77,
			MarketID:  marketID,
			CommandID: "create-market-zero-ts",
			Timestamp: 0,
		}
		_ = cmd.SetPayload(&protocol.CreateMarketParams{
			MinLotSize: "0.01",
		})
		future, err := engine.Submit(ctx, cmd)
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

	cmdMarket := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "prop-market-create",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmdMarket.SetPayload(&protocol.CreateMarketParams{
		MinLotSize: "",
	})
	future, err := engine.Submit(ctx, cmdMarket)
	require.NoError(t, err)

	go engine.Run()

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// Single submit: explicit CommandID should be propagated to emitted logs.
	cmdPlace := &protocol.Command{
		Type:      protocol.CmdPlaceOrder,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "single-oid-cmd",
		Timestamp: 1,
	}
	_ = cmdPlace.SetPayload(&protocol.PlaceOrderParams{
		OrderID:   "single-oid",
		OrderType: Limit,
		Side:      Buy,
		Price:     "100",
		Size:      "1",
	})
	err = engine.SubmitAsync(ctx, cmdPlace)
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
			cmd := &protocol.Command{
				Type:      protocol.CmdCreateMarket,
				UserID:    1,
				MarketID:  market,
				CommandID: "shutdown-market-" + market,
				Timestamp: time.Now().UnixNano(),
			}
			_ = cmd.SetPayload(&protocol.CreateMarketParams{
				MinLotSize: "",
			})
			future, err := engine.Submit(ctx, cmd)
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
				OrderID:   "order-" + market,
				OrderType: Limit,
				Side:      Buy,
				Price:     udecimal.MustFromInt64(int64(100+i*10), 0).String(),
				Size:      udecimal.MustFromInt64(1, 0).String(),
			}
			cmd := &protocol.Command{
				Type:      protocol.CmdPlaceOrder,
				UserID:    0,
				MarketID:  market,
				CommandID: "shutdown-order-" + market,
				Timestamp: int64(i + 1),
			}
			_ = cmd.SetPayload(order)
			err := engine.SubmitAsync(ctx, cmd)
			require.NoError(t, err)
		}

		// Shutdown should complete successfully
		err := engine.Shutdown(ctx)
		require.NoError(t, err)

		// After shutdown, adding orders should return ErrShutdown
		order := &protocol.PlaceOrderParams{
			OrderID:   "after-shutdown",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(1, 0).String(),
		}
		cmdAfter := &protocol.Command{
			Type:      protocol.CmdPlaceOrder,
			UserID:    0,
			MarketID:  markets[0],
			CommandID: "after-shutdown-order",
			Timestamp: 1,
		}
		_ = cmdAfter.SetPayload(order)
		err = engine.SubmitAsync(ctx, cmdAfter)
		assert.Equal(t, ErrShutdown, err)
	})
}

func TestManagement_SuspendResume(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine-"+t.Name(), publish)
	marketID := "ETH-USDT--" + "ShutdownMultipleMarkets" + t.Name()
	ctx := context.Background()

	cmdCreate := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "suspend-market-create",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmdCreate.SetPayload(&protocol.CreateMarketParams{
		MinLotSize: "0.0001",
	})
	future, err := engine.Submit(ctx, cmdCreate)
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// 1. Place Order (Should Succeed)
	order1 := &protocol.PlaceOrderParams{
		OrderID:   "order-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     "3000",
		Size:      "1",
	}
	cmdPlace1 := &protocol.Command{
		Type:      protocol.CmdPlaceOrder,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "suspend-order-1",
		Timestamp: 1,
	}
	_ = cmdPlace1.SetPayload(order1)
	err = engine.SubmitAsync(ctx, cmdPlace1)
	require.NoError(t, err)

	// Wait for Order-1
	assert.Eventually(t, func() bool {
		f, e := engine.Query(ctx, &protocol.GetStatsRequest{MarketID: marketID})
		if e != nil {
			return false
		}
		res, e := f.Wait(ctx)
		stats, ok := res.(*protocol.GetStatsResponse)
		return e == nil && ok && stats.BidOrderCount == 1
	}, 1*time.Second, 10*time.Millisecond)

	// 2. Suspend Market
	cmdSuspend := &protocol.Command{
		Type:      protocol.CmdSuspendMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "suspend-market-1",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmdSuspend.SetPayload(&protocol.SuspendMarketParams{})
	futureSuspend, err := engine.Submit(
		ctx,
		cmdSuspend,
	)
	require.NoError(t, err)
	_, err = futureSuspend.Wait(ctx)
	require.NoError(t, err)

	// 3. Place Order (Should be Rejected)
	order2 := &protocol.PlaceOrderParams{
		OrderID:   "order-2",
		Side:      Buy,
		OrderType: Limit,
		Price:     "3000",
		Size:      "1",
	}
	cmdPlace2 := &protocol.Command{
		Type:      protocol.CmdPlaceOrder,
		UserID:    2,
		MarketID:  marketID,
		CommandID: "suspend-order-2",
		Timestamp: 2,
	}
	_ = cmdPlace2.SetPayload(order2)
	err = engine.SubmitAsync(ctx, cmdPlace2)
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
	cmdResume := &protocol.Command{
		Type:      protocol.CmdResumeMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "resume-market-1",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmdResume.SetPayload(&protocol.ResumeMarketParams{})
	futureResume, err := engine.Submit(
		ctx,
		cmdResume,
	)
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

	cmd1 := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		UserID:    1,
		MarketID:  marketID1,
		CommandID: "cmd-1",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmd1.SetPayload(&protocol.CreateMarketParams{
		MinLotSize: "0.01",
	})
	future1, err := engine.Submit(context.Background(), cmd1)
	require.NoError(t, err)

	_, err = future1.Wait(ctxShort)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	go engine.Run()
	assert.Eventually(t, func() bool {
		f, e := engine.Query(context.Background(), &protocol.GetStatsRequest{MarketID: marketID1})
		if e != nil {
			return false
		}
		_, waitErr := f.Wait(context.Background())
		return waitErr == nil
	}, time.Second, 10*time.Millisecond)

	ctxLong := context.Background()
	cmd2 := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		UserID:    1,
		MarketID:  marketID2,
		CommandID: "cmd-2",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmd2.SetPayload(&protocol.CreateMarketParams{
		MinLotSize: "0.01",
	})
	future2, err := engine.Submit(ctxLong, cmd2)
	require.NoError(t, err)

	res, err := future2.Wait(ctxLong)
	require.NoError(t, err)
	val, ok := res.(bool)
	require.True(t, ok)
	require.True(t, val)

	_ = engine.Shutdown(ctxLong)
}

func TestManagement_UnknownMarketFuture(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("engine-1", publish)
	go func() {
		_ = engine.Run()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Try to suspend a non-existent market using the Submit API helper
	cmdUnknown := &protocol.Command{
		Type:      protocol.CmdSuspendMarket,
		UserID:    1,
		MarketID:  "UNKNOWN-MARKET",
		CommandID: "cmd-unknown",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmdUnknown.SetPayload(&protocol.SuspendMarketParams{
		Reason: "test",
	})
	future, err := engine.Submit(
		ctx,
		cmdUnknown,
	)
	require.NoError(t, err)

	// If fixed correctly, this should return an error immediately (ErrNotFound)
	// instead of waiting for the context timeout.
	start := time.Now()
	_, err = future.Wait(ctx)
	elapsed := time.Since(start)

	require.Error(t, err)
	// If elapsed time is close to 200ms, it means it hung until timeout
	require.Less(
		t,
		elapsed,
		100*time.Millisecond,
		"Future should return immediately for unknown market",
	)
	require.ErrorIs(t, err, ErrNotFound)

	_ = engine.Shutdown(ctx)
}

func TestMatchingEngine_ContextAwareSubmission(t *testing.T) {
	t.Run("EnqueueCommand_ContextCanceled", func(t *testing.T) {
		// Create engine but don't start it, so the ring buffer will fill up
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine", publishTrader)

		// Fill the ring buffer
		// defaultRingBufferSize is 32768.
		// We need to fill all 32768 slots.
		for i := range defaultRingBufferSize {
			err := engine.SubmitAsync(context.Background(), &protocol.Command{
				CommandID: fmt.Sprintf("fill-%d", i),
			})
			require.NoError(t, err)
		}

		// Now it should be full. Next EnqueueCommand should block.
		// We use a short timeout.
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// This call is expected to fail to compile initially once I change the signature,
		// OR I can call it as is and see it block forever (well, until the test times out).
		// But I want to test the NEW signature.

		// For TDD, I'll use the new signature here.
		err := engine.SubmitAsync(ctx, &protocol.Command{
			CommandID: "blocking-command",
			UserID:    0,
			MarketID:  "",
			Timestamp: 0,
		})

		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("CreateMarket_SubmissionTimeout", func(t *testing.T) {
		// Create engine but don't start it, so the ring buffer will fill up
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine", publishTrader)

		// Fill the ring buffer
		for i := range defaultRingBufferSize {
			err := engine.SubmitAsync(context.Background(), &protocol.Command{
				CommandID: fmt.Sprintf("fill-%d", i),
				UserID:    0,
				MarketID:  "",
				Timestamp: 0,
			})
			require.NoError(t, err)
		}

		// Now it should be full. Next submission should block.
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		cmd := &protocol.Command{
			Type:      protocol.CmdCreateMarket,
			UserID:    1,
			MarketID:  "BTC-USD",
			CommandID: "cmd-1",
			Timestamp: time.Now().UnixNano(),
		}
		_ = cmd.SetPayload(&protocol.CreateMarketParams{
			MinLotSize: "0.01",
		})
		_, err := engine.Submit(ctx, cmd)

		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

func TestManagement_UpdateConfig_MalformedPayload(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine", publish)
	marketID := "MALFORMED-TEST"
	ctx := context.Background()

	// 1. Create market first
	cmd := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "config-market-create",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmd.SetPayload(&protocol.CreateMarketParams{
		MinLotSize: "1.0",
	})
	future, err := engine.Submit(ctx, cmd)
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()
	defer engine.Shutdown(ctx)

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// 2. Send malformed UpdateConfig
	// We manually construct a command with a malformed payload
	protoCmd := &protocol.Command{
		Type:      protocol.CmdUpdateConfig,
		MarketID:  marketID,
		CommandID: "malformed-config",
		Payload:   []byte("this-is-not-json-or-binary"),
	}

	futureCmd, err := engine.Submit(ctx, protoCmd)
	require.NoError(t, err)

	// 3. Wait for response - should not hang and should return an error
	_, err = futureCmd.Wait(ctx)
	require.Error(t, err, "Should return error for malformed payload")
}

func TestManagement_SuspendMarket_MalformedPayload(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine", publish)
	marketID := "MALFORMED-SUSPEND"
	ctx := context.Background()

	// 1. Create market first
	cmd := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "config-market-create",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmd.SetPayload(&protocol.CreateMarketParams{
		MinLotSize: "1.0",
	})
	future, err := engine.Submit(ctx, cmd)
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()
	defer engine.Shutdown(ctx)

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// 2. Send malformed SuspendMarket
	protoCmd := &protocol.Command{
		Type:      protocol.CmdSuspendMarket,
		MarketID:  marketID,
		CommandID: "malformed-suspend",
		Payload:   []byte("this-is-not-json-or-binary"),
	}

	futureCmd, err := engine.Submit(ctx, protoCmd)
	require.NoError(t, err)

	// 3. Wait for response - should not hang
	_, err = futureCmd.Wait(ctx)
	require.Error(t, err)
}

func TestManagement_ResumeMarket_MalformedPayload(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("test-engine", publish)
	marketID := "MALFORMED-RESUME"
	ctx := context.Background()

	// 1. Create market first
	cmd := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "config-market-create",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmd.SetPayload(&protocol.CreateMarketParams{
		MinLotSize: "1.0",
	})
	future, err := engine.Submit(ctx, cmd)
	require.NoError(t, err)

	// Start engine event loop
	go engine.Run()
	defer engine.Shutdown(ctx)

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// 2. Send malformed ResumeMarket
	protoCmd := &protocol.Command{
		Type:      protocol.CmdResumeMarket,
		MarketID:  marketID,
		CommandID: "malformed-resume",
		Payload:   []byte("this-is-not-json-or-binary"),
	}

	futureCmd, err := engine.Submit(ctx, protoCmd)
	require.NoError(t, err)

	// 3. Wait for response - should not hang
	_, err = futureCmd.Wait(ctx)
	require.Error(t, err)
}

func TestUserEvent_GenericPayload(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("event-test-engine", publish)
	marketID := "EVENT-TEST"
	ctx := context.Background()

	cmdMarket := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "event-market-create",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmdMarket.SetPayload(&protocol.CreateMarketParams{
		MinLotSize: "1.0",
	})
	future, err := engine.Submit(ctx, cmdMarket)
	require.NoError(t, err)

	go engine.Run()

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// 1. Place Order
	cmdOrder1 := &protocol.Command{
		Type:      protocol.CmdPlaceOrder,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "event-order-1",
		Timestamp: 1,
	}
	_ = cmdOrder1.SetPayload(&protocol.PlaceOrderParams{
		OrderID:   "order-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     "100",
		Size:      "1",
	})
	err = engine.SubmitAsync(ctx, cmdOrder1)
	require.NoError(t, err)

	// 2. Send User Event (e.g. EndOfBlock)
	eventData := []byte("block-hash-0x123456")
	cmdUser1 := &protocol.Command{
		Type:      protocol.CmdUserEvent,
		UserID:    999,
		MarketID:  "",
		CommandID: "event-user-1",
		Timestamp: 123456789,
	}
	_ = cmdUser1.SetPayload(&protocol.UserEventParams{
		EventType: "EndOfBlock",
		Key:       "blk-1",
		Data:      eventData,
	})
	err = engine.SubmitAsync(ctx, cmdUser1)
	require.NoError(t, err)

	// 3. Place Another Order
	cmdOrder2 := &protocol.Command{
		Type:      protocol.CmdPlaceOrder,
		UserID:    2,
		MarketID:  marketID,
		CommandID: "event-order-2",
		Timestamp: 2,
	}
	_ = cmdOrder2.SetPayload(&protocol.PlaceOrderParams{
		OrderID:   "order-2",
		Side:      Buy,
		OrderType: Limit,
		Price:     "101",
		Size:      "1",
	})
	err = engine.SubmitAsync(ctx, cmdOrder2)
	require.NoError(t, err)

	// 4. Verify Log Order: Order-1 -> UserEvent -> Order-2
	assert.Eventually(t, func() bool {
		logs := publish.Logs()
		if len(logs) < 3 {
			return false
		}

		// Find indices
		idx1, idxEvent, idx2 := -1, -1, -1
		for i, l := range logs {
			switch {
			case l.OrderID == "order-1":
				idx1 = i
			case l.Type == protocol.LogTypeUser && l.EventType == "EndOfBlock":
				idxEvent = i
				// Verify Payload
				if string(l.Data) != "block-hash-0x123456" {
					return false
				}
				if l.UserID != 999 {
					return false
				}
				if l.Timestamp != 123456789 {
					return false
				}
			case l.OrderID == "order-2":
				idx2 = i
			default:
				// other logs
			}
		}

		return idx1 != -1 && idxEvent != -1 && idx2 != -1 &&
			idx1 < idxEvent && idxEvent < idx2
	}, 1*time.Second, 10*time.Millisecond)

	_ = engine.Shutdown(ctx)
}

func TestUserEvent_InvalidPayloadEmitsReject(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("event-test-engine", publish)
	ctx := context.Background()

	go engine.Run()

	err := engine.SubmitAsync(ctx, &protocol.Command{
		Type:      protocol.CmdUserEvent,
		CommandID: "bad-user-event",
		UserID:    0,
		MarketID:  "",
		Timestamp: 0,
		Payload:   []byte("{"),
	})
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		logs := publish.Logs()
		if len(logs) != 1 {
			return false
		}
		log := logs[0]
		return log.Type == protocol.LogTypeReject &&
			log.CommandID == "bad-user-event" &&
			log.RejectReason == protocol.RejectReasonInvalidPayload
	}, time.Second, 10*time.Millisecond)

	_ = engine.Shutdown(ctx)
}

func TestUserEvent_RequiresPositiveTimestamp(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("event-test-engine", publish)
	ctx := context.Background()

	go engine.Run()

	cmdUser := &protocol.Command{
		Type:      protocol.CmdUserEvent,
		UserID:    999,
		MarketID:  "",
		CommandID: "event-user-bad-ts",
		Timestamp: 0,
	}
	_ = cmdUser.SetPayload(&protocol.UserEventParams{
		EventType: "EndOfBlock",
		Key:       "blk-0",
		Data:      []byte("x"),
	})
	err := engine.SubmitAsync(ctx, cmdUser)
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		logs := publish.Logs()
		if len(logs) != 1 {
			return false
		}
		log := logs[0]
		return log.Type == protocol.LogTypeReject &&
			log.OrderID == "blk-0" &&
			log.UserID == 999 &&
			log.RejectReason == protocol.RejectReasonInvalidPayload &&
			log.Timestamp == 0
	}, time.Second, 10*time.Millisecond)

	_ = engine.Shutdown(ctx)
}

func TestUserEvent_RequiresCommandID(t *testing.T) {
	engine := NewMatchingEngine("event-test-engine", NewMemoryPublishLog())
	cmd := &protocol.Command{
		Type:      protocol.CmdUserEvent,
		UserID:    999,
		MarketID:  "",
		CommandID: "",
		Timestamp: 1,
	}
	_ = cmd.SetPayload(&protocol.UserEventParams{
		EventType: "EndOfBlock",
		Key:       "blk-0",
		Data:      []byte("x"),
	})
	err := engine.SubmitAsync(context.Background(), cmd)
	require.ErrorIs(t, err, ErrInvalidParam)
}
