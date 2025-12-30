package match

import (
	"context"
	"testing"
	"time"

	"github.com/0x5487/matching-engine/protocol"
	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
)

func TestIceberg_Placement(t *testing.T) {
	ctx := context.Background()
	publishTrader := NewMemoryPublishLog()
	orderBook := NewOrderBook("BTC-USDT", publishTrader)
	go func() { _ = orderBook.Start() }()

	// Iceberg: Total 100, Visible 10
	order := &protocol.PlaceOrderCommand{
		OrderID:     "ice-1",
		OrderType:   Limit,
		Side:        Buy,
		Size:        "100",
		VisibleSize: "10",
		Price:       "90",
		UserID:      101,
		Timestamp:   time.Now().UnixNano(),
	}

	err := orderBook.PlaceOrder(ctx, order)
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		depth, _ := orderBook.Depth(1)
		return depth != nil && len(depth.Bids) == 1 && depth.Bids[0].Size == "10"
	}, 1*time.Second, 10*time.Millisecond)
}

func TestIceberg_Replenishment(t *testing.T) {
	ctx := context.Background()
	publishTrader := NewMemoryPublishLog()
	orderBook := NewOrderBook("BTC-USDT", publishTrader)
	go func() { _ = orderBook.Start() }()

	ts := time.Now().UnixNano()

	// 1. Place Iceberg: Total 100, Visible 10
	err := orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
		OrderID:     "ice-1",
		OrderType:   Limit,
		Side:        Sell,
		Size:        "100",
		VisibleSize: "10",
		Price:       "100",
		UserID:      101,
		Timestamp:   ts,
	})
	assert.NoError(t, err)

	// 2. Place Taker: Buy 10
	err = orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
		OrderID:   "taker-1",
		OrderType: Limit,
		Side:      Buy,
		Size:      "10",
		Price:     "100",
		UserID:    201,
		Timestamp: ts + 100,
	})
	assert.NoError(t, err)

	// 3. Verify Match and Replenish
	// Total matches should be 10. Remaining visible should be 10. Remaining hidden should be 80.
	assert.Eventually(t, func() bool {
		depth, _ := orderBook.Depth(1)
		return depth != nil && len(depth.Asks) == 1 && depth.Asks[0].Size == "10"
	}, 1*time.Second, 10*time.Millisecond)

	// Check logs
	logs := publishTrader.Logs()
	matchFound := false
	openFound := false
	for _, l := range logs {
		if l.Type == protocol.LogTypeMatch && l.MakerOrderID == "ice-1" {
			matchFound = true
			assert.Equal(t, "10", l.Size.String())
		}
		if l.Type == protocol.LogTypeOpen && l.OrderID == "ice-1" {
			// This should be the replenishment log
			openFound = true
			assert.Equal(t, "10", l.Size.String())
		}
	}
	assert.True(t, matchFound, "Match log not found")
	assert.True(t, openFound, "Replenishment (Open) log not found")
}

