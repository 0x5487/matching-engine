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

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// setupEngine creates a new MatchingEngine with the given publisher, starts it
// in a background goroutine, and registers a cleanup that shuts it down when the
// test finishes.
func setupEngine(t *testing.T, log Publisher) *MatchingEngine {
	t.Helper()
	engine := NewMatchingEngine("engine-"+t.Name(), log)
	go engine.Run()
	t.Cleanup(func() { _ = engine.Shutdown(context.Background()) })
	return engine
}

// createMarket submits a CmdCreateMarket command to the engine and waits for it
// to complete, failing the test immediately if any step returns an error.
func createMarket(t *testing.T, engine *MatchingEngine, marketID, minLotSize string) {
	t.Helper()
	ctx := context.Background()
	cmd := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "setup-create-" + marketID,
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmd.SetPayload(&protocol.CreateMarketParams{MinLotSize: minLotSize})
	future, err := engine.Submit(ctx, cmd)
	require.NoError(t, err)
	_, err = future.Wait(ctx)
	require.NoError(t, err)
}

// waitForBidCount polls GetStats until the bid order count for the given market
// equals the expected value, or until the 1-second deadline is reached.
func waitForBidCount(t *testing.T, engine *MatchingEngine, marketID string, count int64) {
	t.Helper()
	assert.Eventually(t, func() bool {
		f, e := engine.Query(context.Background(), &protocol.GetStatsRequest{MarketID: marketID})
		if e != nil {
			return false
		}
		res, e := f.Wait(context.Background())
		stats, ok := res.(*protocol.GetStatsResponse)
		return e == nil && ok && stats.BidOrderCount == count
	}, time.Second, 10*time.Millisecond)
}

// waitForAskCount polls GetStats until the ask order count for the given market
// equals the expected value, or until the 1-second deadline is reached.
func waitForAskCount(t *testing.T, engine *MatchingEngine, marketID string, count int64) {
	t.Helper()
	assert.Eventually(t, func() bool {
		f, e := engine.Query(context.Background(), &protocol.GetStatsRequest{MarketID: marketID})
		if e != nil {
			return false
		}
		res, e := f.Wait(context.Background())
		stats, ok := res.(*protocol.GetStatsResponse)
		return e == nil && ok && stats.AskOrderCount == count
	}, time.Second, 10*time.Millisecond)
}

