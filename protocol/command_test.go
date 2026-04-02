package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPlaceOrderCommand_UnmarshalBinaryRejectsTruncatedPayload verifies malformed binary input is rejected.
func TestPlaceOrderCommand_UnmarshalBinaryRejectsTruncatedPayload(t *testing.T) {
	cmd := &PlaceOrderCommand{
		OrderID:     "order-1",
		Side:        SideBuy,
		OrderType:   OrderTypeLimit,
		Price:       "100",
		Size:        "1",
		VisibleSize: "0.5",
		QuoteSize:   "0",
		UserID:      1,
		Timestamp:   42,
	}

	buf := make([]byte, cmd.BinarySize())
	n, err := cmd.MarshalBinary(buf)
	require.NoError(t, err)

	var decoded PlaceOrderCommand
	_, err = decoded.UnmarshalBinary(buf[:n-1])
	require.Error(t, err)
}

// TestAmendOrderCommand_UnmarshalBinaryRejectsTruncatedPayload verifies malformed binary input is rejected.
func TestAmendOrderCommand_UnmarshalBinaryRejectsTruncatedPayload(t *testing.T) {
	cmd := &AmendOrderCommand{
		OrderID:   "order-1",
		UserID:    1,
		NewPrice:  "101",
		NewSize:   "2",
		Timestamp: 42,
	}

	buf := make([]byte, cmd.BinarySize())
	n, err := cmd.MarshalBinary(buf)
	require.NoError(t, err)

	var decoded AmendOrderCommand
	_, err = decoded.UnmarshalBinary(buf[:n-1])
	require.Error(t, err)
	assert.Empty(t, decoded.NewSize)
}
