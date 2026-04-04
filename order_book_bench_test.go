package match

import (
	"context"
	"math/rand"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/quagmt/udecimal"

	"github.com/0x5487/matching-engine/protocol"
)

func BenchmarkPlaceOrders(b *testing.B) {
	// Ensure engine and producer can run concurrently
	oldProcs := runtime.GOMAXPROCS(runtime.NumCPU())
	defer runtime.GOMAXPROCS(oldProcs)

	publishTrader := NewDiscardPublishLog()
	engine := NewMatchingEngine("bench-engine", publishTrader)

	ctx := context.Background()
	marketID := marketBTC
	future, _ := submitCreateMarket(
		ctx,
		engine,
		1,
		marketID,
		"bench-market-create-1",
		time.Now().UnixNano(),
		&protocol.CreateMarketParams{
			MinLotSize: "",
		},
	)

	// Start engine event loop
	go engine.Run()

	_, _ = future.Wait(ctx)

	// Use fixed seed for repeatability
	rng := rand.New(rand.NewSource(42)) //nolint:gosec // G404: test code
	midPrice := int64(10000)

	// Pre-compute decimal prices to reduce allocations in hot loop
	// 1000 ticks: 500 buy-side (midPrice-1 to midPrice-500), 500 sell-side (midPrice+1 to midPrice+500)
	priceCache := make([]udecimal.Decimal, 1001)
	for i := range 1001 {
		priceCache[i] = udecimal.MustFromInt64(
			midPrice-500+int64(i),
			0,
		) // prices from 9500 to 10500
	}
	sizeOne := udecimal.MustFromInt64(1, 0)

	// Pre-allocate and pre-serialize Commands to simulate MQ consumer scenario.
	// In production, Commands arrive already serialized from NATS/Kafka.
	const poolSize = 65536
	cmdPool := make([]*protocol.Command, poolSize)

	for i := range poolSize {
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

		placeCmd := &protocol.PlaceOrderParams{
			OrderID:   strconv.Itoa(i),
			OrderType: Limit,
			Side:      side,
			Price:     priceCache[priceIdx].String(),
			Size:      sizeOne.String(),
		}

		// Pre-serialize payload (simulating MQ message already serialized)
		payload, _ := placeCmd.MarshalBinary()
		cmdPool[i] = &protocol.Command{
			MarketID:  marketID,
			Type:      protocol.CmdPlaceOrder,
			CommandID: "bench-place-" + strconv.Itoa(i),
			UserID:    uint64(rng.Intn(1000) + 1), //nolint:gosec // G115: test code
			Timestamp: time.Now().UnixNano(),
			Payload:   payload,
		}
	}

	b.ResetTimer()

	for i := range b.N {
		// Hot path: only ExecuteCommand (no serialization overhead)
		_ = engine.SubmitAsync(ctx, cmdPool[i%poolSize])
	}

	b.StopTimer()

	// Report final state of the order book
	if f, err := engine.Query(ctx, &protocol.GetStatsRequest{MarketID: marketID}); err == nil {
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
	future, _ := submitCreateMarket(
		ctx,
		engine,
		1,
		marketID,
		"bench-market-create-2",
		time.Now().UnixNano(),
		&protocol.CreateMarketParams{
			MinLotSize: "",
		},
	)

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
			offset := rng.Intn(10) + 1
			if sideR == 0 {
				side = Buy
				priceIdx = 500 - offset
			} else {
				side = Sell
				priceIdx = 500 + offset
			}
		} else {
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

		placeCmd := &protocol.PlaceOrderParams{
			OrderID:   strconv.Itoa(i),
			OrderType: Limit,
			Side:      side,
			Price:     priceCache[priceIdx].String(),
			Size:      sizeOne.String(),
		}

		payload, _ := placeCmd.MarshalBinary()
		cmdPool[i] = &protocol.Command{
			MarketID:  marketID,
			Type:      protocol.CmdPlaceOrder,
			CommandID: "bench-batch-place-" + strconv.Itoa(i),
			UserID:    uint64(rng.Intn(1000) + 1), //nolint:gosec // G115: test code
			Timestamp: time.Now().UnixNano(),
			Payload:   payload,
		}
	}

	// Create batches
	batches := make([][]*protocol.Command, poolSize/batchSize)
	for i := range batches {
		batches[i] = cmdPool[i*batchSize : (i+1)*batchSize]
	}

	b.ResetTimer()

	for i := range b.N {
		// Hot path: SubmitAsyncBatch
		_ = engine.SubmitAsyncBatch(ctx, batches[i%len(batches)])
	}

	b.StopTimer()

	// Report custom metric: orders per second (N iterations * batchSize orders per iteration)
	totalSeconds := b.Elapsed().Seconds()
	if totalSeconds > 0 {
		ordersPerSec := float64(b.N*batchSize) / totalSeconds
		b.ReportMetric(ordersPerSec, "orders/sec")
	}

	_ = engine.Shutdown(context.Background())
}

