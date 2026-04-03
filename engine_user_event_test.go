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

	future, err := engine.CreateMarket(ctx, "event-market-create", 1, marketID, "1.0", time.Now().UnixNano())
	require.NoError(t, err)

	go engine.Run()

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// 1. Place Order
	err = engine.PlaceOrder(ctx, marketID, &protocol.PlaceOrderCommand{
		CommandID: "event-order-1",
		OrderID:   "order-1",
		Side:      Buy,
		OrderType: Limit,
		Price:     "100",
		Size:      "1",
		UserID:    1,
		Timestamp: 1,
	})
	require.NoError(t, err)

	// 2. Send User Event (e.g. EndOfBlock)
	eventData := []byte("block-hash-0x123456")
	err = engine.SendUserEvent("event-user-1", 999, "EndOfBlock", "blk-1", eventData, 123456789)
	require.NoError(t, err)

	// 3. Place Another Order
	err = engine.PlaceOrder(ctx, marketID, &protocol.PlaceOrderCommand{
		CommandID: "event-order-2",
		OrderID:   "order-2",
		Side:      Buy,
		OrderType: Limit,
		Price:     "101",
		Size:      "1",
		UserID:    2,
		Timestamp: 2,
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

	err := engine.EnqueueCommand(&protocol.Command{
		Type:      protocol.CmdUserEvent,
		CommandID: "bad-user-event",
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

	err := engine.SendUserEvent("event-user-bad-ts", 999, "EndOfBlock", "blk-0", []byte("x"), 0)
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
	err := engine.SendUserEvent("", 999, "EndOfBlock", "blk-0", []byte("x"), 1)
	require.ErrorIs(t, err, ErrInvalidParam)
}
