package match

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"strconv"
	"testing"

	"github.com/0x5487/matching-engine/protocol"
	"github.com/quagmt/udecimal"
)

func BenchmarkPlaceOrders(b *testing.B) {
	// Ensure engine and producer can run concurrently
	oldProcs := runtime.GOMAXPROCS(runtime.NumCPU())
	defer runtime.GOMAXPROCS(oldProcs)

	publishTrader := NewDiscardPublishLog()
	engine := NewMatchingEngine(publishTrader)

	marketID := "BTC-USDT"
	_, _ = engine.AddOrderBook(marketID)

	// Use fixed seed for repeatability
	rng := rand.New(rand.NewSource(42))
	midPrice := int64(10000)

	// Pre-compute decimal prices to reduce allocations in hot loop
	// 1000 ticks: 500 buy-side (midPrice-1 to midPrice-500), 500 sell-side (midPrice+1 to midPrice+500)
	priceCache := make([]udecimal.Decimal, 1001)
	for i := int64(0); i <= 1000; i++ {
		priceCache[i] = udecimal.MustFromInt64(midPrice-500+i, 0) // prices from 9500 to 10500
	}
	sizeOne := udecimal.MustFromInt64(1, 0)

	// Pre-allocate and pre-serialize Commands to simulate MQ consumer scenario.
	// In production, Commands arrive already serialized from NATS/Kafka.
	const poolSize = 65536
	serializer := &protocol.DefaultJSONSerializer{}
	cmdPool := make([]*protocol.Command, poolSize)

	for i := 0; i < poolSize; i++ {
		var side Side
		var priceIdx int

		// 80/20 Distribution
		r := rng.Intn(100)
		if r < 80 {
			// 80% in Top 10 ticks (10 for Buy, 10 for Sell)
			sideR := rng.Intn(2)
			offset := rng.Intn(10) + 1
			if sideR == 0 {
				side = Buy
				priceIdx = 500 - offset
			} else {
				side = Sell
				priceIdx = 500 + offset
			}
		} else {
			// 20% in remaining 490 ticks per side
			sideR := rng.Intn(2)
			offset := rng.Intn(490) + 11
			if sideR == 0 {
				side = Buy
				priceIdx = 500 - offset
			} else {
				side = Sell
				priceIdx = 500 + offset
			}
		}

		placeCmd := &protocol.PlaceOrderCommand{
			OrderID:   strconv.Itoa(i),
			OrderType: Limit,
			Side:      side,
			Price:     priceCache[priceIdx].String(),
			Size:      sizeOne.String(),
			UserID:    int64(rng.Intn(1000) + 1),
		}

		// Pre-serialize payload (simulating MQ message already serialized)
		payload, _ := serializer.Marshal(placeCmd)
		cmdPool[i] = &protocol.Command{
			MarketID: marketID,
			Type:     protocol.CmdPlaceOrder,
			Payload:  payload,
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Hot path: only ExecuteCommand (no serialization overhead)
		_ = engine.ExecuteCommand(cmdPool[i%poolSize])
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

	_ = engine.Shutdown(context.Background())
}

func BenchmarkMatching(b *testing.B) {
	// Ensure engine run concurrently
	oldProcs := runtime.GOMAXPROCS(runtime.NumCPU())
	defer runtime.GOMAXPROCS(oldProcs)

	publishTrader := NewDiscardPublishLog()
	engine := NewMatchingEngine(publishTrader)
	marketID := "MATCH-USDT"
	_, _ = engine.AddOrderBook(marketID)

	price := udecimal.MustFromInt64(10000, 0)
	size := udecimal.MustFromInt64(1, 0)
	serializer := &protocol.DefaultJSONSerializer{}

	// Pre-allocate and pre-serialize command pool
	// We need 2 commands per loop (Sell + Buy that matches)
	poolSize := 4096
	cmdPool := make([]*protocol.Command, poolSize)

	for i := 0; i < poolSize; i += 2 {
		// Sell order (will rest in book)
		sellCmd := &protocol.PlaceOrderCommand{
			OrderID:   "sell-" + strconv.Itoa(i),
			UserID:    1,
			Side:      Sell,
			Price:     price.String(),
			Size:      size.String(),
			OrderType: Limit,
		}
		sellPayload, _ := serializer.Marshal(sellCmd)
		cmdPool[i] = &protocol.Command{
			MarketID: marketID,
			Type:     protocol.CmdPlaceOrder,
			Payload:  sellPayload,
		}

		// Buy order (matches the Sell immediately)
		buyCmd := &protocol.PlaceOrderCommand{
			OrderID:   "buy-" + strconv.Itoa(i+1),
			UserID:    2,
			Side:      Buy,
			Price:     price.String(),
			Size:      size.String(),
			OrderType: Limit,
		}
		buyPayload, _ := serializer.Marshal(buyCmd)
		cmdPool[i+1] = &protocol.Command{
			MarketID: marketID,
			Type:     protocol.CmdPlaceOrder,
			Payload:  buyPayload,
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		idx := (i * 2) % poolSize

		// Place Sell (Resting)
		_ = engine.ExecuteCommand(cmdPool[idx])

		// Place Buy (Matches immediately)
		_ = engine.ExecuteCommand(cmdPool[idx+1])
	}

	b.StopTimer()

	// Report ops/sec (each loop is 2 orders)
	totalSeconds := b.Elapsed().Seconds()
	if totalSeconds > 0 {
		ops := float64(b.N) * 2
		b.ReportMetric(ops/totalSeconds, "orders/sec")
	}
}
