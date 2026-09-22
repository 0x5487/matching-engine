package match_test

import (
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	match "github.com/0x5487/matching-engine"
	"github.com/0x5487/matching-engine/protocol"
)

func TestOrderBook_SyncAPI(t *testing.T) {
	t.Run("NewOrderBook and Options", func(t *testing.T) {
		lotSize := udecimal.MustFromInt64(1, 4) // 0.0001
		book := match.NewOrderBook(
			"BTC-USDT",
			match.WithEngineID("custom-engine"),
			match.WithLotSize(lotSize),
			match.WithSkiplistSeed(42),
		)

		assert.Equal(t, "BTC-USDT", book.MarketID())
		assert.Equal(t, "custom-engine", book.EngineID())
		assert.Equal(t, protocol.OrderBookStateRunning, book.State())

		book.SetState(protocol.OrderBookStateSuspended)
		assert.Equal(t, protocol.OrderBookStateSuspended, book.State())
		book.SetState(protocol.OrderBookStateRunning)
	})

	t.Run("PlaceOrder Limit and Match", func(t *testing.T) {
		book := match.NewOrderBook("BTC-USDT")

		// 1. Place Sell Maker order at 50,000 for 1 BTC
		sellReq := &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				MarketID:  "BTC-USDT",
				CommandID: "cmd-sell-1",
				UserID:    1001,
				Timestamp: 1000000,
			},
			OrderID:   "sell-order-1",
			Side:      protocol.SideSell,
			OrderType: protocol.OrderTypeLimit,
			Price:     udecimal.MustFromInt64(50000, 0),
			Size:      udecimal.MustFromInt64(1, 0),
		}

		batch, err := book.PlaceOrder(sellReq)
		require.NoError(t, err)
		require.NotNil(t, batch)
		defer batch.Release()

		require.Len(t, batch.Logs, 1)
		assert.Equal(t, protocol.LogTypeOpen, batch.Logs[0].Type)
		assert.Equal(t, "sell-order-1", batch.Logs[0].OrderID)
		assert.Equal(t, uint64(1001), batch.Logs[0].UserID)
		assert.Equal(t, "50000", batch.Logs[0].Price.String())
		assert.Equal(t, "1", batch.Logs[0].Size.String())

		depth := book.GetDepth(10)
		require.NotNil(t, depth)
		require.Len(t, depth.Asks, 1)
		assert.Equal(t, "50000", depth.Asks[0].Price)
		assert.Equal(t, "1", depth.Asks[0].Size)

		// 2. Place Buy Taker order at 50,000 for 0.4 BTC
		buyReq := &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				MarketID:  "BTC-USDT",
				CommandID: "cmd-buy-1",
				UserID:    2002,
				Timestamp: 1000001,
			},
			OrderID:   "buy-order-1",
			Side:      protocol.SideBuy,
			OrderType: protocol.OrderTypeLimit,
			Price:     udecimal.MustFromInt64(50000, 0),
			Size:      udecimal.MustFromInt64(4, 1), // 0.4
		}

		buyBatch, err := book.PlaceOrder(buyReq)
		require.NoError(t, err)
		require.NotNil(t, buyBatch)
		defer buyBatch.Release()

		require.Len(t, buyBatch.Logs, 1)
		trade := buyBatch.Logs[0]
		assert.Equal(t, protocol.LogTypeMatch, trade.Type)
		assert.Equal(t, "buy-order-1", trade.OrderID)
		assert.Equal(t, uint64(2002), trade.UserID)
		assert.Equal(t, "sell-order-1", trade.MakerOrderID)
		assert.Equal(t, uint64(1001), trade.MakerUserID)
		assert.Equal(t, "50000", trade.Price.String())
		assert.Equal(t, "0.4", trade.Size.String())
		assert.Equal(t, "20000", trade.Amount.String()) // 50000 * 0.4 = 20000

		// Remaining ask size should be 0.6
		depth = book.GetDepth(10)
		require.Len(t, depth.Asks, 1)
		assert.Equal(t, "0.6", depth.Asks[0].Size)
	})

	t.Run("CancelOrder Synchronously", func(t *testing.T) {
		book := match.NewOrderBook("BTC-USDT")

		placeReq := &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				MarketID:  "BTC-USDT",
				CommandID: "cmd-place-cancel",
				UserID:    1001,
				Timestamp: 1000000,
			},
			OrderID:   "order-to-cancel",
			Side:      protocol.SideBuy,
			OrderType: protocol.OrderTypeLimit,
			Price:     udecimal.MustFromInt64(49000, 0),
			Size:      udecimal.MustFromInt64(1, 0),
		}

		pBatch, err := book.PlaceOrder(placeReq)
		require.NoError(t, err)
		pBatch.Release()

		// Cancel order
		cancelReq := &protocol.CancelOrderRequest{
			BaseCommand: protocol.BaseCommand{
				MarketID:  "BTC-USDT",
				CommandID: "cmd-cancel-1",
				UserID:    1001,
				Timestamp: 1000002,
			},
			OrderID: "order-to-cancel",
		}

		cBatch, err := book.CancelOrder(cancelReq)
		require.NoError(t, err)
		require.NotNil(t, cBatch)
		defer cBatch.Release()

		require.Len(t, cBatch.Logs, 1)
		assert.Equal(t, protocol.LogTypeCancel, cBatch.Logs[0].Type)
		assert.Equal(t, "order-to-cancel", cBatch.Logs[0].OrderID)

		// Book depth should now be empty
		depth := book.GetDepth(10)
		assert.Empty(t, depth.Bids)

		// Cancelling again should return a RejectLog
		cBatch2, err := book.CancelOrder(cancelReq)
		require.NoError(t, err)
		require.NotNil(t, cBatch2)
		defer cBatch2.Release()

		require.Len(t, cBatch2.Logs, 1)
		assert.Equal(t, protocol.LogTypeReject, cBatch2.Logs[0].Type)
		assert.Equal(t, protocol.RejectReasonOrderNotFound, cBatch2.Logs[0].RejectReason)
	})

	t.Run("AmendOrder Synchronously", func(t *testing.T) {
		book := match.NewOrderBook("BTC-USDT")

		placeReq := &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				MarketID:  "BTC-USDT",
				CommandID: "cmd-place-amend",
				UserID:    1001,
				Timestamp: 1000000,
			},
			OrderID:   "order-to-amend",
			Side:      protocol.SideBuy,
			OrderType: protocol.OrderTypeLimit,
			Price:     udecimal.MustFromInt64(49000, 0),
			Size:      udecimal.MustFromInt64(2, 0),
		}

		pBatch, err := book.PlaceOrder(placeReq)
		require.NoError(t, err)
		pBatch.Release()

		// Amend size decrease: in-place priority retention
		amendReq := &protocol.AmendOrderRequest{
			BaseCommand: protocol.BaseCommand{
				MarketID:  "BTC-USDT",
				CommandID: "cmd-amend-1",
				UserID:    1001,
				Timestamp: 1000003,
			},
			OrderID:  "order-to-amend",
			NewPrice: udecimal.MustFromInt64(49000, 0),
			NewSize:  udecimal.MustFromInt64(1, 0),
		}

		aBatch, err := book.AmendOrder(amendReq)
		require.NoError(t, err)
		require.NotNil(t, aBatch)
		defer aBatch.Release()

		require.Len(t, aBatch.Logs, 1)
		assert.Equal(t, protocol.LogTypeAmend, aBatch.Logs[0].Type)
		assert.Equal(t, "1", aBatch.Logs[0].Size.String())

		depth := book.GetDepth(10)
		require.Len(t, depth.Bids, 1)
		assert.Equal(t, "1", depth.Bids[0].Size)
	})

	t.Run("Snapshot and Restore Synchronously", func(t *testing.T) {
		book := match.NewOrderBook("BTC-USDT")

		// Place bid and ask
		bBatch, _ := book.PlaceOrder(&protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				MarketID:  "BTC-USDT",
				CommandID: "cmd-b1",
				UserID:    1,
				Timestamp: 1000,
			},
			OrderID:   "bid-1",
			Side:      protocol.SideBuy,
			OrderType: protocol.OrderTypeLimit,
			Price:     udecimal.MustFromInt64(48000, 0),
			Size:      udecimal.MustFromInt64(5, 0),
		})
		bBatch.Release()

		aBatch, _ := book.PlaceOrder(&protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				MarketID:  "BTC-USDT",
				CommandID: "cmd-a1",
				UserID:    2,
				Timestamp: 1001,
			},
			OrderID:   "ask-1",
			Side:      protocol.SideSell,
			OrderType: protocol.OrderTypeLimit,
			Price:     udecimal.MustFromInt64(52000, 0),
			Size:      udecimal.MustFromInt64(3, 0),
		})
		aBatch.Release()

		// Create snapshot
		snap := book.Snapshot()
		require.NotNil(t, snap)
		assert.Equal(t, "BTC-USDT", snap.MarketID)
		assert.Len(t, snap.Bids, 1)
		assert.Len(t, snap.Asks, 1)

		// Create fresh new order book and restore
		newBook := match.NewOrderBook("BTC-USDT")
		newBook.Restore(snap)

		depth := newBook.GetDepth(10)
		require.Len(t, depth.Bids, 1)
		assert.Equal(t, "48000", depth.Bids[0].Price)
		assert.Equal(t, "5", depth.Bids[0].Size)

		require.Len(t, depth.Asks, 1)
		assert.Equal(t, "52000", depth.Asks[0].Price)
		assert.Equal(t, "3", depth.Asks[0].Size)
	})

	t.Run("Rejections on Duplicate ID and Invalid Timestamp", func(t *testing.T) {
		book := match.NewOrderBook("BTC-USDT")

		// 1. Invalid Timestamp (zero or negative)
		_, err := book.PlaceOrder(&protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				MarketID:  "BTC-USDT",
				CommandID: "cmd-bad-ts",
				UserID:    1,
				Timestamp: 0,
			},
			OrderID:   "order-bad-ts",
			Side:      protocol.SideBuy,
			OrderType: protocol.OrderTypeLimit,
			Price:     udecimal.MustFromInt64(100, 0),
			Size:      udecimal.MustFromInt64(1, 0),
		})
		// With our update, missing timestamp returns a RejectLog with RejectReasonInvalidPayload
		// Let's verify:
		batch, err := book.PlaceOrder(&protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				MarketID:  "BTC-USDT",
				CommandID: "cmd-bad-ts",
				UserID:    1,
				Timestamp: 0,
			},
			OrderID:   "order-bad-ts",
			Side:      protocol.SideBuy,
			OrderType: protocol.OrderTypeLimit,
			Price:     udecimal.MustFromInt64(100, 0),
			Size:      udecimal.MustFromInt64(1, 0),
		})
		require.NoError(t, err)
		require.NotNil(t, batch)
		require.Len(t, batch.Logs, 1)
		assert.Equal(t, protocol.LogTypeReject, batch.Logs[0].Type)
		assert.Equal(t, protocol.RejectReasonInvalidPayload, batch.Logs[0].RejectReason)
		batch.Release()

		// 2. Normal order
		batch1, err := book.PlaceOrder(&protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				MarketID:  "BTC-USDT",
				CommandID: "cmd-valid",
				UserID:    1,
				Timestamp: 1000,
			},
			OrderID:   "order-dup",
			Side:      protocol.SideBuy,
			OrderType: protocol.OrderTypeLimit,
			Price:     udecimal.MustFromInt64(100, 0),
			Size:      udecimal.MustFromInt64(1, 0),
		})
		require.NoError(t, err)
		batch1.Release()

		// Duplicate order ID
		batch2, err := book.PlaceOrder(&protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				MarketID:  "BTC-USDT",
				CommandID: "cmd-dup",
				UserID:    1,
				Timestamp: 1001,
			},
			OrderID:   "order-dup",
			Side:      protocol.SideBuy,
			OrderType: protocol.OrderTypeLimit,
			Price:     udecimal.MustFromInt64(100, 0),
			Size:      udecimal.MustFromInt64(1, 0),
		})
		require.NoError(t, err)
		require.NotNil(t, batch2)
		require.Len(t, batch2.Logs, 1)
		assert.Equal(t, protocol.LogTypeReject, batch2.Logs[0].Type)
		assert.Equal(t, protocol.RejectReasonDuplicateID, batch2.Logs[0].RejectReason)
		batch2.Release()
	})
}
