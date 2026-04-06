package protocol

import (
	"testing"
	"time"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
)

func TestMarshalUnmarshalCommand(t *testing.T) {
	params := &PlaceOrderParams{
		OrderID:     "order-123",
		Side:        SideBuy,
		OrderType:   OrderTypeLimit,
		Price:       udecimal.MustFromInt64(1005, 1),
		Size:        udecimal.MustFromInt64(10, 0),
		VisibleSize: udecimal.MustFromInt64(5, 0),
		QuoteSize:   udecimal.Zero,
	}

	cmd := &Command{
		Version:   1,
		Type:      CmdPlaceOrder,
		UserID:    1001,
		MarketID:  "BTC-USDT",
		CommandID: "cmd-001",
		Timestamp: time.Now().UnixNano(),
		Params:    params,
	}

	data, err := MarshalCommand(cmd)
	assert.NoError(t, err)
	assert.NotNil(t, data)

	decoded, err := UnmarshalCommand(data)
	assert.NoError(t, err)
	assert.Equal(t, cmd.Version, decoded.Version)
	assert.Equal(t, cmd.UserID, decoded.UserID)
	assert.Equal(t, cmd.Type, decoded.Type)
	assert.Equal(t, cmd.MarketID, decoded.MarketID)
	assert.Equal(t, cmd.CommandID, decoded.CommandID)

	decodedParams, ok := decoded.Params.(*PlaceOrderParams)
	assert.True(t, ok)
	assert.Equal(t, params.OrderID, decodedParams.OrderID)
	assert.Equal(t, params.Price.String(), decodedParams.Price.String())
	assert.Equal(t, params.Size.String(), decodedParams.Size.String())

	ReleaseCommand(decoded)
}

func BenchmarkMarshalCommand(b *testing.B) {
	params := &PlaceOrderParams{
		OrderID:     "order-123",
		Side:        SideBuy,
		OrderType:   OrderTypeLimit,
		Price:       udecimal.MustFromInt64(1005, 1),
		Size:        udecimal.MustFromInt64(10, 0),
		VisibleSize: udecimal.MustFromInt64(5, 0),
		QuoteSize:   udecimal.Zero,
	}

	cmd := &Command{
		Version:   1,
		Type:      CmdPlaceOrder,
		UserID:    1001,
		MarketID:  "BTC-USDT",
		CommandID: "cmd-001",
		Timestamp: time.Now().UnixNano(),
		Params:    params,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = MarshalCommand(cmd)
	}
}

func BenchmarkUnmarshalCommand(b *testing.B) {
	params := &PlaceOrderParams{
		OrderID:     "order-123",
		Side:        SideBuy,
		OrderType:   OrderTypeLimit,
		Price:       udecimal.MustFromInt64(1005, 1),
		Size:        udecimal.MustFromInt64(10, 0),
		VisibleSize: udecimal.MustFromInt64(5, 0),
		QuoteSize:   udecimal.Zero,
	}

	cmd := &Command{
		Version:   1,
		Type:      CmdPlaceOrder,
		UserID:    1001,
		MarketID:  "BTC-USDT",
		CommandID: "cmd-001",
		Timestamp: time.Now().UnixNano(),
		Params:    params,
	}

	data, _ := MarshalCommand(cmd)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		decoded, _ := UnmarshalCommand(data)
		ReleaseCommand(decoded)
	}
}
