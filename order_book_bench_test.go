package match

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/quagmt/udecimal"

	"github.com/0x5487/matching-engine/protocol"
)

const (
	benchmarkMarketBTC              = "BTC-USDT"
	benchmarkBatchSize10            = 10
	benchmarkWarmTopLevelCount      = 20
	benchmarkWarmOuterLevelCount    = 80
	benchmarkWarmupCommandCount     = 4000
	benchmarkProductionPatternCount = 20
)

var benchmarkUnitSize = udecimal.MustFromInt64(1, 0)

// benchmarkNewEngine creates a benchmark engine, starts the event loop, and creates the market.
func benchmarkNewEngine(b *testing.B, marketID string) (context.Context, *MatchingEngine) {
	b.Helper()

	ctx := context.Background()
	engine := NewMatchingEngine("bench-engine", NewDiscardPublishLog())
	req := &protocol.CreateMarketRequest{
		BaseCommand: protocol.BaseCommand{
			Type:      protocol.CmdCreateMarket,
			UserID:    1,
			MarketID:  marketID,
			CommandID: "bench-market-create-" + marketID,
			Timestamp: time.Now().UnixNano(),
		},
		MinLotSize: udecimal.Zero,
	}

	future, err := engine.CreateMarket(ctx, req)
	if err != nil {
		b.Fatal(err)
	}

	go engine.Run()

	if _, err := future.Wait(ctx); err != nil {
		b.Fatal(err)
	}

	return ctx, engine
}

// benchmarkDrain waits until all previously submitted commands have been processed.
func benchmarkDrain(
	ctx context.Context,
	b *testing.B,
	engine *MatchingEngine,
	marketID string,
) *protocol.GetStatsResponse {
	b.Helper()

	query := &protocol.Query{
		Type:     protocol.QueryGetStats,
		MarketID: marketID,
	}

	future, err := engine.Query(ctx, query)
	if err != nil {
		b.Fatal(err)
	}

	result, err := future.Wait(ctx)
	if err != nil {
		b.Fatal(err)
	}

	stats, ok := result.(*protocol.GetStatsResponse)
	if !ok {
		b.Fatalf("unexpected stats response type %T", result)
	}

	return stats
}

// benchmarkLogDepth logs the final book depth so benchmark runs can be compared safely.
func benchmarkLogDepth(b *testing.B, stats *protocol.GetStatsResponse) {
	b.Helper()

	if os.Getenv("BENCH_DEBUG_DEPTH") == "" {
		return
	}

	b.Logf("Final Order Book State: Bids=%d levels, Asks=%d levels", stats.BidDepthCount, stats.AskDepthCount)
}

// benchmarkReportOrdersPerSecond reports throughput using the logical order count.
func benchmarkReportOrdersPerSecond(b *testing.B, orderCount int) {
	b.Helper()

	totalSeconds := b.Elapsed().Seconds()
	if totalSeconds > 0 {
		b.ReportMetric(float64(orderCount)/totalSeconds, "orders/sec")
	}
}

// benchmarkCrossingCommandPool builds a deterministic crossing stream where each pair matches immediately.
func benchmarkCrossingCommandPool(marketID string, pairCount int) []*protocol.PlaceOrderRequest {
	price := udecimal.MustFromInt64(10000, 0)
	reqs := make([]*protocol.PlaceOrderRequest, pairCount*2)

	for i := range pairCount {
		sellIndex := i * 2
		reqs[sellIndex] = &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				Type:      protocol.CmdPlaceOrder,
				UserID:    1,
				MarketID:  marketID,
				CommandID: fmt.Sprintf("cross-sell-cmd-%d", sellIndex),
				Timestamp: int64(sellIndex + 1),
			},
			OrderID:   fmt.Sprintf("cross-sell-order-%d", sellIndex),
			Side:      Sell,
			OrderType: protocol.OrderTypeLimit,
			Price:     price,
			Size:      benchmarkUnitSize,
		}

		buyIndex := sellIndex + 1
		reqs[buyIndex] = &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				Type:      protocol.CmdPlaceOrder,
				UserID:    2,
				MarketID:  marketID,
				CommandID: fmt.Sprintf("cross-buy-cmd-%d", buyIndex),
				Timestamp: int64(buyIndex + 1),
			},
			OrderID:   fmt.Sprintf("cross-buy-order-%d", buyIndex),
			Side:      Buy,
			OrderType: protocol.OrderTypeLimit,
			Price:     price,
			Size:      benchmarkUnitSize,
		}
	}

	return reqs
}