func TestIceberg_ReplenishmentPriority(t *testing.T) {
	ctx := context.Background()
	publishTrader := NewMemoryPublishLog()
	orderBook := NewOrderBook("BTC-USDT", publishTrader)
	go func() { _ = orderBook.Start() }()

	ts := time.Now().UnixNano()

	// 1. Place Iceberg: Total 100, Visible 10 @ 100
	orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
		OrderID:     "ice-1",
		OrderType:   Limit,
		Side:        Sell,
		Size:        "100",
		VisibleSize: "10",
		Price:       "100",
		UserID:      101,
		Timestamp:   ts,
	})

	// 2. Place Normal Order: 10 @ 100 (queued after ice-1)
	orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
		OrderID:   "norm-1",
		OrderType: Limit,
		Side:      Sell,
		Size:      "10",
		Price:     "100",
		UserID:    102,
		Timestamp: ts + 1,
	})

	// Wait for queue
	assert.Eventually(t, func() bool {
		depth, _ := orderBook.Depth(1)
		return depth != nil && len(depth.Asks) == 1 && depth.Asks[0].Size == "20"
	}, 1*time.Second, 10*time.Millisecond)

	// 3. Taker buys 10. This exhausts ice-1's visible part.
	// ice-1 should replenish and move BEHIND norm-1.
	orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
		OrderID:   "taker-1",
		OrderType: Limit,
		Side:      Buy,
		Size:      "10",
		Price:     "100",
		UserID:    201,
		Timestamp: ts + 100,
	})

	// 4. Taker buys another 10. This should match with norm-1, NOT ice-1.
	orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
		OrderID:   "taker-2",
		OrderType: Limit,
		Side:      Buy,
		Size:      "10",
		Price:     "100",
		UserID:    202,
		Timestamp: ts + 200,
	})

	assert.Eventually(t, func() bool {
		logs := publishTrader.Logs()
		matchedWithNorm := false
		for _, l := range logs {
			if l.Type == protocol.LogTypeMatch && l.MakerOrderID == "norm-1" && l.OrderID == "taker-2" {
				matchedWithNorm = true
			}
		}
		return matchedWithNorm
	}, 1*time.Second, 10*time.Millisecond)
}

func TestIceberg_Amend(t *testing.T) {
	ctx := context.Background()
	publishTrader := NewMemoryPublishLog()
	orderBook := NewOrderBook("BTC-USDT", publishTrader)
	go func() { _ = orderBook.Start() }()

	ts := time.Now().UnixNano()

	// 1. Iceberg: Total 100, Visible 10 @ 100
	orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
		OrderID:     "ice-1",
		OrderType:   Limit,
		Side:        Sell,
		Size:        "100",
		VisibleSize: "10",
		Price:       "100",
		UserID:      101,
		Timestamp:   ts,
	})

	// 2. Normal: 10 @ 100
	orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
		OrderID:   "norm-1",
		OrderType: Limit,
		Side:      Sell,
		Size:      "10",
		Price:     "100",
		UserID:    102,
		Timestamp: ts + 1,
	})

	// 3. Amend Iceberg: Decrease total to 50
	// Priority should be retained (ice-1 still at head)
	orderBook.AmendOrder(ctx, &protocol.AmendOrderCommand{
		OrderID:   "ice-1",
		UserID:    101,
		NewPrice:  "100",
		NewSize:   "50",
		Timestamp: ts + 100,
	})

	// Taker buys 5. Should match with ice-1.
	orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
		OrderID:   "taker-1",
		OrderType: Limit,
		Side:      Buy,
		Size:      "5",
		Price:     "100",
		UserID:    201,
		Timestamp: ts + 200,
	})

	assert.Eventually(t, func() bool {
		logs := publishTrader.Logs()
		matchedWithIce := false
		for _, l := range logs {
			if l.Type == protocol.LogTypeMatch && l.MakerOrderID == "ice-1" && l.OrderID == "taker-1" {
				matchedWithIce = true
			}
		}
		return matchedWithIce
	}, 1*time.Second, 10*time.Millisecond)

	// 4. Amend Iceberg: Increase total to 200
	// Priority should be lost (ice-1 moves to tail, behind norm-1)
	orderBook.AmendOrder(ctx, &protocol.AmendOrderCommand{
		OrderID:   "ice-1",
		UserID:    101,
		NewPrice:  "100",
		NewSize:   "200",
		Timestamp: ts + 300,
	})

	// Taker buys 10. Should match with norm-1.
	orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
		OrderID:   "taker-2",
		OrderType: Limit,
		Side:      Buy,
		Size:      "10",
		Price:     "100",
		UserID:    202,
		Timestamp: ts + 400,
	})

	assert.Eventually(t, func() bool {
		logs := publishTrader.Logs()
		matchedWithNorm := false
		for _, l := range logs {
			if l.Type == protocol.LogTypeMatch && l.MakerOrderID == "norm-1" && l.OrderID == "taker-2" {
				matchedWithNorm = true
			}
		}
		return matchedWithNorm
	}, 1*time.Second, 10*time.Millisecond)
}

