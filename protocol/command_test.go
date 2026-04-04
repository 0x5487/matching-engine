package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommand_MarshalUnmarshalBinary(t *testing.T) {
	cmd := &Command{
		Version:   1,
		Type:      CmdPlaceOrder,
		SeqID:     123,
		MarketID:  "BTC-USDT",
		CommandID: "cmd-456",
		Timestamp: 1678901234,
		Payload:   []byte("test-payload"),
	}

	data, err := cmd.MarshalBinary()
	require.NoError(t, err)

	var decoded Command
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	assert.Equal(t, cmd.Version, decoded.Version)
	assert.Equal(t, cmd.Type, decoded.Type)
	assert.Equal(t, cmd.SeqID, decoded.SeqID)
	assert.Equal(t, cmd.MarketID, decoded.MarketID)
	assert.Equal(t, cmd.CommandID, decoded.CommandID)
	assert.Equal(t, cmd.Timestamp, decoded.Timestamp)
	assert.Equal(t, cmd.Payload, decoded.Payload)
}

func TestPlaceOrderParams_MarshalUnmarshalBinary(t *testing.T) {
	params := &PlaceOrderParams{
		OrderID:     "order-1",
		Side:        SideBuy,
		OrderType:   OrderTypeLimit,
		Price:       "100.5",
		Size:        "10",
		VisibleSize: "5",
		QuoteSize:   "0",
		UserID:      123,
		Timestamp:   1678901234,
	}

	data, err := params.MarshalBinary()
	require.NoError(t, err)

	var decoded PlaceOrderParams
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	assert.Equal(t, params.OrderID, decoded.OrderID)
	assert.Equal(t, params.Side, decoded.Side)
	assert.Equal(t, params.OrderType, decoded.OrderType)
	assert.Equal(t, params.Price, decoded.Price)
	assert.Equal(t, params.Size, decoded.Size)
	assert.Equal(t, params.VisibleSize, decoded.VisibleSize)
	assert.Equal(t, params.QuoteSize, decoded.QuoteSize)
	assert.Equal(t, params.UserID, decoded.UserID)
}

// TestPlaceOrderParams_UnmarshalBinaryRejectsTruncatedPayload verifies malformed binary input is rejected.
func TestPlaceOrderParams_UnmarshalBinaryRejectsTruncatedPayload(t *testing.T) {
	cmd := &PlaceOrderParams{
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

	data, err := cmd.MarshalBinary()
	require.NoError(t, err)

	var decoded PlaceOrderParams
	err = decoded.UnmarshalBinary(data[:len(data)-1])
	require.Error(t, err)
}

func TestCancelOrderParams_MarshalUnmarshalBinary(t *testing.T) {
	params := &CancelOrderParams{
		OrderID:   "order-123",
		UserID:    456,
		Timestamp: 1678901234,
	}

	data, err := params.MarshalBinary()
	require.NoError(t, err)

	var decoded CancelOrderParams
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	assert.Equal(t, params.OrderID, decoded.OrderID)
	assert.Equal(t, params.UserID, decoded.UserID)
}

func TestAmendOrderParams_MarshalUnmarshalBinary(t *testing.T) {
	params := &AmendOrderParams{
		OrderID:   "order-123",
		UserID:    456,
		Timestamp: 1678901234,
		NewPrice:  "100.5",
		NewSize:   "10",
	}

	data, err := params.MarshalBinary()
	require.NoError(t, err)

	var decoded AmendOrderParams
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	assert.Equal(t, params.OrderID, decoded.OrderID)
	assert.Equal(t, params.UserID, decoded.UserID)
	assert.Equal(t, params.NewPrice, decoded.NewPrice)
	assert.Equal(t, params.NewSize, decoded.NewSize)
}

func TestCreateMarketParams_MarshalUnmarshalBinary(t *testing.T) {
	params := &CreateMarketParams{
		UserID:     1,
		Timestamp:  1678901234,
		MinLotSize: "0.01",
	}

	data, err := params.MarshalBinary()
	require.NoError(t, err)

	var decoded CreateMarketParams
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	assert.Equal(t, params.UserID, decoded.UserID)
	assert.Equal(t, params.MinLotSize, decoded.MinLotSize)
}

func TestSuspendMarketParams_MarshalUnmarshalBinary(t *testing.T) {
	params := &SuspendMarketParams{
		UserID:    1,
		Timestamp: 1678901234,
		MarketID:  "BTC-USDT",
		Reason:    "maintenance",
	}

	data, err := params.MarshalBinary()
	require.NoError(t, err)

	var decoded SuspendMarketParams
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	assert.Equal(t, params.UserID, decoded.UserID)
	assert.Equal(t, params.Reason, decoded.Reason)
}

func TestResumeMarketParams_MarshalUnmarshalBinary(t *testing.T) {
	params := &ResumeMarketParams{
		UserID:    1,
		Timestamp: 1678901234,
		MarketID:  "BTC-USDT",
	}

	data, err := params.MarshalBinary()
	require.NoError(t, err)

	var decoded ResumeMarketParams
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	assert.Equal(t, params.UserID, decoded.UserID)
}

func TestUpdateConfigParams_MarshalUnmarshalBinary(t *testing.T) {
	params := &UpdateConfigParams{
		UserID:     1,
		Timestamp:  1678901234,
		MarketID:   "BTC-USDT",
		MinLotSize: "0.001",
	}

	data, err := params.MarshalBinary()
	require.NoError(t, err)

	var decoded UpdateConfigParams
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	assert.Equal(t, params.UserID, decoded.UserID)
	assert.Equal(t, params.MinLotSize, decoded.MinLotSize)
}

func TestUserEventParams_MarshalUnmarshalBinary(t *testing.T) {
	params := &UserEventParams{
		UserID:    1,
		Timestamp: 1678901234,
		EventType: "Audit",
		Key:       "key-1",
		Data:      []byte("some-data"),
	}

	data, err := params.MarshalBinary()
	require.NoError(t, err)

	var decoded UserEventParams
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	assert.Equal(t, params.UserID, decoded.UserID)
	assert.Equal(t, params.EventType, decoded.EventType)
	assert.Equal(t, params.Key, decoded.Key)
	assert.Equal(t, params.Data, decoded.Data)
}