// benchmarkSubmitSingle submits commands one-by-one through PlaceOrderAsync.
func benchmarkSubmitSingle(ctx context.Context, engine *MatchingEngine, reqs []*protocol.PlaceOrderRequest) {
	for _, req := range reqs {
		_ = engine.PlaceOrderAsync(ctx, req)
	}
}

// benchmarkSubmitBatch10 submits commands in batches of ten through PlaceOrderBatchAsync.
func benchmarkSubmitBatch10(ctx context.Context, engine *MatchingEngine, reqs []*protocol.PlaceOrderRequest) {
	for i := 0; i < len(reqs); i += benchmarkBatchSize10 {
		end := min(i+benchmarkBatchSize10, len(reqs))
		_ = engine.PlaceOrderBatchAsync(ctx, reqs[i:end])
	}
}

// benchmarkWarmBookCommandPool builds warmup and measured streams for the production-like benchmark.
func benchmarkWarmBookCommandPool(
	marketID string,
	warmupCount int,
	measuredCount int,
) (warmupReqs []*protocol.PlaceOrderRequest, measuredReqs []*protocol.PlaceOrderRequest) {
	const midPrice = int64(10000)

	warmup := make([]*protocol.PlaceOrderRequest, 0, warmupCount)
	measured := make([]*protocol.PlaceOrderRequest, measuredCount)

	nextTimestamp := int64(1)
	nextOrderIndex := 0

	buildRequest := func(
		prefix string,
		userID uint64,
		side Side,
		price udecimal.Decimal,
	) *protocol.PlaceOrderRequest {
		commandID := fmt.Sprintf("%s-cmd-%d", prefix, nextOrderIndex)
		orderID := fmt.Sprintf("%s-order-%d", prefix, nextOrderIndex)
		nextOrderIndex++
		req := &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				Type:      protocol.CmdPlaceOrder,
				UserID:    userID,
				MarketID:  marketID,
				CommandID: commandID,
				Timestamp: nextTimestamp,
			},
			OrderID:   orderID,
			Side:      side,
			OrderType: protocol.OrderTypeLimit,
			Price:     price,
			Size:      benchmarkUnitSize,
		}
		nextTimestamp++
		return req
	}

	nearCount := warmupCount * 80 / 100
	outerCount := warmupCount - nearCount

	for i := range nearCount {
		level := i % benchmarkWarmTopLevelCount
		if i%2 == 0 {
			price := udecimal.MustFromInt64(midPrice-1-int64(level), 0)
			warmup = append(warmup, buildRequest("warm-near-bid", 1, Buy, price))
			continue
		}
		price := udecimal.MustFromInt64(midPrice+1+int64(level), 0)
		warmup = append(warmup, buildRequest("warm-near-ask", 2, Sell, price))
	}

	for i := range outerCount {
		level := i % benchmarkWarmOuterLevelCount
		if i%2 == 0 {
			price := udecimal.MustFromInt64(midPrice-1-int64(benchmarkWarmTopLevelCount+level), 0)
			warmup = append(warmup, buildRequest("warm-outer-bid", 3, Buy, price))
			continue
		}
		price := udecimal.MustFromInt64(midPrice+1+int64(benchmarkWarmTopLevelCount+level), 0)
		warmup = append(warmup, buildRequest("warm-outer-ask", 4, Sell, price))
	}

	for i := range measuredCount {
		patternPos := i % benchmarkProductionPatternCount

		if patternPos < 14 {
			if patternPos%2 == 0 {
				price := udecimal.MustFromInt64(midPrice+1, 0)
				measured[i] = buildRequest("prod-cross-buy", 10, Buy, price)
				continue
			}
			price := udecimal.MustFromInt64(midPrice-1, 0)
			measured[i] = buildRequest("prod-cross-sell", 11, Sell, price)
			continue
		}

		replenishLevel := (i / benchmarkProductionPatternCount) % benchmarkWarmTopLevelCount
		if patternPos%2 == 0 {
			price := udecimal.MustFromInt64(midPrice-1-int64(replenishLevel), 0)
			measured[i] = buildRequest("prod-rest-bid", 12, Buy, price)
			continue
		}
		price := udecimal.MustFromInt64(midPrice+1+int64(replenishLevel), 0)
		measured[i] = buildRequest("prod-rest-ask", 13, Sell, price)
	}

	return warmup, measured
}

