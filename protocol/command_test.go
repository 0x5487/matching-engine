package protocol

import (
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/require"
)

func TestCommand_MarshalUnmarshalBinary(t *testing.T) {
	cmd := &Command{
		Version:   1,
		Type:      CmdPlaceOrder,
		SeqID:     123,
		UserID:    789,
		MarketID:  "BTC-USDT",
		CommandID: "cmd-456",
		Timestamp: 1678901234,
		Params: &PlaceOrderParams{
			OrderID: "order-1",
			Price:   udecimal.MustFromInt64(100, 0),
			Size:    udecimal.MustFromInt64(1, 0),
		},
	}

	data, err := cmd.MarshalBinary()
	require.NoError(t, err)

	decoded, err := UnmarshalCommand(data)
	require.NoError(t, err)

	require.Equal(t, cmd.Version, decoded.Version)
	require.Equal(t, cmd.Type, decoded.Type)
	require.Equal(t, cmd.SeqID, decoded.SeqID)
	require.Equal(t, cmd.UserID, decoded.UserID)
	require.Equal(t, cmd.MarketID, decoded.MarketID)
	require.Equal(t, cmd.CommandID, decoded.CommandID)
	require.Equal(t, cmd.Timestamp, decoded.Timestamp)

	p, ok := decoded.Params.(*PlaceOrderParams)
	require.True(t, ok)
	require.Equal(t, "order-1", p.OrderID)
	require.Equal(t, "100", p.Price.String())

	ReleaseCommand(decoded)
}

func TestPlaceOrderParams_MarshalUnmarshalBinary(t *testing.T) {
	params := &PlaceOrderParams{
		OrderID:     "order-1",
		Side:        SideBuy,
		OrderType:   OrderTypeLimit,
		Price:       udecimal.MustFromInt64(1005, 1),
		Size:        udecimal.MustFromInt64(10, 0),
		VisibleSize: udecimal.MustFromInt64(5, 0),
		QuoteSize:   udecimal.Zero,
	}

	data, err := params.MarshalBinary()
	require.NoError(t, err)

	var decoded PlaceOrderParams
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	require.Equal(t, params.OrderID, decoded.OrderID)
	require.Equal(t, params.Side, decoded.Side)
	require.Equal(t, params.OrderType, decoded.OrderType)
	require.Equal(t, params.Price.String(), decoded.Price.String())
	require.Equal(t, params.Size.String(), decoded.Size.String())
}

func TestPlaceOrderParams_UnmarshalBinaryRejectsTruncatedPayload(t *testing.T) {
	cmd := &PlaceOrderParams{
		OrderID:     "order-1",
		Side:        SideBuy,
		OrderType:   OrderTypeLimit,
		Price:       udecimal.MustFromInt64(100, 0),
		Size:        udecimal.MustFromInt64(1, 0),
		VisibleSize: udecimal.MustFromInt64(5, 1),
		QuoteSize:   udecimal.Zero,
	}

	data, err := cmd.MarshalBinary()
	require.NoError(t, err)

	var decoded PlaceOrderParams
	err = decoded.UnmarshalBinary(data[:len(data)-1])
	require.Error(t, err)
}

func TestCancelOrderParams_MarshalUnmarshalBinary(t *testing.T) {
	params := &CancelOrderParams{
		OrderID: "order-123",
	}

	data, err := params.MarshalBinary()
	require.NoError(t, err)

	var decoded CancelOrderParams
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	require.Equal(t, params.OrderID, decoded.OrderID)
}

func TestAmendOrderParams_MarshalUnmarshalBinary(t *testing.T) {
	params := &AmendOrderParams{
		OrderID:  "order-123",
		NewPrice: udecimal.MustFromInt64(1005, 1),
		NewSize:  udecimal.MustFromInt64(10, 0),
	}

	data, err := params.MarshalBinary()
	require.NoError(t, err)

	var decoded AmendOrderParams
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	require.Equal(t, params.OrderID, decoded.OrderID)
	require.Equal(t, params.NewPrice.String(), decoded.NewPrice.String())
	require.Equal(t, params.NewSize.String(), decoded.NewSize.String())
}

func TestCreateMarketParams_MarshalUnmarshalBinary(t *testing.T) {
	params := &CreateMarketParams{
		MinLotSize: udecimal.MustFromInt64(1, 2),
	}

	data, err := params.MarshalBinary()
	require.NoError(t, err)

	var decoded CreateMarketParams
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	require.Equal(t, params.MinLotSize.String(), decoded.MinLotSize.String())
}

func TestUpdateConfigParams_MarshalUnmarshalBinary(t *testing.T) {
	params := &UpdateConfigParams{
		MinLotSize: udecimal.MustFromInt64(1, 3),
	}

	data, err := params.MarshalBinary()
	require.NoError(t, err)

	var decoded UpdateConfigParams
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	require.Equal(t, params.MinLotSize.String(), decoded.MinLotSize.String())
}

func TestUserEventParams_MarshalUnmarshalBinary(t *testing.T) {
	params := &UserEventParams{
		EventType: "Audit",
		Key:       "key-1",
		Data:      []byte("some-data"),
	}

	data, err := params.MarshalBinary()
	require.NoError(t, err)

	var decoded UserEventParams
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	require.Equal(t, params.EventType, decoded.EventType)
	require.Equal(t, params.Key, decoded.Key)
	require.Equal(t, params.Data, decoded.Data)
}

func TestCommand_SetAndUnmarshalPayload(t *testing.T) {
	cmd := &Command{
		Type:      CmdPlaceOrder,
		UserID:    123,
		MarketID:  "BTC-USDT",
		CommandID: "cmd-456",
		Timestamp: 1678901234,
	}

	params := &PlaceOrderParams{
		OrderID: "order-1",
		Side:    SideBuy,
		Price:   udecimal.MustFromInt64(1005, 1),
		Size:    udecimal.MustFromInt64(10, 0),
	}

	err := cmd.SetPayload(params)
	require.NoError(t, err)

	data, err := cmd.MarshalBinary()
	require.NoError(t, err)

	decodedCmd, err := UnmarshalCommand(data)
	require.NoError(t, err)

	require.Equal(t, cmd.UserID, decodedCmd.UserID)
	require.Equal(t, cmd.CommandID, decodedCmd.CommandID)

	p, ok := decodedCmd.Params.(*PlaceOrderParams)
	require.True(t, ok)
	require.Equal(t, params.OrderID, p.OrderID)
	require.Equal(t, params.Price.String(), p.Price.String())

	ReleaseCommand(decodedCmd)
}
