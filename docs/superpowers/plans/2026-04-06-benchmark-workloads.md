# Benchmark Workloads Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add four stable end-to-end benchmarks that compare crossing and production-like warm-book workloads under single-order and batch-of-10 submission modes.

**Architecture:** Extend `order_book_bench_test.go` with shared benchmark helpers for engine setup, market creation, warmup, command generation, batch submission, sentinel draining, and final depth logging. Keep benchmark setup and warmup outside the timer and ensure single and batch modes consume equivalent command streams.

**Tech Stack:** Go, `testing.B`, current `protocol.Command` API, `SubmitAsync`, `SubmitAsyncBatch`, benchmark helpers in the existing benchmark file.

---

### Task 1: Inspect current benchmark file and define helper boundaries

**Files:**
- Modify: `order_book_bench_test.go`

- [ ] **Step 1: Identify shared setup and drain logic in the existing benchmarks**

Read:

```bash
sed -n '1,260p' order_book_bench_test.go
```

Expected: existing `BenchmarkOrderBook_Match` and `BenchmarkSubmitAsyncBatch` show repeated engine setup, market creation, command generation, sentinel query, and shutdown logic.

- [ ] **Step 2: Define helper responsibilities before editing**

Planned helpers:

```go
func benchmarkNewEngine(b *testing.B, marketID string) (context.Context, *MatchingEngine)
func benchmarkDrain(b *testing.B, ctx context.Context, engine *MatchingEngine, marketID string) *protocol.GetStatsResponse
func benchmarkLogDepth(b *testing.B, stats *protocol.GetStatsResponse)
func benchmarkCrossingCommandPool(marketID string, pairCount int) []*protocol.Command
func benchmarkWarmBookCommandPool(marketID string, warmupCount int, measuredCount int) ([]*protocol.Command, []*protocol.Command)
func benchmarkSubmitSingle(ctx context.Context, engine *MatchingEngine, cmds []*protocol.Command)
func benchmarkSubmitBatch10(ctx context.Context, engine *MatchingEngine, cmds []*protocol.Command)
```

Expected: helper boundaries keep the four public benchmarks short and ensure workload generation is reused across single and batch modes.

### Task 2: Implement shared benchmark helpers

**Files:**
- Modify: `order_book_bench_test.go`

- [ ] **Step 1: Add engine setup and drain helpers**

Implementation shape:

```go
func benchmarkNewEngine(b *testing.B, marketID string) (context.Context, *MatchingEngine) {
	ctx := context.Background()
	engine := NewMatchingEngine("bench-engine", NewDiscardPublishLog())
	cmd := &protocol.Command{Type: protocol.CmdCreateMarket, UserID: 1, MarketID: marketID, CommandID: "bench-market-create-" + marketID, Timestamp: time.Now().UnixNano()}
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
```

```go
func benchmarkDrain(b *testing.B, ctx context.Context, engine *MatchingEngine, marketID string) *protocol.GetStatsResponse {
	query := &protocol.Query{Type: protocol.QueryGetStats, MarketID: marketID, Payload: &protocol.GetStatsRequest{MarketID: marketID}}
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
```

- [ ] **Step 2: Add depth logging and submit helpers**

Implementation shape:

```go
func benchmarkLogDepth(b *testing.B, stats *protocol.GetStatsResponse) {
	b.Logf("Final Order Book State: Bids=%d levels, Asks=%d levels", stats.BidDepthCount, stats.AskDepthCount)
}
```

```go
func benchmarkSubmitSingle(ctx context.Context, engine *MatchingEngine, cmds []*protocol.Command) {
	for _, cmd := range cmds {
		_ = engine.SubmitAsync(ctx, cmd)
	}
}
```

```go
func benchmarkSubmitBatch10(ctx context.Context, engine *MatchingEngine, cmds []*protocol.Command) {
	for i := 0; i < len(cmds); i += 10 {
		end := i + 10
		if end > len(cmds) {
			end = len(cmds)
		}
		_ = engine.SubmitAsyncBatch(ctx, cmds[i:end])
	}
}
```

- [ ] **Step 3: Format the file**

Run:

```bash
gofmt -w order_book_bench_test.go
```

Expected: file formats cleanly with no syntax errors.

### Task 3: Implement crossing workload benchmarks

**Files:**
- Modify: `order_book_bench_test.go`

- [ ] **Step 1: Add a command-pool builder for the crossing workload**

Implementation shape:

```go
func benchmarkCrossingCommandPool(marketID string, pairCount int) []*protocol.Command {
	price := udecimal.MustFromInt64(10000, 0)
	size := udecimal.MustFromInt64(1, 0)
	cmds := make([]*protocol.Command, pairCount*2)
	for i := 0; i < len(cmds); i += 2 {
		// one resting sell, one matching buy
	}
	return cmds
}
```

Expected: each pair has unique command IDs and order IDs and preserves the same logical stream for both single and batch submissions.

- [ ] **Step 2: Add `BenchmarkCrossing_EndToEnd_Single`**

Implementation shape:

```go
func BenchmarkCrossing_EndToEnd_Single(b *testing.B) {
	ctx, engine := benchmarkNewEngine(b, "CROSS-USDT")
	cmds := benchmarkCrossingCommandPool("CROSS-USDT", b.N)
	b.ResetTimer()
	b.ReportAllocs()
	benchmarkSubmitSingle(ctx, engine, cmds)
	stats := benchmarkDrain(b, ctx, engine, "CROSS-USDT")
	b.StopTimer()
	benchmarkLogDepth(b, stats)
	b.ReportMetric(float64(len(cmds))/b.Elapsed().Seconds(), "orders/sec")
	_ = engine.Shutdown(context.Background())
}
```

