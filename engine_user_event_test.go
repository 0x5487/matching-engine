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

	future, err := submitCreateMarket(ctx, engine, &protocol.CreateMarketParams{
		CommandID:  "event-market-create",
		UserID:     1,
		MarketID:   marketID,
		MinLotSize: "1.0",
		Timestamp:  time.Now().UnixNano(),
	})
	require.NoError(t, err)

	go engine.Run()

	_, err = future.Wait(ctx)
	require.NoError(t, err)

	// 1. Place Order
	err = submitPlaceOrder(ctx, engine, &protocol.PlaceOrderParams{
		CommandID: "event-order-1",
		MarketID:  marketID,
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
	err = submitUserEvent(ctx, engine, &protocol.UserEventParams{
		CommandID: "event-user-1",
		UserID:    999,
		EventType: "EndOfBlock",
		Key:       "blk-1",
		Data:      eventData,
		Timestamp: 123456789,
	})
	require.NoError(t, err)

	// 3. Place Another Order
	err = submitPlaceOrder(ctx, engine, &protocol.PlaceOrderParams{
		CommandID: "event-order-2",
		MarketID:  marketID,
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

	err := engine.enqueueCommand(ctx, &protocol.Command{
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

	err := submitUserEvent(ctx, engine, &protocol.UserEventParams{
		CommandID: "event-user-bad-ts",
		UserID:    999,
		EventType: "EndOfBlock",
		Key:       "blk-0",
		Data:      []byte("x"),
		Timestamp: 0,
	})
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
	err := submitUserEvent(context.Background(), engine, &protocol.UserEventParams{
		CommandID: "",
		UserID:    999,
		EventType: "EndOfBlock",
		Key:       "blk-0",
		Data:      []byte("x"),
		Timestamp: 1,
	})
	require.ErrorIs(t, err, ErrInvalidParam)
}
