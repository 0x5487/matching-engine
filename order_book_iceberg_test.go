package match

import (
	"testing"
	"time"

	"github.com/0x5487/matching-engine/protocol"
	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
)

func TestIceberg_Placement(t *testing.T) {
	publishTrader := NewMemoryPublishLog()
	orderBook := newOrderBook("BTC-USDT", publishTrader)

	// Iceberg: Total 100, Visible 10
	testPlace(orderBook, &protocol.PlaceOrderCommand{
		OrderID:     "ice-1",
		OrderType:   Limit,
		Side:        Buy,
		Size:        "100",
		VisibleSize: "10",
		Price:       "90",
		UserID:      101,
		Timestamp:   time.Now().UnixNano(),
	})

	depth := orderBook.depth(1)
	assert.Len(t, depth.Bids, 1)
	assert.Equal(t, "10", depth.Bids[0].Size)
}

func TestIceberg_Replenishment(t *testing.T) {
	publishTrader := NewMemoryPublishLog()
	orderBook := newOrderBook("BTC-USDT", publishTrader)

	ts := time.Now().UnixNano()

	// 1. Place Iceberg: Total 100, Visible 10
	testPlace(orderBook, &protocol.PlaceOrderCommand{
		OrderID:     "ice-1",
		OrderType:   Limit,
		Side:        Sell,
		Size:        "100",
		VisibleSize: "10",
		Price:       "100",
		UserID:      101,
		Timestamp:   ts,
	})

	// 2. Place Taker: Buy 10
	testPlace(orderBook, &protocol.PlaceOrderCommand{
		OrderID:   "taker-1",
		OrderType: Limit,
		Side:      Buy,
		Size:      "10",
		Price:     "100",
		UserID:    201,
		Timestamp: ts + 100,
	})

	// 3. Verify Match and Replenish
	depth := orderBook.depth(1)
	assert.Len(t, depth.Asks, 1)
	assert.Equal(t, "10", depth.Asks[0].Size)

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
			openFound = true
			assert.Equal(t, "10", l.Size.String())
		}
	}
	assert.True(t, matchFound, "Match log not found")
	assert.True(t, openFound, "Replenishment (Open) log not found")
}