// TestIceberg_PartialFillNoReplenish verifies that partial fill does NOT trigger replenishment.
func TestIceberg_PartialFillNoReplenish(t *testing.T) {
	ctx := context.Background()
	publishTrader := NewMemoryPublishLog()
	orderBook := NewOrderBook("BTC-USDT", publishTrader)
	go func() { _ = orderBook.Start() }()

	ts := time.Now().UnixNano()

	// 1. Place Iceberg: Total 60, Visible 10, Hidden 50
	orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
		OrderID:     "ice-1",
		OrderType:   Limit,
		Side:        Sell,
		Size:        "60",
		VisibleSize: "10",
		Price:       "100",
		UserID:      101,
		Timestamp:   ts,
	})

	// Wait for order to be placed
	assert.Eventually(t, func() bool {
		depth, _ := orderBook.Depth(1)
		return depth != nil && len(depth.Asks) == 1 && depth.Asks[0].Size == "10"
	}, 1*time.Second, 10*time.Millisecond)

	// 2. Taker buys only 5 (partial fill of visible part)
	orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
		OrderID:   "taker-1",
		OrderType: Limit,
		Side:      Buy,
		Size:      "5",
		Price:     "100",
		UserID:    201,
		Timestamp: ts + 100,
	})

	// 3. Verify: Visible should be 5 (10 - 5), NOT replenished to 10.
	// Hidden should still be 50.
	assert.Eventually(t, func() bool {
		depth, _ := orderBook.Depth(1)
		// After partial fill, visible = 5
		return depth != nil && len(depth.Asks) == 1 && depth.Asks[0].Size == "5"
	}, 1*time.Second, 10*time.Millisecond)

	// Verify no Open (replenishment) log was generated for ice-1 after the match
	logs := publishTrader.Logs()
	replenishCount := 0
	for _, l := range logs {
		// After initial open, if there's another Open for ice-1, it's a replenishment
		if l.Type == protocol.LogTypeOpen && l.OrderID == "ice-1" {
			replenishCount++
		}
	}
	// Should only have the initial Open log, not a replenishment
	assert.Equal(t, 1, replenishCount, "Should not have replenishment log for partial fill")
}

// TestIceberg_TakerAggressiveMatch verifies Iceberg as Taker uses FULL size (not just visible) for matching.
func TestIceberg_TakerAggressiveMatch(t *testing.T) {
	ctx := context.Background()
	publishTrader := NewMemoryPublishLog()
	orderBook := NewOrderBook("BTC-USDT", publishTrader)
	go func() { _ = orderBook.Start() }()

	ts := time.Now().UnixNano()

	// 1. Place multiple Sell orders as liquidity
	for i := 0; i < 5; i++ {
		orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
			OrderID:   "sell-" + string(rune('A'+i)),
			OrderType: Limit,
			Side:      Sell,
			Size:      "20",
			Price:     "100",
			UserID:    int64(100 + i),
			Timestamp: ts + int64(i),
		})
	}

	// Wait for all orders to be placed (total 100 @ 100)
	assert.Eventually(t, func() bool {
		depth, _ := orderBook.Depth(1)
		return depth != nil && len(depth.Asks) == 1 && depth.Asks[0].Size == "100"
	}, 1*time.Second, 10*time.Millisecond)

	// 2. Place Iceberg BUY order as TAKER: Total 80, Visible 10
	// The iceberg should use FULL 80 to match, not just visible 10
	orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
		OrderID:     "ice-buyer",
		OrderType:   Limit,
		Side:        Buy,
		Size:        "80",
		VisibleSize: "10",
		Price:       "100",
		UserID:      999,
		Timestamp:   ts + 100,
	})

	// 3. Verify: All 80 should be matched (4 full orders of 20 each)
	assert.Eventually(t, func() bool {
		logs := publishTrader.Logs()
		totalMatched := udecimal.Zero
		for _, l := range logs {
			if l.Type == protocol.LogTypeMatch && l.OrderID == "ice-buyer" {
				totalMatched = totalMatched.Add(l.Size)
			}
		}
		return totalMatched.String() == "80"
	}, 1*time.Second, 10*time.Millisecond)

	// Verify only 20 remains in the order book (100 - 80)
	assert.Eventually(t, func() bool {
		depth, _ := orderBook.Depth(1)
		return depth != nil && len(depth.Asks) == 1 && depth.Asks[0].Size == "20"
	}, 1*time.Second, 10*time.Millisecond)

	// The iceberg buyer should be fully filled, NOT resting in the book
	stats, _ := orderBook.GetStats()
	assert.Equal(t, int64(0), stats.BidOrderCount, "Iceberg taker should be fully filled, not resting")
}