- [ ] **Step 3: Add `BenchmarkCrossing_EndToEnd_Batch10`**

Implementation shape:

```go
func BenchmarkCrossing_EndToEnd_Batch10(b *testing.B) {
	ctx, engine := benchmarkNewEngine(b, "CROSS-BATCH-USDT")
	cmds := benchmarkCrossingCommandPool("CROSS-BATCH-USDT", b.N)
	b.ResetTimer()
	b.ReportAllocs()
	benchmarkSubmitBatch10(ctx, engine, cmds)
	stats := benchmarkDrain(b, ctx, engine, "CROSS-BATCH-USDT")
	b.StopTimer()
	benchmarkLogDepth(b, stats)
	b.ReportMetric(float64(len(cmds))/b.Elapsed().Seconds(), "orders/sec")
	_ = engine.Shutdown(context.Background())
}
```

### Task 4: Implement production warm-book workload benchmarks

**Files:**
- Modify: `order_book_bench_test.go`

- [ ] **Step 1: Add a warm-book workload builder**

Implementation shape:

```go
func benchmarkWarmBookCommandPool(marketID string, warmupCount int, measuredCount int) ([]*protocol.Command, []*protocol.Command) {
	// warmup commands create a stable book with 80% volume in top 20 levels
	// measured commands keep a 70% crossing / 30% replenishment mix
}
```

Expected:
- warmup command slice is submitted before `b.ResetTimer()`
- measured command slice is equivalent for single and batch modes
- prices are deterministic and buy/sell flow stays balanced

- [ ] **Step 2: Add `BenchmarkProductionWarmBook_EndToEnd_Single`**

Implementation shape:

```go
func BenchmarkProductionWarmBook_EndToEnd_Single(b *testing.B) {
	ctx, engine := benchmarkNewEngine(b, "PROD-USDT")
	warmupCmds, measuredCmds := benchmarkWarmBookCommandPool("PROD-USDT", 4000, b.N)
	benchmarkSubmitSingle(ctx, engine, warmupCmds)
	_ = benchmarkDrain(b, ctx, engine, "PROD-USDT")
	b.ResetTimer()
	b.ReportAllocs()
	benchmarkSubmitSingle(ctx, engine, measuredCmds)
	stats := benchmarkDrain(b, ctx, engine, "PROD-USDT")
	b.StopTimer()
	benchmarkLogDepth(b, stats)
	b.ReportMetric(float64(len(measuredCmds))/b.Elapsed().Seconds(), "orders/sec")
	_ = engine.Shutdown(context.Background())
}
```

- [ ] **Step 3: Add `BenchmarkProductionWarmBook_EndToEnd_Batch10`**

Implementation shape:

```go
func BenchmarkProductionWarmBook_EndToEnd_Batch10(b *testing.B) {
	ctx, engine := benchmarkNewEngine(b, "PROD-BATCH-USDT")
	warmupCmds, measuredCmds := benchmarkWarmBookCommandPool("PROD-BATCH-USDT", 4000, b.N)
	benchmarkSubmitSingle(ctx, engine, warmupCmds)
	_ = benchmarkDrain(b, ctx, engine, "PROD-BATCH-USDT")
	b.ResetTimer()
	b.ReportAllocs()
	benchmarkSubmitBatch10(ctx, engine, measuredCmds)
	stats := benchmarkDrain(b, ctx, engine, "PROD-BATCH-USDT")
	b.StopTimer()
	benchmarkLogDepth(b, stats)
	b.ReportMetric(float64(len(measuredCmds))/b.Elapsed().Seconds(), "orders/sec")
	_ = engine.Shutdown(context.Background())
}
```

- [ ] **Step 4: Format the file again**

Run:

```bash
gofmt -w order_book_bench_test.go
```

Expected: file formats cleanly after the new helpers and benchmarks are added.

### Task 5: Verify benchmark behavior

**Files:**
- Modify: `order_book_bench_test.go`
- Test: `order_book_bench_test.go`

- [ ] **Step 1: Run the new crossing benchmarks**

Run:

```bash
go test -run '^$' -bench '^(BenchmarkCrossing_EndToEnd_Single|BenchmarkCrossing_EndToEnd_Batch10)$' -benchmem .
```

Expected: both benchmarks pass, report `orders/sec`, and log shallow final depth.

- [ ] **Step 2: Run the new warm-book benchmarks**

Run:

```bash
go test -run '^$' -bench '^(BenchmarkProductionWarmBook_EndToEnd_Single|BenchmarkProductionWarmBook_EndToEnd_Batch10)$' -benchmem .
```

Expected: both benchmarks pass, report `orders/sec`, and log stable non-empty final bid and ask depths.

- [ ] **Step 3: Run race-enabled benchmark compilation check**

Run:

```bash
go test -race -run '^$' ./...
```

Expected: benchmark code compiles cleanly under the race build even if unrelated pre-existing tests fail elsewhere in the repository.

- [ ] **Step 4: Run repository checks**

Run:

```bash
make check
```

Expected: capture whether the repository still has unrelated pre-existing failures; do not silently ignore failures.