func TestIceberg_ReplenishmentPriority(t *testing.T) {
	publishTrader := NewMemoryPublishLog()
	orderBook := newOrderBook("BTC-USDT", publishTrader)

	ts := time.Now().UnixNano()

	// 1. Place Iceberg: Total 100, Visible 10 @ 100
	testPlace(orderBook, &protocol.PlaceOrderCommand{
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
	testPlace(orderBook, &protocol.PlaceOrderCommand{
		OrderID:   "norm-1",
		OrderType: Limit,
		Side:      Sell,
		Size:      "10",
		Price:     "100",
		UserID:    102,
		Timestamp: ts + 1,
	})

	depth := orderBook.depth(1)
	assert.Len(t, depth.Asks, 1)
	assert.Equal(t, "20", depth.Asks[0].Size)

	// 3. Taker buys 10. This exhausts ice-1's visible part.
	// ice-1 should replenish and move BEHIND norm-1.
	testPlace(orderBook, &protocol.PlaceOrderCommand{
		OrderID:   "taker-1",
		OrderType: Limit,
		Side:      Buy,
		Size:      "10",
		Price:     "100",
		UserID:    201,
		Timestamp: ts + 100,
	})

	// 4. Taker buys another 10. This should match with norm-1, NOT ice-1.
	testPlace(orderBook, &protocol.PlaceOrderCommand{
		OrderID:   "taker-2",
		OrderType: Limit,
		Side:      Buy,
		Size:      "10",
		Price:     "100",
		UserID:    202,
		Timestamp: ts + 200,
	})

	logs := publishTrader.Logs()
	matchedWithNorm := false
	for _, l := range logs {
		if l.Type == protocol.LogTypeMatch && l.MakerOrderID == "norm-1" && l.OrderID == "taker-2" {
			matchedWithNorm = true
		}
	}
	assert.True(t, matchedWithNorm, "taker-2 should match with norm-1")
}

func TestIceberg_Amend(t *testing.T) {
	publishTrader := NewMemoryPublishLog()
	orderBook := newOrderBook("BTC-USDT", publishTrader)

	ts := time.Now().UnixNano()

	// 1. Iceberg: Total 100, Visible 10 @ 100
	testPlace(orderBook, &protocol.PlaceOrderCommand{
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
	testPlace(orderBook, &protocol.PlaceOrderCommand{
		OrderID:   "norm-1",
		OrderType: Limit,
		Side:      Sell,
		Size:      "10",
		Price:     "100",
		UserID:    102,
		Timestamp: ts + 1,
	})

	// 3. Amend Iceberg: Decrease total to 50
	testAmend(orderBook, &protocol.AmendOrderCommand{
		OrderID:   "ice-1",
		UserID:    101,
		NewPrice:  "100",
		NewSize:   "50",
		Timestamp: ts + 100,
	})

	// Taker buys 5. Should match with ice-1.
	testPlace(orderBook, &protocol.PlaceOrderCommand{
		OrderID:   "taker-1",
		OrderType: Limit,
		Side:      Buy,
		Size:      "5",
		Price:     "100",
		UserID:    201,
		Timestamp: ts + 200,
	})

	logs := publishTrader.Logs()
	matchedWithIce := false
	for _, l := range logs {
		if l.Type == protocol.LogTypeMatch && l.MakerOrderID == "ice-1" && l.OrderID == "taker-1" {
			matchedWithIce = true
		}
	}
	assert.True(t, matchedWithIce, "taker-1 should match with ice-1")

	// 4. Amend Iceberg: Increase total to 200
	testAmend(orderBook, &protocol.AmendOrderCommand{
		OrderID:   "ice-1",
		UserID:    101,
		NewPrice:  "100",
		NewSize:   "200",
		Timestamp: ts + 300,
	})

	// Taker buys 10. Should match with norm-1.
	testPlace(orderBook, &protocol.PlaceOrderCommand{
		OrderID:   "taker-2",
		OrderType: Limit,
		Side:      Buy,
		Size:      "10",
		Price:     "100",
		UserID:    202,
		Timestamp: ts + 400,
	})

	logs = publishTrader.Logs()
	matchedWithNorm := false
	for _, l := range logs {
		if l.Type == protocol.LogTypeMatch && l.MakerOrderID == "norm-1" && l.OrderID == "taker-2" {
			matchedWithNorm = true
		}
	}
	assert.True(t, matchedWithNorm, "taker-2 should match with norm-1")
}

// TestIceberg_PartialFillNoReplenish verifies that partial fill does NOT trigger replenishment.
func TestIceberg_PartialFillNoReplenish(t *testing.T) {
	publishTrader := NewMemoryPublishLog()
	orderBook := newOrderBook("BTC-USDT", publishTrader)

	ts := time.Now().UnixNano()

	// 1. Place Iceberg: Total 60, Visible 10, Hidden 50
	testPlace(orderBook, &protocol.PlaceOrderCommand{
		OrderID:     "ice-1",
		OrderType:   Limit,
		Side:        Sell,
		Size:        "60",
		VisibleSize: "10",
		Price:       "100",
		UserID:      101,
		Timestamp:   ts,
	})

	depth := orderBook.depth(1)
	assert.Len(t, depth.Asks, 1)
	assert.Equal(t, "10", depth.Asks[0].Size)

	// 2. Taker buys only 5 (partial fill of visible part)
	testPlace(orderBook, &protocol.PlaceOrderCommand{
		OrderID:   "taker-1",
		OrderType: Limit,
		Side:      Buy,
		Size:      "5",
		Price:     "100",
		UserID:    201,
		Timestamp: ts + 100,
	})

	// 3. Verify: Visible should be 5 (10 - 5), NOT replenished to 10.
	depth = orderBook.depth(1)
	assert.Len(t, depth.Asks, 1)
	assert.Equal(t, "5", depth.Asks[0].Size)

	// Verify no Open (replenishment) log was generated for ice-1 after the match
	logs := publishTrader.Logs()
	replenishCount := 0
	for _, l := range logs {
		if l.Type == protocol.LogTypeOpen && l.OrderID == "ice-1" {
			replenishCount++
		}
	}
	assert.Equal(t, 1, replenishCount, "Should not have replenishment log for partial fill")
}

// TestIceberg_TakerAggressiveMatch verifies Iceberg as Taker uses FULL size (not just visible) for matching.
func TestIceberg_TakerAggressiveMatch(t *testing.T) {
	publishTrader := NewMemoryPublishLog()
	orderBook := newOrderBook("BTC-USDT", publishTrader)

	ts := time.Now().UnixNano()

	// 1. Place multiple Sell orders as liquidity
	for i := 0; i < 5; i++ {
		testPlace(orderBook, &protocol.PlaceOrderCommand{
			OrderID:   "sell-" + string(rune('A'+i)),
			OrderType: Limit,
			Side:      Sell,
			Size:      "20",
			Price:     "100",
			UserID:    uint64(100 + i),
			Timestamp: ts + int64(i),
		})
	}

	depth := orderBook.depth(1)
	assert.Len(t, depth.Asks, 1)
	assert.Equal(t, "100", depth.Asks[0].Size)

	// 2. Place Iceberg BUY order as TAKER: Total 80, Visible 10
	testPlace(orderBook, &protocol.PlaceOrderCommand{
		OrderID:     "ice-buyer",
		OrderType:   Limit,
		Side:        Buy,
		Size:        "80",
		VisibleSize: "10",
		Price:       "100",
		UserID:      999,
		Timestamp:   ts + 100,
	})

	// 3. Verify: All 80 should be matched
	logs := publishTrader.Logs()
	totalMatched := udecimal.Zero
	for _, l := range logs {
		if l.Type == protocol.LogTypeMatch && l.OrderID == "ice-buyer" {
			totalMatched = totalMatched.Add(l.Size)
		}
	}
	assert.Equal(t, "80", totalMatched.String())

	// Verify only 20 remains in the order book (100 - 80)
	depth = orderBook.depth(1)
	assert.Len(t, depth.Asks, 1)
	assert.Equal(t, "20", depth.Asks[0].Size)

	// The iceberg buyer should be fully filled, NOT resting in the book
	assert.Equal(t, int64(0), orderBook.bidQueue.orderCount(), "Iceberg taker should be fully filled, not resting")
}

// TestIceberg_SnapshotRestore verifies Iceberg state is correctly preserved across Snapshot/Restore.
func TestIceberg_SnapshotRestore(t *testing.T) {
	publishTrader := NewMemoryPublishLog()
	orderBook := newOrderBook("BTC-USDT", publishTrader)

	ts := time.Now().UnixNano()

	// 1. Place Iceberg: Total 100, Visible 10
	testPlace(orderBook, &protocol.PlaceOrderCommand{
		OrderID:     "ice-1",
		OrderType:   Limit,
		Side:        Sell,
		Size:        "100",
		VisibleSize: "10",
		Price:       "100",
		UserID:      101,
		Timestamp:   ts,
	})

	depth := orderBook.depth(1)
	assert.Len(t, depth.Asks, 1)

	// 2. Take a snapshot
	snapshot := orderBook.createSnapshot()
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
	orderBook2 := newOrderBook("BTC-USDT", publishTrader2)
	orderBook2.Restore(snapshot)

	// 4. Verify restored state
	depth2 := orderBook2.depth(1)
	assert.Len(t, depth2.Asks, 1)
	assert.Equal(t, "10", depth2.Asks[0].Size)

	// 5. Verify replenishment still works on restored order book
	testPlace(orderBook2, &protocol.PlaceOrderCommand{
		OrderID:   "taker-restore",
		OrderType: Limit,
		Side:      Buy,
		Size:      "10",
		Price:     "100",
		UserID:    201,
		Timestamp: ts + 1000,
	})

	// After replenishment, visible should be 10 again (from hidden)
	depth2 = orderBook2.depth(1)
	assert.Len(t, depth2.Asks, 1)
	assert.Equal(t, "10", depth2.Asks[0].Size)

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
