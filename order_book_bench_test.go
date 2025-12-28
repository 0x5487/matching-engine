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

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var side Side
		var price int64

		// 80/20 Distribution
		r := rng.Intn(100)
		if r < 80 {
			// 80% in Top 10 ticks (10 for Buy, 10 for Sell)
			sideR := rng.Intn(2)
			offset := int64(rng.Intn(10)) + 1
			if sideR == 0 {
				side = Buy
				price = midPrice - offset
			} else {
				side = Sell
				price = midPrice + offset
			}
		} else {
			// 20% in remaining 490 ticks per side
			sideR := rng.Intn(2)
			offset := int64(rng.Intn(490)) + 11
			if sideR == 0 {
				side = Buy
				price = midPrice - offset
			} else {
				side = Sell
				price = midPrice + offset
			}
		}

		order := &PlaceOrderCommand{
			MarketID: marketID,
			ID:       string(strconv.AppendInt(make([]byte, 0, 16), int64(i), 10)),
			Type:     Limit,
			Side:     side,
			Price:    decimal.NewFromInt(price),
			Size:     decimal.NewFromInt(1),
			UserID:   int64(rng.Intn(1000) + 1),
		}

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
