package match

import (
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

// amendStepBook rests one buy of 10 at 100 for user 1.
func amendStepBook(t *testing.T) *OrderBook {
	t.Helper()
	book := NewOrderBook("BTC-USDT", WithPublisher(NewDiscardPublishLog()))
	batch, err := book.PlaceOrder(&protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{UserID: 1, CommandID: "cmd-rest", MarketID: "BTC-USDT", Timestamp: 1},
		OrderID:     "rest",
		OrderType:   Limit,
		Side:        Buy,
		Price:       udecimal.MustParse("100"),
		Size:        udecimal.MustParse("10"),
	})
	require.NoError(t, err)
	batch.Release()
	return book
}

// TestAmendStepSize pins the step check on amendments. NewSize that is not a whole
// number of StepSize is refused as invalid payload and changes nothing. A zero
// StepSize means no unit is given, so NewSize is not checked, as for a new order.
// A zero NewSize means no size change, so it is never checked.
func TestAmendStepSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		newSize  string
		stepSize string
		refused  bool
		wantSize string
	}{
		{name: "zero step does not check", newSize: "2.5", stepSize: "0", wantSize: "2.5"},
		{name: "fraction of the step is refused", newSize: "2.5", stepSize: "1", refused: true, wantSize: "10"},
		{name: "whole steps are accepted", newSize: "4", stepSize: "1", wantSize: "4"},
		{name: "half steps are accepted at a 0.5 step", newSize: "2.5", stepSize: "0.5", wantSize: "2.5"},
		{name: "zero new size is no size change and is not checked", newSize: "0", stepSize: "1", wantSize: "10"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			book := amendStepBook(t)
			batch, err := book.AmendOrder(&protocol.AmendOrderRequest{
				BaseCommand: protocol.BaseCommand{
					UserID:    1,
					CommandID: "cmd-amend",
					MarketID:  "BTC-USDT",
					Timestamp: 2,
				},
				OrderID:  "rest",
				NewSize:  udecimal.MustParse(tc.newSize),
				StepSize: udecimal.MustParse(tc.stepSize),
			})
			require.NoError(t, err)
			defer batch.Release()
			if tc.refused {
				requireInvalidPayload(t, batch, "rest")
			} else {
				require.Len(t, batch.Logs, 1)
				require.Equal(t, protocol.LogTypeAmend, batch.Logs[0].Type)
			}
			rest, ok := book.findOrder("rest")
			require.True(t, ok)
			require.Equal(t, tc.wantSize, rest.Size.Add(rest.HiddenSize).String())
		})
	}
}