// BenchmarkCrossing_EndToEnd_Single measures pure crossing throughput with single-order submission.
func BenchmarkCrossing_EndToEnd_Single(b *testing.B) {
	oldProcs := runtime.GOMAXPROCS(runtime.NumCPU())
	defer runtime.GOMAXPROCS(oldProcs)

	ctx, engine := benchmarkNewEngine(b, "CROSS-USDT")
	cmds := benchmarkCrossingCommandPool("CROSS-USDT", b.N)

	b.ResetTimer()
	b.ReportAllocs()
	benchmarkSubmitSingle(ctx, engine, cmds)
	stats := benchmarkDrain(ctx, b, engine, "CROSS-USDT")
	b.StopTimer()

	benchmarkLogDepth(b, stats)
	benchmarkReportOrdersPerSecond(b, len(cmds))
	_ = engine.Shutdown(context.Background())
}

// BenchmarkCrossing_EndToEnd_Batch10 measures pure crossing throughput with batch-of-10 submission.
func BenchmarkCrossing_EndToEnd_Batch10(b *testing.B) {
	oldProcs := runtime.GOMAXPROCS(runtime.NumCPU())
	defer runtime.GOMAXPROCS(oldProcs)

	ctx, engine := benchmarkNewEngine(b, "CROSS-BATCH-USDT")
	cmds := benchmarkCrossingCommandPool("CROSS-BATCH-USDT", b.N)

	b.ResetTimer()
	b.ReportAllocs()
	benchmarkSubmitBatch10(ctx, engine, cmds)
	stats := benchmarkDrain(ctx, b, engine, "CROSS-BATCH-USDT")
	b.StopTimer()

	benchmarkLogDepth(b, stats)
	benchmarkReportOrdersPerSecond(b, len(cmds))
	_ = engine.Shutdown(context.Background())
}

// BenchmarkProductionWarmBook_EndToEnd_Single measures warm-book throughput with single-order submission.
func BenchmarkProductionWarmBook_EndToEnd_Single(b *testing.B) {
	oldProcs := runtime.GOMAXPROCS(runtime.NumCPU())
	defer runtime.GOMAXPROCS(oldProcs)

	ctx, engine := benchmarkNewEngine(b, "PROD-USDT")
	warmupCmds, measuredCmds := benchmarkWarmBookCommandPool(
		"PROD-USDT",
		benchmarkWarmupCommandCount,
		b.N,
	)

	benchmarkSubmitSingle(ctx, engine, warmupCmds)
	_ = benchmarkDrain(ctx, b, engine, "PROD-USDT")

	b.ResetTimer()
	b.ReportAllocs()
	benchmarkSubmitSingle(ctx, engine, measuredCmds)
	stats := benchmarkDrain(ctx, b, engine, "PROD-USDT")
	b.StopTimer()

	benchmarkLogDepth(b, stats)
	benchmarkReportOrdersPerSecond(b, len(measuredCmds))
	_ = engine.Shutdown(context.Background())
}

// BenchmarkProductionWarmBook_EndToEnd_Batch10 measures warm-book throughput with batch-of-10 submission.
func BenchmarkProductionWarmBook_EndToEnd_Batch10(b *testing.B) {
	oldProcs := runtime.GOMAXPROCS(runtime.NumCPU())
	defer runtime.GOMAXPROCS(oldProcs)

	ctx, engine := benchmarkNewEngine(b, "PROD-BATCH-USDT")
	warmupCmds, measuredCmds := benchmarkWarmBookCommandPool(
		"PROD-BATCH-USDT",
		benchmarkWarmupCommandCount,
		b.N,
	)

	benchmarkSubmitSingle(ctx, engine, warmupCmds)
	_ = benchmarkDrain(ctx, b, engine, "PROD-BATCH-USDT")

	b.ResetTimer()
	b.ReportAllocs()
	benchmarkSubmitBatch10(ctx, engine, measuredCmds)
	stats := benchmarkDrain(ctx, b, engine, "PROD-BATCH-USDT")
	b.StopTimer()

	benchmarkLogDepth(b, stats)
	benchmarkReportOrdersPerSecond(b, len(measuredCmds))
	_ = engine.Shutdown(context.Background())
}

