package match

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"strconv"
	"testing"

	"github.com/shopspring/decimal"
)

func BenchmarkPlaceOrders(b *testing.B) {
	// Ensure engine and producer can run concurrently
	oldProcs := runtime.GOMAXPROCS(runtime.NumCPU())
	defer runtime.GOMAXPROCS(oldProcs)

	ctx := context.Background()
	publishTrader := NewDiscardPublishLog()
	engine := NewMatchingEngine(publishTrader)

	marketID := "BTC-USDT"
	_, _ = engine.AddOrderBook(marketID)

	// Use fixed seed for repeatability
	rng := rand.New(rand.NewSource(42))
	midPrice := int64(10000)

	// Pre-compute decimal prices to reduce allocations in hot loop
	// 1000 ticks: 500 buy-side (midPrice-1 to midPrice-500), 500 sell-side (midPrice+1 to midPrice+500)
	priceCache := make([]decimal.Decimal, 1001)
	for i := int64(0); i <= 1000; i++ {
		priceCache[i] = decimal.NewFromInt(midPrice - 500 + i) // prices from 9500 to 10500
	}
	sizeOne := decimal.NewFromInt(1)

	// Reuse a single PlaceOrderCommand struct
	order := &PlaceOrderCommand{
		MarketID: marketID,
		Type:     Limit,
		Size:     sizeOne,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var priceIdx int

		// 80/20 Distribution
		r := rng.Intn(100)
		if r < 80 {
			// 80% in Top 10 ticks (10 for Buy, 10 for Sell)
			sideR := rng.Intn(2)
			offset := rng.Intn(10) + 1
			if sideR == 0 {
				order.Side = Buy
				priceIdx = 500 - offset // 490-499 -> prices 9990-9999
			} else {
				order.Side = Sell
				priceIdx = 500 + offset // 501-510 -> prices 10001-10010
			}
		} else {
			// 20% in remaining 490 ticks per side
			sideR := rng.Intn(2)
			offset := rng.Intn(490) + 11
			if sideR == 0 {
				order.Side = Buy
				priceIdx = 500 - offset // 10-499 -> prices 9510-9989
			} else {
				order.Side = Sell
				priceIdx = 500 + offset // 511-1000 -> prices 10011-10500
			}
		}

		order.ID = strconv.FormatInt(int64(i), 10)
		order.Price = priceCache[priceIdx]
		order.UserID = int64(rng.Intn(1000) + 1)

		_ = engine.AddOrder(ctx, order)
	}

	b.StopTimer()

	// Report final state of the order book
	if ob := engine.OrderBook(marketID); ob != nil {
		if stats, err := ob.GetStats(); err == nil {
			fmt.Printf("\nFinal Order Book State: Bids=%d levels, Asks=%d levels\n", stats.BidDepthCount, stats.AskDepthCount)
		}
	}

	// Report custom metric: orders per second
	totalSeconds := b.Elapsed().Seconds()
	if totalSeconds > 0 {
		ordersPerSec := float64(b.N) / totalSeconds
		b.ReportMetric(ordersPerSec, "orders/sec")
	}

	_ = engine.Shutdown(ctx)
}
