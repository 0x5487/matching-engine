# Typed Request Engine API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the third and final phase of replacing the legacy `protocol.Command` envelope with typed requests across all remaining tests and benchmarks, then permanently remove the legacy types and bridge logic.

**Architecture:** We are replacing `engine.Submit(ctx, cmd)` with typed facade methods like `engine.PlaceOrderAsync(ctx, req)`. The tests are the last remaining callers using the legacy `protocol.Command` builder pattern. Once the callers are updated, we will remove the legacy structs (`Command`, `Params` interface) and bridge functions from `protocol` and `engine.go`.

**Tech Stack:** Go (1.25.4), Testify, Udecimal

---

### Task 1: Refactor `order_book_bench_test.go` to use Typed Requests

**Files:**
- Modify: `order_book_bench_test.go`

- [ ] **Step 1: Replace legacy Command creation in `benchmarkNewEngine`**

Update `benchmarkNewEngine` to use `engine.CreateMarket` with a `CreateMarketRequest` instead of `engine.Submit` with a legacy `Command`.

```go
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
```

- [ ] **Step 2: Inline `PlaceOrderRequest` creation and remove `benchmarkBuildPlaceOrderCommand`**

Remove the `benchmarkBuildPlaceOrderCommand` helper function entirely. The new typed request structs are clean enough to be instantiated directly where they are needed.

Update `benchmarkCrossingCommandPool` to build and return a slice of `*protocol.PlaceOrderRequest` directly.

```go
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
```

Update `benchmarkWarmBookCommandPool` similarly to inline the creation of `*protocol.PlaceOrderRequest` objects instead of using a helper. Return `([]*protocol.PlaceOrderRequest, []*protocol.PlaceOrderRequest)`.

- [ ] **Step 3: Update `benchmarkSubmitSingle` and `benchmarkSubmitBatch10` to use facade methods**

Change the parameter slice type to `[]*protocol.PlaceOrderRequest` and call `PlaceOrderAsync` / `PlaceOrderBatchAsync`.

```go
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
```

- [ ] **Step 4: Update `BenchmarkOrderBook_Match` and `BenchmarkSubmitAsyncBatch` initializers**

Replace the legacy `cmdPool` population logic with typed `PlaceOrderRequest` structs. Update the `CreateMarket` call at the top of each benchmark as well. Update `engine.SubmitAsync` calls to `engine.PlaceOrderAsync` and `engine.SubmitAsyncBatch` to `engine.PlaceOrderBatchAsync`.

```go
// ... replacing the CreateMarket block ...
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
// ...

// ... replacing the cmdPool population block ...
	cmdPool := make([]*protocol.PlaceOrderRequest, poolSize)

	for i := range poolSize {
// ... side and priceIdx logic remains the same ...
		cmdPool[i] = &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				Type:      protocol.CmdPlaceOrder,
				UserID:    uint64(rng.Intn(1000) + 1),
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
```

- [ ] **Step 5: Run tests and benchmarks**

Run: `go test -race ./...`
Run: `make bench`
Expected: PASS for both. No functional degradation.

- [ ] **Step 6: Commit**

```bash
git add order_book_bench_test.go
git commit -m "test: migrate order_book_bench_test.go to typed requests and remove obsolete helpers"
```

### Task 2: Refactor `engine_test.go` to use Typed Requests

**Files:**
- Modify: `engine_test.go`

- [ ] **Step 1: Update `createMarket` helper**

```go
func createMarket(t *testing.T, engine *MatchingEngine, marketID, minLotSize string) {
	t.Helper()
	ctx := context.Background()
	var mls udecimal.Decimal
	if minLotSize != "" {
		mls, _ = udecimal.Parse(minLotSize)
	}
	req := &protocol.CreateMarketRequest{
		BaseCommand: protocol.BaseCommand{
			Type:      protocol.CmdCreateMarket,
			UserID:    1,
			MarketID:  marketID,
			CommandID: "setup-create-" + marketID,
			Timestamp: time.Now().UnixNano(),
		},
		MinLotSize: mls,
	}
	future, err := engine.CreateMarket(ctx, req)
	require.NoError(t, err)
	_, err = future.Wait(ctx)
	require.NoError(t, err)
}
```

- [ ] **Step 2: Update `fillRingBuffer` helper**

Update it to use `PlaceOrderAsync` instead of `SubmitAsync`.

```go
func fillRingBuffer(t *testing.T, engine *MatchingEngine) {
	t.Helper()
	for i := range defaultRingBufferSize {
		err := engine.PlaceOrderAsync(context.Background(), &protocol.PlaceOrderRequest{
			BaseCommand: protocol.BaseCommand{
				CommandID: fmt.Sprintf("fill-%d", i),
			},
		})
		require.NoError(t, err)
	}
}
```

- [ ] **Step 3: Update `TestMatchingEngine_CreateMarket_*` tests**

Replace `engine.Submit(ctx, cmd)` with `engine.CreateMarket(ctx, req)` and use `protocol.CreateMarketRequest` structs throughout. Update `TestMatchingEngine_SuspendMarket`, `TestMatchingEngine_ResumeMarket`, and `TestMatchingEngine_UpdateConfig` similarly using their respective facade methods and request structs.

- [ ] **Step 4: Update `TestMatchingEngine_Enqueue*` and `TestMatchingEngine_Shutdown`**

Update the remaining tests that submit orders to use `PlaceOrderRequest` and `engine.PlaceOrderAsync`.

- [ ] **Step 5: Run tests**

Run: `go test -race ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add engine_test.go
git commit -m "test: migrate engine_test.go to typed requests"
```

### Task 3: Remove Legacy Command Bridge and Structs

**Files:**
- Modify: `protocol/command.go`
- Modify: `engine.go`

- [ ] **Step 1: Remove bridge functions from `engine.go`**

Remove `engine.Submit`, `engine.SubmitAsync`, `engine.SubmitAsyncBatch`. Remove `bridgeCommandToRequest`.

- [ ] **Step 2: Clean up `protocol/command.go`**

Remove `type Command struct`, `func (c *Command) SetPayload`, `type Params interface`, `func MarshalCommand`, `func UnmarshalCommand`, `func AcquireCommand`, `func ReleaseCommand`.
Remove the `PlaceOrderParams`, `CancelOrderParams`, etc. structs (keep the `XxxRequest` structs!).
Remove `requestToCommand` and `commandToRequest`.
Remove the `sync.Pool` definitions related to `Command` and `Params` (e.g. `commandPool`, `placeOrderPool`, etc.).

- [ ] **Step 3: Implement standalone `MarshalRequest` and `UnmarshalRequest`**

Replace the bridge implementation in `protocol/command.go` with direct binary serialization for the `XxxRequest` types, mirroring the logic previously used for `Command` + `Params`.

- [ ] **Step 4: Run tests to verify the removal didn't break serialization**

Run: `go test -race ./...`
Run: `make bench`
Expected: PASS. This verifies the new standalone `MarshalRequest`/`UnmarshalRequest` work correctly and the legacy bridge is fully removed.

- [ ] **Step 5: Commit**

```bash
git add protocol/command.go engine.go
git commit -m "refactor: remove legacy Command envelope and bridge logic"
```

### Task 4: Update Documentation

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update README.md Command Semantics section**

Update any code examples or explanations in the README that refer to `engine.Submit`, `protocol.Command`, or `SetPayload` to use the new typed requests (e.g., `PlaceOrderRequest`) and facade methods (e.g., `PlaceOrderAsync`).

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: update README to reflect typed request API"
```