// TestIceberg_SnapshotRestore verifies Iceberg state is correctly preserved across Snapshot/Restore.
func TestIceberg_SnapshotRestore(t *testing.T) {
	ctx := context.Background()
	publishTrader := NewMemoryPublishLog()
	orderBook := NewOrderBook("BTC-USDT", publishTrader)
	go func() { _ = orderBook.Start() }()

	ts := time.Now().UnixNano()

	// 1. Place Iceberg: Total 100, Visible 10
	orderBook.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
		OrderID:     "ice-1",
		OrderType:   Limit,
		Side:        Sell,
		Size:        "100",
		VisibleSize: "10",
		Price:       "100",
		UserID:      101,
		Timestamp:   ts,
	})

	// Wait for order to be placed
	assert.Eventually(t, func() bool {
		depth, _ := orderBook.Depth(1)
		return depth != nil && len(depth.Asks) == 1
	}, 1*time.Second, 10*time.Millisecond)

	// 2. Take a snapshot
	snapshot, err := orderBook.TakeSnapshot()
	assert.NoError(t, err)
	assert.NotNil(t, snapshot)

	// Verify snapshot contains the iceberg order with correct fields
	assert.Len(t, snapshot.Asks, 1)
	iceOrder := snapshot.Asks[0]
	assert.Equal(t, "ice-1", iceOrder.ID)
	assert.Equal(t, "10", iceOrder.Size.String(), "Visible size should be 10")
	assert.Equal(t, "90", iceOrder.HiddenSize.String(), "Hidden size should be 90")
	assert.Equal(t, "10", iceOrder.VisibleLimit.String(), "VisibleLimit should be 10")

	// 3. Create a new order book and restore from snapshot
	publishTrader2 := NewMemoryPublishLog()
	orderBook2 := NewOrderBook("BTC-USDT", publishTrader2)
	go func() { _ = orderBook2.Start() }()

	orderBook2.Restore(snapshot)

	// 4. Verify restored state
	assert.Eventually(t, func() bool {
		depth, _ := orderBook2.Depth(1)
		return depth != nil && len(depth.Asks) == 1 && depth.Asks[0].Size == "10"
	}, 1*time.Second, 10*time.Millisecond)

	// 5. Verify replenishment still works on restored order book
	// Taker buys 10 to exhaust visible part
	orderBook2.PlaceOrder(ctx, &protocol.PlaceOrderCommand{
		OrderID:   "taker-restore",
		OrderType: Limit,
		Side:      Buy,
		Size:      "10",
		Price:     "100",
		UserID:    201,
		Timestamp: ts + 1000,
	})

	// After replenishment, visible should be 10 again (from hidden)
	assert.Eventually(t, func() bool {
		depth, _ := orderBook2.Depth(1)
		return depth != nil && len(depth.Asks) == 1 && depth.Asks[0].Size == "10"
	}, 1*time.Second, 10*time.Millisecond)

	// Verify match + replenishment logs
	logs := publishTrader2.Logs()
	matchFound := false
	replenishFound := false
	for _, l := range logs {
		if l.Type == protocol.LogTypeMatch && l.MakerOrderID == "ice-1" {
			matchFound = true
		}
		if l.Type == protocol.LogTypeOpen && l.OrderID == "ice-1" {
			replenishFound = true
		}
	}
	assert.True(t, matchFound, "Match log should be generated")
	assert.True(t, replenishFound, "Replenishment log should be generated after restore")
}
