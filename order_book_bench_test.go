package match

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"testing"
	"time"

	"github.com/quagmt/udecimal"

	"github.com/0x5487/matching-engine/protocol"
)

func BenchmarkOrderBook_Match(b *testing.B) {
	// Ensure engine and producer can run concurrently
	oldProcs := runtime.GOMAXPROCS(runtime.NumCPU())
	defer runtime.GOMAXPROCS(oldProcs)

	publishTrader := NewDiscardPublishLog()
	engine := NewMatchingEngine("bench-engine", publishTrader)

	ctx := context.Background()
	marketID := marketBTC
	cmd := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "bench-market-create",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmd.SetPayload(&protocol.CreateMarketParams{
		MinLotSize: "",
	})
	future, err := engine.Submit(ctx, cmd)
	if err != nil {
		b.Fatal(err)
	}

	// Start engine event loop
	go engine.Run()

	if _, err := future.Wait(ctx); err != nil {
		b.Fatal(err)
	}

	// Use fixed seed for repeatability
	rng := rand.New(rand.NewSource(42)) //nolint:gosec // G404: test code
	midPrice := int64(10000)

	// Pre-compute decimal prices to reduce allocations in hot loop
	priceCache := make([]udecimal.Decimal, 1001)
	for i := range 1001 {
		priceCache[i] = udecimal.MustFromInt64(midPrice-500+int64(i), 0)
	}
	sizeOne := udecimal.MustFromInt64(1, 0)

	const poolSize = 65536
	cmdPool := make([]*protocol.Command, poolSize)

	for i := range poolSize {
		var side Side
		var priceIdx int

		r := rng.Intn(100)
		if r < 80 {
			// 80% limit orders around midPrice
			sideR := rng.Intn(2)
			if sideR == 0 {
				side = Buy
				priceIdx = rng.Intn(500) // 9500-9999
			} else {
				side = Sell
				priceIdx = rng.Intn(500) + 501 // 10001-10500
			}
		} else {
			// 20% crossing orders to trigger matches
			sideR := rng.Intn(2)
			if sideR == 0 {
				side = Buy
				priceIdx = rng.Intn(500) + 501 // 10001-10500 (crosses)
			} else {
				side = Sell
				priceIdx = rng.Intn(500) // 9500-9999 (crosses)
			}
		}

		c := &protocol.Command{
			Type:      protocol.CmdPlaceOrder,
			UserID:    uint64(rng.Intn(1000) + 1), //nolint:gosec
			MarketID:  marketID,
			CommandID: fmt.Sprintf("order-%d-%d", i, rng.Int63()),
			Timestamp: time.Now().UnixNano(),
		}
		_ = c.SetPayload(&protocol.PlaceOrderParams{
			OrderID:   c.CommandID,
			Side:      side,
			OrderType: protocol.OrderTypeLimit,
			Price:     priceCache[priceIdx].String(),
			Size:      sizeOne.String(),
		})
		cmdPool[i] = c
	}

	b.ResetTimer()
	b.ReportAllocs()

	// Submit asynchronously
	for i := range b.N {
		cmdIdx := i % poolSize
		_ = engine.SubmitAsync(ctx, cmdPool[cmdIdx])
	}

	b.StopTimer()

	// Report final state of the order book
	query := &protocol.Query{
		Type:     protocol.QueryGetStats,
		MarketID: marketID,
		Payload:  &protocol.GetStatsRequest{MarketID: marketID},
	}
	if f, err := engine.Query(ctx, query); err == nil {
		if res, err := f.Wait(context.Background()); err == nil {
			if stats, ok := res.(*protocol.GetStatsResponse); ok {
				b.Logf(
					"\nFinal Order Book State: Bids=%d levels, Asks=%d levels\n",
					stats.BidDepthCount,
					stats.AskDepthCount,
				)
			}
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

func BenchmarkSubmitAsyncBatch(b *testing.B) {
	// Ensure engine and producer can run concurrently
	oldProcs := runtime.GOMAXPROCS(runtime.NumCPU())
	defer runtime.GOMAXPROCS(oldProcs)

	publishTrader := NewDiscardPublishLog()
	engine := NewMatchingEngine("bench-engine", publishTrader)

	ctx := context.Background()
	marketID := marketBTC
	cmd := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "bench-market-create-2",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmd.SetPayload(&protocol.CreateMarketParams{
		MinLotSize: "",
	})
	future, _ := engine.Submit(ctx, cmd)

	// Start engine event loop
	go engine.Run()

	_, _ = future.Wait(ctx)

	// Use fixed seed for repeatability
	rng := rand.New(rand.NewSource(42)) //nolint:gosec // G404: test code
	midPrice := int64(10000)

	// Pre-compute decimal prices to reduce allocations in hot loop
	priceCache := make([]udecimal.Decimal, 1001)
	for i := range 1001 {
		priceCache[i] = udecimal.MustFromInt64(midPrice-500+int64(i), 0)
	}
	sizeOne := udecimal.MustFromInt64(1, 0)

	const poolSize = 65536
	const batchSize = 100 // Size of each batch

	cmdPool := make([]*protocol.Command, poolSize)

	for i := range poolSize {
		var side Side
		var priceIdx int

		r := rng.Intn(100)
		if r < 80 {
			sideR := rng.Intn(2)
			if sideR == 0 {
				side = Buy
				priceIdx = rng.Intn(500)
			} else {
				side = Sell
				priceIdx = rng.Intn(500) + 501
			}
		} else {
			sideR := rng.Intn(2)
			if sideR == 0 {
				side = Buy
				priceIdx = rng.Intn(500) + 501
			} else {
				side = Sell
				priceIdx = rng.Intn(500)
			}
		}

		c := &protocol.Command{
			Type:      protocol.CmdPlaceOrder,
			UserID:    uint64(rng.Intn(1000) + 1), //nolint:gosec
			MarketID:  marketID,
			CommandID: fmt.Sprintf("order-%d-%d", i, rng.Int63()),
			Timestamp: time.Now().UnixNano(),
		}
		_ = c.SetPayload(&protocol.PlaceOrderParams{
			OrderID:   c.CommandID,
			Side:      side,
			OrderType: protocol.OrderTypeLimit,
			Price:     priceCache[priceIdx].String(),
			Size:      sizeOne.String(),
		})
		cmdPool[i] = c
	}

	b.ResetTimer()
	b.ReportAllocs()

	numBatches := b.N / batchSize
	for i := range numBatches {
		startIdx := (i * batchSize) % (poolSize - batchSize)
		batch := cmdPool[startIdx : startIdx+batchSize]
		_ = engine.SubmitAsyncBatch(ctx, batch)
	}

	b.StopTimer()
	_ = engine.Shutdown(context.Background())
}