// BenchmarkOrderBook_Match measures the existing random end-to-end benchmark for comparison.
func BenchmarkOrderBook_Match(b *testing.B) {
	// Ensure engine and producer can run concurrently
	oldProcs := runtime.GOMAXPROCS(runtime.NumCPU())
	defer runtime.GOMAXPROCS(oldProcs)

	publishTrader := NewDiscardPublishLog()
	engine := NewMatchingEngine("bench-engine", publishTrader)

	ctx := context.Background()
	marketID := benchmarkMarketBTC
	req := &protocol.CreateMarketRequest{
		BaseCommand: protocol.BaseCommand{
			Type:      protocol.CmdCreateMarket,
			UserID:    1,
			MarketID:  marketID,
			CommandID: "bench-market-create",
			Timestamp: time.Now().UnixNano(),
		},
		MinLotSize: udecimal.Zero,
	}
	future, err := engine.CreateMarket(ctx, req)
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

	const poolSize = 1000000
	cmdPool := make([]*protocol.PlaceOrderRequest, poolSize)

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

		cmdPool[i] = &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				Type:      protocol.CmdPlaceOrder,
				UserID:    (rng.Uint64() % 1000) + 1,
				MarketID:  marketID,
				CommandID: fmt.Sprintf("o-%d-%d", i, rng.Int63()),
				Timestamp: time.Now().UnixNano(),
			},
			OrderID:   fmt.Sprintf("o-%d-%d", i, rng.Int63()),
			Side:      side,
			OrderType: protocol.OrderTypeLimit,
			Price:     priceCache[priceIdx],
			Size:      sizeOne,
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	// Enqueue orders asynchronously and measure end-to-end processing through the engine.
	for i := range b.N {
		cmdIdx := i % poolSize
		_ = engine.PlaceOrderAsync(ctx, cmdPool[cmdIdx])
	}

	// Send a sentinel query to ensure all preceding commands are processed
	query := &protocol.Query{
		Type:     protocol.QueryGetStats,
		MarketID: marketID,
	}
	if f, err := engine.Query(ctx, query); err == nil {
		_, _ = f.Wait(ctx)
	}

	b.StopTimer()

	// Report final state of the order book
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

// BenchmarkSubmitAsyncBatch measures batch submission on the existing random benchmark workload.
func BenchmarkSubmitAsyncBatch(b *testing.B) {
	// Ensure engine and producer can run concurrently
	oldProcs := runtime.GOMAXPROCS(runtime.NumCPU())
	defer runtime.GOMAXPROCS(oldProcs)

	publishTrader := NewDiscardPublishLog()
	engine := NewMatchingEngine("bench-engine", publishTrader)

	ctx := context.Background()
	marketID := benchmarkMarketBTC
	req := &protocol.CreateMarketRequest{
		BaseCommand: protocol.BaseCommand{
			Type:      protocol.CmdCreateMarket,
			UserID:    1,
			MarketID:  marketID,
			CommandID: "bench-market-create-2",
			Timestamp: time.Now().UnixNano(),
		},
		MinLotSize: udecimal.Zero,
	}
	future, _ := engine.CreateMarket(ctx, req)

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

	const poolSize = 5000000
	const batchSize = 100 // Size of each batch

	cmdPool := make([]*protocol.PlaceOrderRequest, poolSize)

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

		cmdPool[i] = &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				Type:      protocol.CmdPlaceOrder,
				UserID:    (rng.Uint64() % 1000) + 1,
				MarketID:  marketID,
				CommandID: fmt.Sprintf("order-%d-%d", i, rng.Int63()),
				Timestamp: time.Now().UnixNano(),
			},
			OrderID:   fmt.Sprintf("order-%d-%d", i, rng.Int63()),
			Side:      side,
			OrderType: protocol.OrderTypeLimit,
			Price:     priceCache[priceIdx],
			Size:      sizeOne,
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	numBatches := b.N / batchSize
	for i := range numBatches {
		startIdx := (i * batchSize) % (poolSize - batchSize)
		batch := cmdPool[startIdx : startIdx+batchSize]
		_ = engine.PlaceOrderBatchAsync(ctx, batch)
	}

	// Wait for processing to finish
	query := &protocol.Query{
		Type:     protocol.QueryGetStats,
		MarketID: marketID,
	}
	if f, err := engine.Query(ctx, query); err == nil {
		_, _ = f.Wait(ctx)
	}

	b.StopTimer()
	_ = engine.Shutdown(context.Background())
}