// fillRingBuffer saturates the engine's ring buffer by enqueuing placeholder
// commands without starting the engine event loop.
func fillRingBuffer(t *testing.T, engine *MatchingEngine) {
	t.Helper()
	for i := range defaultRingBufferSize {
		err := engine.SubmitAsync(context.Background(), &protocol.Command{
			CommandID: fmt.Sprintf("fill-%d", i),
		})
		require.NoError(t, err)
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

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
		engine := setupEngine(t, publishTrader)

		ctx := context.Background()
		market1 := "BTC-USDT-" + t.Name() + "-1"
		market2 := "ETH-USDT-" + t.Name() + "-2"
		createMarket(t, engine, market1, "")
		createMarket(t, engine, market2, "")

		cmdOrder1 := &protocol.Command{
			Type:      protocol.CmdPlaceOrder,
			UserID:    0,
			MarketID:  market1,
			CommandID: "place-orders-order-1",
			Timestamp: 1,
		}
		_ = cmdOrder1.SetPayload(&protocol.PlaceOrderParams{
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
		})
		err := engine.SubmitAsync(ctx, cmdOrder1)
		require.NoError(t, err)
		waitForBidCount(t, engine, market1, 1)

		cmdOrder2 := &protocol.Command{
			Type:      protocol.CmdPlaceOrder,
			UserID:    0,
			MarketID:  market2,
			CommandID: "place-orders-order-2",
			Timestamp: 2,
		}
		_ = cmdOrder2.SetPayload(&protocol.PlaceOrderParams{
			OrderID:   "order2",
			OrderType: Limit,
			Side:      Sell,
			Price:     udecimal.MustFromInt64(110, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
		})
		err = engine.SubmitAsync(ctx, cmdOrder2)
		require.NoError(t, err)
		waitForAskCount(t, engine, market2, 1)
	})

	t.Run("CancelOrder", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := setupEngine(t, publishTrader)

		ctx := context.Background()
		market1 := "BTC-USDT-" + t.Name()
		createMarket(t, engine, market1, "")

		cmdPlace1 := &protocol.Command{
			Type:      protocol.CmdPlaceOrder,
			UserID:    1,
			MarketID:  market1,
			CommandID: "cancel-order-1",
			Timestamp: 1,
		}
		_ = cmdPlace1.SetPayload(&protocol.PlaceOrderParams{
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(2, 0).String(),
		})
		err := engine.SubmitAsync(ctx, cmdPlace1)
		require.NoError(t, err)
		waitForBidCount(t, engine, market1, 1)

		cmdCancel1 := &protocol.Command{
			Type:      protocol.CmdCancelOrder,
			UserID:    1,
			MarketID:  market1,
			CommandID: "cancel-order-1-cancel",
			Timestamp: 2,
		}
		_ = cmdCancel1.SetPayload(&protocol.CancelOrderParams{OrderID: "order1"})
		err = engine.SubmitAsync(ctx, cmdCancel1)
		require.NoError(t, err)
		waitForBidCount(t, engine, market1, 0)
	})

	// MarketNotFound covers two aspects of an order submitted to a non-existent
	// market: the enqueue itself is accepted (ring buffer is market-agnostic) and
	// the engine emits a reject log with the correct fields.
	t.Run("MarketNotFound", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := setupEngine(t, publishTrader)

		market := "NON-EXISTENT"
		ctx := context.Background()

		cmdPlace := &protocol.Command{
			Type:      protocol.CmdPlaceOrder,
			UserID:    7,
			MarketID:  market,
			CommandID: "missing-market-order-cmd",
			Timestamp: 123456789,
		}
		_ = cmdPlace.SetPayload(&protocol.PlaceOrderParams{
			OrderID:   "missing-market-order",
			OrderType: Limit,
			Side:      Buy,
			Price:     "100",
			Size:      "1",
		})
		err := engine.SubmitAsync(ctx, cmdPlace)
		require.NoError(t, err) // Enqueue succeeds; market check is on the consumer side.

		// The order book entry must not exist.
		assert.Nil(t, engine.orderbooks[market])

		// The engine must emit a reject log with the correct fields.
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
	})

	t.Run("QueryMissingMarketReturnsNotFound", func(t *testing.T) {
		engine := setupEngine(t, NewMemoryPublishLog())
		ctx := context.Background()

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
	})

	t.Run("CreateMarketRejectIncludesManagementUserID", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := setupEngine(t, publishTrader)
		ctx := context.Background()
		marketID := "BTC-USDT--" + "CreateMarketRejectIncludesManagementUserID" + t.Name()

		// First creation must succeed.
		cmd1 := &protocol.Command{
			Type:      protocol.CmdCreateMarket,
			UserID:    42,
			MarketID:  marketID,
			CommandID: "create-market-existing-1",
			Timestamp: 1001,
		}
		_ = cmd1.SetPayload(&protocol.CreateMarketParams{MinLotSize: "0.01"})
		future1, err := engine.Submit(ctx, cmd1)
		require.NoError(t, err)
		_, err = future1.Wait(ctx)
		require.NoError(t, err)

		// Second creation on the same market ID must fail and emit a reject log
		// that carries the original UserID and Timestamp.
		cmd2 := &protocol.Command{
			Type:      protocol.CmdCreateMarket,
			UserID:    42,
			MarketID:  marketID,
			CommandID: "create-market-existing-2",
			Timestamp: 1002,
		}
		_ = cmd2.SetPayload(&protocol.CreateMarketParams{MinLotSize: "0.01"})
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
	})

	t.Run("CreateMarketRequiresPositiveTimestamp", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := setupEngine(t, publishTrader)
		ctx := context.Background()
		marketID := "BTC-USDT--" + "CreateMarketRequiresPositiveTimestamp" + t.Name()

		cmd := &protocol.Command{
			Type:      protocol.CmdCreateMarket,
			UserID:    77,
			MarketID:  marketID,
			CommandID: "create-market-zero-ts",
			Timestamp: 0,
		}
		_ = cmd.SetPayload(&protocol.CreateMarketParams{MinLotSize: "0.01"})
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
	})
}

func TestCommandAndEngineIDPropagation(t *testing.T) {
	ctx := context.Background()
	publishTrader := NewMemoryPublishLog()

	// Use a fixed engine ID so the assertion below can reference testEngineID.
	engine := NewMatchingEngine(testEngineID, publishTrader)
	marketID := "BTC-USDT--" + "CreateMarketRequiresPositiveTimestamp" + t.Name()

	cmdMarket := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "prop-market-create",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmdMarket.SetPayload(&protocol.CreateMarketParams{MinLotSize: ""})
	future, err := engine.Submit(ctx, cmdMarket)
	require.NoError(t, err)

	go engine.Run()
	t.Cleanup(func() { _ = engine.Shutdown(ctx) })

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// Explicit CommandID must be propagated to emitted logs.
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
}

