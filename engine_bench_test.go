package match

import (
	"context"
	"runtime"
	"strconv"
	"testing"

	"github.com/shopspring/decimal"
)

const (
	start = 2000 // actual = start  * goprocs
	end   = 3000 // actual = end    * goprocs
	step  = 200
)

func BenchmarkPlaceOrders(b *testing.B) {
	// Restore GOMAXPROCS to default to allow engine and producer to run concurrently
	runtime.GOMAXPROCS(runtime.NumCPU())

	ctx := context.Background()
	publishTrader := NewDiscardPublishLog()
	engine := NewMatchingEngine(publishTrader)

	// Ensure market exists before benchmark
	_ = engine.OrderBook("BTC-USDT")

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Use simple static data to test engine throughput, not RNG performance
		order := &PlaceOrderCommand{
			MarketID: "BTC-USDT",
			ID:       strconv.Itoa(i),
			Type:     Limit,
			Side:     Buy,
			Price:    decimal.NewFromInt(100),
			Size:     decimal.NewFromInt(1),
			UserID:   1,
		}

		_ = engine.AddOrder(ctx, order)
	}

	b.StopTimer()
	_ = engine.Shutdown(ctx)
}
