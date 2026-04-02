package match

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

func TestUserEvent_GenericPayload(t *testing.T) {
	publish := NewMemoryPublishLog()
	engine := NewMatchingEngine("event-test-engine", publish)
	marketID := "EVENT-TEST"
	ctx := context.Background()

	err := engine.CreateMarket("admin", marketID, "1.0")
	require.NoError(t, err)

	go engine.Run()

	// 1. Place Order
	err = engine.PlaceOrder(ctx, marketID, &protocol.PlaceOrderCommand{
		OrderID:   "order-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     "100",
		Size:      "1",
		UserID:    1,
	})
	require.NoError(t, err)

	// 2. Send User Event (e.g. EndOfBlock)
	eventData := []byte("block-hash-0x123456")
	err = engine.SendUserEvent(999, "EndOfBlock", "blk-1", eventData)
	require.NoError(t, err)

	// 3. Place Another Order
	err = engine.PlaceOrder(ctx, marketID, &protocol.PlaceOrderCommand{
		OrderID:   "order-2",
		Side:      Buy,
		OrderType: Limit,
		Price:     "101",
		Size:      "1",
		UserID:    2,
	})
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
				if l.Timestamp == 0 {
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