func TestMatchingEngineShutdown(t *testing.T) {
	t.Run("ShutdownMultipleMarkets", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := NewMatchingEngine("test-engine-"+t.Name(), publishTrader)

		ctx := context.Background()

		// Submit market-creation commands before starting the engine so that
		// all three commands are enqueued together.
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
			_ = cmd.SetPayload(&protocol.CreateMarketParams{MinLotSize: ""})
			future, err := engine.Submit(ctx, cmd)
			require.NoError(t, err)
			futures = append(futures, future)
		}

		go engine.Run()

		for _, future := range futures {
			_, err := future.Wait(ctx)
			require.NoError(t, err)
		}

		for i, market := range markets {
			cmd := &protocol.Command{
				Type:      protocol.CmdPlaceOrder,
				UserID:    0,
				MarketID:  market,
				CommandID: "shutdown-order-" + market,
				Timestamp: int64(i + 1),
			}
			_ = cmd.SetPayload(&protocol.PlaceOrderParams{
				OrderID:   "order-" + market,
				OrderType: Limit,
				Side:      Buy,
				Price:     udecimal.MustFromInt64(int64(100+i*10), 0).String(),
				Size:      udecimal.MustFromInt64(1, 0).String(),
			})
			err := engine.SubmitAsync(ctx, cmd)
			require.NoError(t, err)
		}

		// Shutdown should complete successfully.
		err := engine.Shutdown(ctx)
		require.NoError(t, err)

		// After shutdown, further submissions must return ErrShutdown.
		cmdAfter := &protocol.Command{
			Type:      protocol.CmdPlaceOrder,
			UserID:    0,
			MarketID:  markets[0],
			CommandID: "after-shutdown-order",
			Timestamp: 1,
		}
		_ = cmdAfter.SetPayload(&protocol.PlaceOrderParams{
			OrderID:   "after-shutdown",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0).String(),
			Size:      udecimal.MustFromInt64(1, 0).String(),
		})
		err = engine.SubmitAsync(ctx, cmdAfter)
		assert.Equal(t, ErrShutdown, err)
	})
}

