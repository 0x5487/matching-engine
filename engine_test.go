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
	var mls udecimal.Decimal
	if minLotSize != "" {
		mls, _ = udecimal.Parse(minLotSize)
	}
	req := &protocol.CreateMarketRequest{
		BaseCommand: protocol.BaseCommand{
			UserID:    1,
			MarketID:  marketID,
			CommandID: "setup-create-" + marketID,
			Timestamp: time.Now().UnixNano(),
		},
		MinLotSize: mls,
	}
	future, err := engine.CreateMarket(ctx, req)
	require.NoError(t, err)
	_, err = future.Wait(ctx)
	require.NoError(t, err)
}

// waitForBidCount polls GetStats until the bid order count for the given market
// equals the expected value, or until the 1-second deadline is reached.
func waitForBidCount(t *testing.T, engine *MatchingEngine, marketID string, count int64) {
	t.Helper()
	assert.Eventually(t, func() bool {
		f, e := engine.GetStats(context.Background(), &protocol.GetStatsQuery{
			BaseQuery: protocol.BaseQuery{MarketID: marketID},
		})
		if e != nil {
			return false
		}
		res, e := f.Wait(context.Background())
		stats := res
		return e == nil && stats != nil && stats.BidOrderCount == count
	}, time.Second, 10*time.Millisecond)
}

// waitForAskCount polls GetStats until the ask order count for the given market
// equals the expected value, or until the 1-second deadline is reached.
func waitForAskCount(t *testing.T, engine *MatchingEngine, marketID string, count int64) {
	t.Helper()
	assert.Eventually(t, func() bool {
		f, e := engine.GetStats(context.Background(), &protocol.GetStatsQuery{
			BaseQuery: protocol.BaseQuery{MarketID: marketID},
		})
		if e != nil {
			return false
		}
		res, e := f.Wait(context.Background())
		stats := res
		return e == nil && stats != nil && stats.AskOrderCount == count
	}, time.Second, 10*time.Millisecond)
}

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
		req := &protocol.CreateMarketRequest{
			BaseCommand: protocol.BaseCommand{
				UserID:    1,
				MarketID:  marketID,
				CommandID: "",
				Timestamp: 1,
			},
			MinLotSize: udecimal.MustFromInt64(1, 2),
		}
		_, err := engine.CreateMarket(context.Background(), req)
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

		reqOrder1 := &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				UserID:    1,
				MarketID:  market1,
				CommandID: "cmd-1",
				Timestamp: 1,
			},
			OrderID:   "order1",
			Side:      Buy,
			OrderType: Limit,
			Price:     udecimal.MustFromInt64(100, 0),
			Size:      udecimal.MustFromInt64(2, 0),
		}
		err := engine.PlaceOrderAsync(ctx, reqOrder1)

		require.NoError(t, err)
		waitForBidCount(t, engine, market1, 1)

		reqOrder2 := &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				UserID:    2,
				MarketID:  market2,
				CommandID: "cmd-2",
				Timestamp: 2,
			},
			OrderID:   "order2",
			OrderType: Limit,
			Side:      Sell,
			Price:     udecimal.MustFromInt64(110, 0),
			Size:      udecimal.MustFromInt64(2, 0),
		}
		err = engine.PlaceOrderAsync(ctx, reqOrder2)

		require.NoError(t, err)
		waitForAskCount(t, engine, market2, 1)
	})

	t.Run("CancelOrder", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := setupEngine(t, publishTrader)

		ctx := context.Background()
		market1 := "BTC-USDT-" + t.Name()
		createMarket(t, engine, market1, "")

		reqPlace1 := &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				UserID:    1,
				MarketID:  market1,
				CommandID: "cancel-order-1",
				Timestamp: 1,
			},
			OrderID:   "order1",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0),
			Size:      udecimal.MustFromInt64(2, 0),
		}
		err := engine.PlaceOrderAsync(ctx, reqPlace1)
		require.NoError(t, err)
		waitForBidCount(t, engine, market1, 1)

		reqCancel1 := &protocol.CancelOrderRequest{
			BaseCommand: protocol.BaseCommand{
				UserID:    1,
				MarketID:  market1,
				CommandID: "cancel-order-1-cancel",
				Timestamp: 2,
			},
			OrderID: "order1",
		}
		err = engine.CancelOrderAsync(ctx, reqCancel1)
		require.NoError(t, err)
		waitForBidCount(t, engine, market1, 0)
	})

	t.Run("MarketNotFound", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := setupEngine(t, publishTrader)

		market := "NON-EXISTENT"
		ctx := context.Background()

		reqPlace := &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				UserID:    7,
				MarketID:  market,
				CommandID: "missing-market-order-cmd",
				Timestamp: 123456789,
			},
			OrderID:   "missing-market-order",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0),
			Size:      udecimal.MustFromInt64(1, 0),
		}
		err := engine.PlaceOrderAsync(ctx, reqPlace)
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
	})
}