func BenchmarkMatching(b *testing.B) {
	// Ensure engine run concurrently
	oldProcs := runtime.GOMAXPROCS(runtime.NumCPU())
	defer runtime.GOMAXPROCS(oldProcs)

	publishTrader := NewDiscardPublishLog()
	engine := NewMatchingEngine("bench-engine", publishTrader)
	ctx := context.Background()
	marketID := marketBTC
	future, _ := submitCreateMarket(
		ctx,
		engine,
		1,
		marketID,
		"bench-market-create-3",
		time.Now().UnixNano(),
		&protocol.CreateMarketParams{
			MinLotSize: "",
		},
	)

	// Start engine event loop
	go engine.Run()

	_, _ = future.Wait(ctx)

	price := udecimal.MustFromInt64(10000, 0)
	size := udecimal.MustFromInt64(1, 0)

	// Pre-allocate and pre-serialize command pool
	// We need 2 commands per loop (Sell + Buy that matches)
	poolSize := 4096
	cmdPool := make([]*protocol.Command, poolSize)

	for i := 0; i < poolSize; i += 2 {
		// Sell order (will rest in book)
		sellCmd := &protocol.PlaceOrderParams{
			OrderID:   "sell-" + strconv.Itoa(i),
			Side:      Sell,
			Price:     price.String(),
			Size:      size.String(),
			OrderType: Limit,
		}
		sellPayload, _ := sellCmd.MarshalBinary()
		cmdPool[i] = &protocol.Command{
			MarketID:  marketID,
			Type:      protocol.CmdPlaceOrder,
			CommandID: "bench-match-sell-" + strconv.Itoa(i),
			UserID:    1,
			Timestamp: int64(i + 1),
			Payload:   sellPayload,
		}

		// Buy order (matches the Sell immediately)
		buyCmd := &protocol.PlaceOrderParams{
			OrderID:   "buy-" + strconv.Itoa(i+1),
			Side:      Buy,
			Price:     price.String(),
			Size:      size.String(),
			OrderType: Limit,
		}
		buyPayload, _ := buyCmd.MarshalBinary()
		cmdPool[i+1] = &protocol.Command{
			MarketID:  marketID,
			Type:      protocol.CmdPlaceOrder,
			UserID:    2,
			CommandID: "bench-match-buy-" + strconv.Itoa(i+1),
			Timestamp: int64(i + 2),
			Payload:   buyPayload,
		}
	}

	b.ResetTimer()

	for i := range b.N {
		idx := (i * 2) % poolSize

		// Place Sell (Resting)
		_ = engine.SubmitAsync(ctx, cmdPool[idx])

		// Place Buy (Matches immediately)
		_ = engine.SubmitAsync(ctx, cmdPool[idx+1])
	}

	b.StopTimer()

	// Report ops/sec (each loop is 2 orders)
	totalSeconds := b.Elapsed().Seconds()
	if totalSeconds > 0 {
		ops := float64(b.N) * 2
		b.ReportMetric(ops/totalSeconds, "orders/sec")
	}

	_ = engine.Shutdown(context.Background())
}