func TestManagement_SuspendResume(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := setupEngine(t, publish)
	marketID := "ETH-USDT-" + t.Name()
	ctx := context.Background()

	createMarket(t, engine, marketID, "0.0001")

	// 1. Place Order (Should Succeed)
	cmdPlace1 := &protocol.Command{
		Type:      protocol.CmdPlaceOrder,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "suspend-order-1",
		Timestamp: 1,
	}
	_ = cmdPlace1.SetPayload(&protocol.PlaceOrderParams{
		OrderID:   "order-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     "3000",
		Size:      "1",
	})
	err := engine.SubmitAsync(ctx, cmdPlace1)
	require.NoError(t, err)
	waitForBidCount(t, engine, marketID, 1)

	// 2. Suspend Market
	cmdSuspend := &protocol.Command{
		Type:      protocol.CmdSuspendMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "suspend-market-1",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmdSuspend.SetPayload(&protocol.SuspendMarketParams{})
	futureSuspend, err := engine.Submit(ctx, cmdSuspend)
	require.NoError(t, err)
	_, err = futureSuspend.Wait(ctx)
	require.NoError(t, err)

	// 3. Place Order (Should be Rejected)
	cmdPlace2 := &protocol.Command{
		Type:      protocol.CmdPlaceOrder,
		UserID:    2,
		MarketID:  marketID,
		CommandID: "suspend-order-2",
		Timestamp: 2,
	}
	_ = cmdPlace2.SetPayload(&protocol.PlaceOrderParams{
		OrderID:   "order-2",
		Side:      Buy,
		OrderType: Limit,
		Price:     "3000",
		Size:      "1",
	})
	err = engine.SubmitAsync(ctx, cmdPlace2)
	require.NoError(t, err)

	// Verify the reject log for the suspended market.
	assert.Eventually(t, func() bool {
		for _, l := range publish.Logs() {
			if l.OrderID == "order-2" &&
				l.Type == protocol.LogTypeReject &&
				l.RejectReason == protocol.RejectReasonMarketSuspended {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)

	// 4. Resume Market
	cmdResume := &protocol.Command{
		Type:      protocol.CmdResumeMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "resume-market-1",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmdResume.SetPayload(&protocol.ResumeMarketParams{})
	futureResume, err := engine.Submit(ctx, cmdResume)
	require.NoError(t, err)
	res, err := futureResume.Wait(ctx)
	require.NoError(t, err)
	val, ok := res.(bool)
	require.True(t, ok)
	require.True(t, val)
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
	engine := setupEngine(t, NewMemoryPublishLog())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Try to suspend a non-existent market using the Submit API.
	cmdUnknown := &protocol.Command{
		Type:      protocol.CmdSuspendMarket,
		UserID:    1,
		MarketID:  "UNKNOWN-MARKET",
		CommandID: "cmd-unknown",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmdUnknown.SetPayload(&protocol.SuspendMarketParams{Reason: "test"})
	future, err := engine.Submit(ctx, cmdUnknown)
	require.NoError(t, err)

	// If fixed correctly, this must return ErrNotFound immediately (well under
	// the 200 ms deadline) instead of hanging until context timeout.
	start := time.Now()
	_, err = future.Wait(ctx)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Less(t, elapsed, 100*time.Millisecond,
		"Future should return immediately for unknown market")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestMatchingEngine_ContextAwareSubmission(t *testing.T) {
	t.Run("EnqueueCommand_ContextCanceled", func(t *testing.T) {
		// Create engine but don't start it, so the ring buffer will fill up.
		engine := NewMatchingEngine("test-engine", NewMemoryPublishLog())
		fillRingBuffer(t, engine)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		err := engine.SubmitAsync(ctx, &protocol.Command{
			CommandID: "blocking-command",
		})
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("CreateMarket_SubmissionTimeout", func(t *testing.T) {
		// Create engine but don't start it, so the ring buffer will fill up.
		engine := NewMatchingEngine("test-engine", NewMemoryPublishLog())
		fillRingBuffer(t, engine)

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		cmd := &protocol.Command{
			Type:      protocol.CmdCreateMarket,
			UserID:    1,
			MarketID:  "BTC-USD",
			CommandID: "cmd-1",
			Timestamp: time.Now().UnixNano(),
		}
		_ = cmd.SetPayload(&protocol.CreateMarketParams{MinLotSize: "0.01"})
		_, err := engine.Submit(ctx, cmd)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

// TestManagement_MalformedPayload verifies that malformed payloads for management
// commands (UpdateConfig, SuspendMarket, ResumeMarket) return an immediate error
// rather than hanging.
func TestManagement_MalformedPayload(t *testing.T) {
	cases := []struct {
		name    string
		cmdType protocol.CommandType
		cmdID   string
	}{
		{"UpdateConfig", protocol.CmdUpdateConfig, "malformed-config"},
		{"SuspendMarket", protocol.CmdSuspendMarket, "malformed-suspend"},
		{"ResumeMarket", protocol.CmdResumeMarket, "malformed-resume"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			publish := NewMemoryPublishLog()
			engine := setupEngine(t, publish)
			marketID := "MALFORMED-" + tc.name
			createMarket(t, engine, marketID, "1.0")

			protoCmd := &protocol.Command{
				Type:      tc.cmdType,
				MarketID:  marketID,
				CommandID: tc.cmdID,
				Payload:   []byte("this-is-not-json-or-binary"),
			}
			futureCmd, err := engine.Submit(context.Background(), protoCmd)
			require.NoError(t, err)

			_, err = futureCmd.Wait(context.Background())
			require.Error(t, err, "should return error for malformed payload")
		})
	}
}

func TestUserEvent_GenericPayload(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := setupEngine(t, publish)
	marketID := "EVENT-TEST-" + t.Name()
	ctx := context.Background()

	createMarket(t, engine, marketID, "1.0")

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
	err := engine.SubmitAsync(ctx, cmdOrder1)
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
				// other logs – no action needed
			}
		}

		return idx1 != -1 && idxEvent != -1 && idx2 != -1 &&
			idx1 < idxEvent && idxEvent < idx2
	}, time.Second, 10*time.Millisecond)
}

// TestUserEvent_ValidationErrors groups all UserEvent input-validation failure
// scenarios into a single test function with named sub-tests.
func TestUserEvent_ValidationErrors(t *testing.T) {
	t.Run("InvalidPayloadEmitsReject", func(t *testing.T) {
		publish := NewMemoryPublishLog()
		engine := setupEngine(t, publish)
		ctx := context.Background()

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
	})

	t.Run("RequiresPositiveTimestamp", func(t *testing.T) {
		publish := NewMemoryPublishLog()
		engine := setupEngine(t, publish)
		ctx := context.Background()

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
	})

	t.Run("RequiresCommandID", func(t *testing.T) {
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
	})
}
