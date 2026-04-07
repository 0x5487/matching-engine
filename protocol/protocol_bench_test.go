package protocol

import (
	"testing"
	"time"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/require"
)

func TestMarshalUnmarshalRequestBench(t *testing.T) {
	req := &PlaceOrderRequest{
		BaseCommand: BaseCommand{
			SeqID:     123,
			CommandID: "cmd-place",
			UserID:    789,
			MarketID:  "BTC-USDT",
			Timestamp: time.Now().UnixNano(),
		},
		OrderID:     "order-123",
		Side:        SideBuy,
		OrderType:   OrderTypeLimit,
		Price:       udecimal.MustFromInt64(1005, 1),
		Size:        udecimal.MustFromInt64(10, 0),
		VisibleSize: udecimal.MustFromInt64(5, 0),
		QuoteSize:   udecimal.Zero,
	}

	data, err := MarshalRequest(req)
	require.NoError(t, err)
	require.NotNil(t, data)

	decoded, err := UnmarshalRequest(data)
	require.NoError(t, err)
	require.Equal(t, req, decoded)
}

func BenchmarkMarshalRequest(b *testing.B) {
	req := &PlaceOrderRequest{
		BaseCommand: BaseCommand{
			SeqID:     123,
			CommandID: "cmd-place",
			UserID:    789,
			MarketID:  "BTC-USDT",
			Timestamp: time.Now().UnixNano(),
		},
		OrderID:     "order-123",
		Side:        SideBuy,
		OrderType:   OrderTypeLimit,
		Price:       udecimal.MustFromInt64(1005, 1),
		Size:        udecimal.MustFromInt64(10, 0),
		VisibleSize: udecimal.MustFromInt64(5, 0),
		QuoteSize:   udecimal.Zero,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = MarshalRequest(req)
	}
}

func BenchmarkUnmarshalRequest(b *testing.B) {
	req := &PlaceOrderRequest{
		BaseCommand: BaseCommand{
			SeqID:     123,
			CommandID: "cmd-place",
			UserID:    789,
			MarketID:  "BTC-USDT",
			Timestamp: time.Now().UnixNano(),
		},
		OrderID:     "order-123",
		Side:        SideBuy,
		OrderType:   OrderTypeLimit,
		Price:       udecimal.MustFromInt64(1005, 1),
		Size:        udecimal.MustFromInt64(10, 0),
		VisibleSize: udecimal.MustFromInt64(5, 0),
		QuoteSize:   udecimal.Zero,
	}

	data, _ := MarshalRequest(req)

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = UnmarshalRequest(data)
	}
}
