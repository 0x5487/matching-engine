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
	cmd := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "bench-market-create-" + marketID,
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmd.SetPayload(&protocol.CreateMarketParams{MinLotSize: udecimal.Zero})

	future, err := engine.Submit(ctx, cmd)
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
		Payload:  &protocol.GetStatsRequest{MarketID: marketID},
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

// benchmarkBuildPlaceOrderCommand creates a deterministic place-order command for benchmark flows.
func benchmarkBuildPlaceOrderCommand(
	marketID string,
	commandID string,
	orderID string,
	userID uint64,
	side Side,
	price udecimal.Decimal,
	timestamp int64,
) *protocol.Command {
	cmd := &protocol.Command{
		Type:      protocol.CmdPlaceOrder,
		UserID:    userID,
		MarketID:  marketID,
		CommandID: commandID,
		Timestamp: timestamp,
	}
	_ = cmd.SetPayload(&protocol.PlaceOrderParams{
		OrderID:   orderID,
		Side:      side,
		OrderType: protocol.OrderTypeLimit,
		Price:     price,
		Size:      benchmarkUnitSize,
	})

	return cmd
}

// benchmarkSubmitSingle submits commands one-by-one through SubmitAsync.
func benchmarkSubmitSingle(ctx context.Context, engine *MatchingEngine, cmds []*protocol.Command) {
	for _, cmd := range cmds {
		_ = engine.SubmitAsync(ctx, cmd)
	}
}

// benchmarkSubmitBatch10 submits commands in batches of ten through SubmitAsyncBatch.
func benchmarkSubmitBatch10(ctx context.Context, engine *MatchingEngine, cmds []*protocol.Command) {
	for i := 0; i < len(cmds); i += benchmarkBatchSize10 {
		end := min(i+benchmarkBatchSize10, len(cmds))
		_ = engine.SubmitAsyncBatch(ctx, cmds[i:end])
	}
}

// benchmarkCrossingCommandPool builds a deterministic crossing stream where each pair matches immediately.
func benchmarkCrossingCommandPool(marketID string, pairCount int) []*protocol.Command {
	price := udecimal.MustFromInt64(10000, 0)
	cmds := make([]*protocol.Command, pairCount*2)

	for i := range pairCount {
		sellIndex := i * 2
		cmds[sellIndex] = benchmarkBuildPlaceOrderCommand(
			marketID,
			fmt.Sprintf("cross-sell-cmd-%d", sellIndex),
			fmt.Sprintf("cross-sell-order-%d", sellIndex),
			1,
			Sell,
			price,
			int64(sellIndex+1),
		)

		buyIndex := sellIndex + 1
		cmds[buyIndex] = benchmarkBuildPlaceOrderCommand(
			marketID,
			fmt.Sprintf("cross-buy-cmd-%d", buyIndex),
			fmt.Sprintf("cross-buy-order-%d", buyIndex),
			2,
			Buy,
			price,
			int64(buyIndex+1),
		)
	}

	return cmds
}