func TestMatchingEngineShutdown(t *testing.T) {
	t.Run("ShutdownMultipleMarkets", func(t *testing.T) {
		publishTrader := NewMemoryPublishLog()
		engine := setupEngine(t, publishTrader)

		ctx := context.Background()

		markets := []string{"BTC-USDT-1", "ETH-USDT-2"}
		for _, market := range markets {
			createMarket(t, engine, market, "")
		}

		err := engine.Shutdown(ctx)
		require.NoError(t, err)

		reqAfter := &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				UserID:    1,
				MarketID:  "BTC-USDT-1",
				CommandID: "after-shutdown",
				Timestamp: time.Now().UnixNano(),
			},
			OrderID:   "after-shutdown",
			OrderType: Limit,
			Side:      Buy,
			Price:     udecimal.MustFromInt64(100, 0),
			Size:      udecimal.MustFromInt64(1, 0),
		}
		err = engine.PlaceOrderAsync(ctx, reqAfter)
		assert.Equal(t, ErrShutdown, err)
	})
}

func TestManagement_SuspendResume(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := setupEngine(t, publish)
	marketID := "ETH-USDT-" + t.Name()
	ctx := context.Background()

	createMarket(t, engine, marketID, "0.0001")

	reqPlace1 := &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			UserID:    1,
			MarketID:  marketID,
			CommandID: "suspend-order-1",
			Timestamp: 1,
		},
		OrderID:   "order-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(3000, 0),
		Size:      udecimal.MustFromInt64(1, 0),
	}
	err := engine.PlaceOrderAsync(ctx, reqPlace1)
	require.NoError(t, err)
	waitForBidCount(t, engine, marketID, 1)

	reqSuspend := &protocol.SuspendMarketRequest{
		BaseCommand: protocol.BaseCommand{
			UserID:    1,
			MarketID:  marketID,
			CommandID: "suspend-market-1",
			Timestamp: time.Now().UnixNano(),
		},
	}
	futureSuspend, _ := engine.SuspendMarket(ctx, reqSuspend)
	_, _ = futureSuspend.Wait(ctx)

	reqPlace2 := &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			UserID:    2,
			MarketID:  marketID,
			CommandID: "suspend-order-2",
			Timestamp: 2,
		},
		OrderID:   "order-2",
		Side:      Buy,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(3000, 0),
		Size:      udecimal.MustFromInt64(1, 0),
	}
	_ = engine.PlaceOrderAsync(ctx, reqPlace2)

	assert.Eventually(t, func() bool {
		for _, l := range publish.Logs() {
			if l.OrderID == "order-2" && l.RejectReason == protocol.RejectReasonMarketSuspended {
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)

	reqResume := &protocol.ResumeMarketRequest{
		BaseCommand: protocol.BaseCommand{
			UserID:    1,
			MarketID:  marketID,
			CommandID: "resume-market-1",
			Timestamp: time.Now().UnixNano(),
		},
	}
	futureResume, _ := engine.ResumeMarket(ctx, reqResume)
	_, _ = futureResume.Wait(ctx)
}

func TestUserEvent_GenericPayload(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := setupEngine(t, publish)
	market1 := "EVENT-TEST-" + t.Name()
	ctx := context.Background()

	createMarket(t, engine, market1, "1.0")

	reqOrder1 := &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			UserID:    1,
			MarketID:  market1,
			CommandID: "cmd-1",
			Timestamp: 1,
		},
		OrderID:   "order-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(100, 0),
		Size:      udecimal.MustFromInt64(2, 0),
	}
	_ = engine.PlaceOrderAsync(ctx, reqOrder1)

	reqUser1 := &protocol.UserEventRequest{
		BaseCommand: protocol.BaseCommand{
			UserID:    99,
			MarketID:  market1,
			CommandID: "user-event-1",
			Timestamp: 2,
		},
		EventType: "EndOfBlock",
		Key:       "block-123",
		Data:      []byte("block-hash"),
	}
	_ = engine.SendUserEvent(ctx, reqUser1)

	reqOrder2 := &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			UserID:    1,
			MarketID:  market1,
			CommandID: "cmd-2",
			Timestamp: 3,
		},
		OrderID:   "order-2",
		Side:      Buy,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(100, 0),
		Size:      udecimal.MustFromInt64(2, 0),
	}
	_ = engine.PlaceOrderAsync(ctx, reqOrder2)

	assert.Eventually(t, func() bool {
		logs := publish.Logs()
		return len(logs) >= 3
	}, time.Second, 10*time.Millisecond)
}

func TestEngine_UnknownCommand(t *testing.T) {
	engine := setupEngine(t, NewMemoryPublishLog())
	ctx := context.Background()
	req := struct{}{}
	err := engine.validateTypedRequest(ctx, req)
	require.ErrorIs(t, err, ErrInvalidParam)
}