// benchmarkWarmBookCommandPool builds warmup and measured streams for the production-like benchmark.
func benchmarkWarmBookCommandPool(
	marketID string,
	warmupCount int,
	measuredCount int,
) ([]*protocol.Command, []*protocol.Command) {
	const midPrice = int64(10000)

	warmup := make([]*protocol.Command, 0, warmupCount)
	measured := make([]*protocol.Command, measuredCount)

	nextTimestamp := int64(1)
	nextOrderIndex := 0

	buildCommand := func(
		prefix string,
		userID uint64,
		side Side,
		price udecimal.Decimal,
	) *protocol.Command {
		commandID := fmt.Sprintf("%s-cmd-%d", prefix, nextOrderIndex)
		orderID := fmt.Sprintf("%s-order-%d", prefix, nextOrderIndex)
		nextOrderIndex++
		cmd := benchmarkBuildPlaceOrderCommand(
			marketID,
			commandID,
			orderID,
			userID,
			side,
			price,
			nextTimestamp,
		)
		nextTimestamp++
		return cmd
	}

	nearCount := warmupCount * 80 / 100
	outerCount := warmupCount - nearCount

	for i := range nearCount {
		level := i % benchmarkWarmTopLevelCount
		if i%2 == 0 {
			price := udecimal.MustFromInt64(midPrice-1-int64(level), 0)
			warmup = append(warmup, buildCommand("warm-near-bid", 1, Buy, price))
			continue
		}
		price := udecimal.MustFromInt64(midPrice+1+int64(level), 0)
		warmup = append(warmup, buildCommand("warm-near-ask", 2, Sell, price))
	}

	for i := range outerCount {
		level := i % benchmarkWarmOuterLevelCount
		if i%2 == 0 {
			price := udecimal.MustFromInt64(midPrice-1-int64(benchmarkWarmTopLevelCount+level), 0)
			warmup = append(warmup, buildCommand("warm-outer-bid", 3, Buy, price))
			continue
		}
		price := udecimal.MustFromInt64(midPrice+1+int64(benchmarkWarmTopLevelCount+level), 0)
		warmup = append(warmup, buildCommand("warm-outer-ask", 4, Sell, price))
	}

	for i := range measuredCount {
		patternPos := i % benchmarkProductionPatternCount

		if patternPos < 14 {
			if patternPos%2 == 0 {
				price := udecimal.MustFromInt64(midPrice+1, 0)
				measured[i] = buildCommand("prod-cross-buy", 10, Buy, price)
				continue
			}
			price := udecimal.MustFromInt64(midPrice-1, 0)
			measured[i] = buildCommand("prod-cross-sell", 11, Sell, price)
			continue
		}

		replenishLevel := (i / benchmarkProductionPatternCount) % benchmarkWarmTopLevelCount
		if patternPos%2 == 0 {
			price := udecimal.MustFromInt64(midPrice-1-int64(replenishLevel), 0)
			measured[i] = buildCommand("prod-rest-bid", 12, Buy, price)
			continue
		}
		price := udecimal.MustFromInt64(midPrice+1+int64(replenishLevel), 0)
		measured[i] = buildCommand("prod-rest-ask", 13, Sell, price)
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
	marketID := marketBTC
	cmd := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "bench-market-create",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmd.SetPayload(&protocol.CreateMarketParams{
		MinLotSize: udecimal.Zero,
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

	const poolSize = 1000000
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
			UserID:    uint64(rng.Intn(1000) + 1),
			MarketID:  marketID,
			CommandID: fmt.Sprintf("o-%d-%d", i, rng.Int63()),
			Timestamp: time.Now().UnixNano(),
		}
		_ = c.SetPayload(&protocol.PlaceOrderParams{
			OrderID:   c.CommandID,
			Side:      side,
			OrderType: protocol.OrderTypeLimit,
			Price:     priceCache[priceIdx],
			Size:      sizeOne,
		})
		cmdPool[i] = c
	}

	b.ResetTimer()
	b.ReportAllocs()

	// Submit asynchronously and measure end-to-end processing through the engine.
	for i := range b.N {
		cmdIdx := i % poolSize
		_ = engine.SubmitAsync(ctx, cmdPool[cmdIdx])
	}

	// Send a sentinel query to ensure all preceding commands are processed
	query := &protocol.Query{
		Type:     protocol.QueryGetStats,
		MarketID: marketID,
		Payload:  &protocol.GetStatsRequest{MarketID: marketID},
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
	marketID := marketBTC
	cmd := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		UserID:    1,
		MarketID:  marketID,
		CommandID: "bench-market-create-2",
		Timestamp: time.Now().UnixNano(),
	}
	_ = cmd.SetPayload(&protocol.CreateMarketParams{
		MinLotSize: udecimal.Zero,
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

	const poolSize = 5000000
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
			UserID:    (rng.Uint64() % 1000) + 1,
			MarketID:  marketID,
			CommandID: fmt.Sprintf("order-%d-%d", i, rng.Int63()),
			Timestamp: time.Now().UnixNano(),
		}
		_ = c.SetPayload(&protocol.PlaceOrderParams{
			OrderID:   c.CommandID,
			Side:      side,
			OrderType: protocol.OrderTypeLimit,
			Price:     priceCache[priceIdx],
			Size:      sizeOne,
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

	// Wait for processing to finish
	query := &protocol.Query{
		Type:     protocol.QueryGetStats,
		MarketID: marketID,
		Payload:  &protocol.GetStatsRequest{MarketID: marketID},
	}
	if f, err := engine.Query(ctx, query); err == nil {
		_, _ = f.Wait(ctx)
	}

	b.StopTimer()
	_ = engine.Shutdown(context.Background())
}
