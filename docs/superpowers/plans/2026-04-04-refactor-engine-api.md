# Matching Engine API Refactoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Simplify the `MatchingEngine` API by replacing `GetStats`/`Depth` with a unified `Query` method and hiding low-level implementation details like `OnEvent` and `EnqueueCommand`.

**Architecture:** We are adopting a strict CQRS separation at the engine API layer. `Submit`/`SubmitAsync` will handle state-mutating commands, while `Query` will handle non-mutating reads. Internal RingBuffer event handlers and enqueueing methods will be privatized to prevent public API misuse.

**Tech Stack:** Go

---

### Task 1: Migrate Tests to use Unified `Query` Method

**Files:**
- Modify: `engine_test.go`
- Modify: `order_book_bench_test.go`

- [ ] **Step 1: Update `engine_test.go` to use `Query`**

Replace all instances of `GetStats(ctx, marketID)` and `Depth(ctx, marketID, limit)` with the new `Query` signature.

```bash
sed -i 's/future, e := engine.GetStats/future, e := engine.Query/g' engine_test.go
sed -i 's/future, err := engine.GetStats(ctx, market1)/future, err := engine.Query(ctx, \&protocol.GetStatsRequest{MarketID: market1})/g' engine_test.go
sed -i 's/future, err := engine.GetStats(ctx, "NON-EXISTENT")/future, err := engine.Query(ctx, \&protocol.GetStatsRequest{MarketID: "NON-EXISTENT"})/g' engine_test.go
sed -i 's/statsFuture, err := engine.GetStats(ctx, marketID)/statsFuture, err := engine.Query(ctx, \&protocol.GetStatsRequest{MarketID: marketID})/g' engine_test.go
sed -i 's/f, e := engine.GetStats(ctx, marketID)/f, e := engine.Query(ctx, \&protocol.GetStatsRequest{MarketID: marketID})/g' engine_test.go
sed -i 's/futureDepth, err := engine.Depth(ctx, "NON-EXISTENT", 10)/futureDepth, err := engine.Query(ctx, \&protocol.GetDepthRequest{MarketID: "NON-EXISTENT", Limit: 10})/g' engine_test.go
# Catch the ones that just did s/future, e := engine.Query/g without replacing args:
sed -i 's/future, e := engine.Query(ctx, market1)/future, e := engine.Query(ctx, \&protocol.GetStatsRequest{MarketID: market1})/g' engine_test.go
sed -i 's/future, e := engine.Query(ctx, market2)/future, e := engine.Query(ctx, \&protocol.GetStatsRequest{MarketID: market2})/g' engine_test.go
```

- [ ] **Step 2: Update `order_book_bench_test.go` to use `Query`**

```bash
sed -i 's/if f, err := engine.GetStats(ctx, marketID); err == nil/if f, err := engine.Query(ctx, \&protocol.GetStatsRequest{MarketID: marketID}); err == nil/g' order_book_bench_test.go
```

- [ ] **Step 3: Run test to verify it fails (Compile Error)**

Run: `go test -v ./...`
Expected: FAIL with "engine.Query undefined" and "engine.GetStats undefined".

- [ ] **Step 4: Commit**

```bash
git add engine_test.go order_book_bench_test.go
git commit -m "test: migrate tests to use unified Query method"
```

### Task 2: Implement Unified `Query` Method in Engine

**Files:**
- Modify: `engine.go`

- [ ] **Step 1: Replace `GetStats` and `Depth` with `Query` in `engine.go`**

Delete the `GetStats` and `Depth` methods in `engine.go` and replace them with:

```go
// Query executes a read-only request against the matching engine.
// Supported requests include *protocol.GetDepthRequest and *protocol.GetStatsRequest.
func (engine *MatchingEngine) Query(
	ctx context.Context,
	req any,
) (*Future[any], error) {
	if engine.isShutdown.Load() {
		return nil, ErrShutdown
	}

	respChan := engine.acquireResponseChannel()

	if err := engine.enqueueQueryWithResponse(ctx, req, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}

	return &Future[any]{
		engine:   engine,
		respChan: respChan,
	}, nil
}
```

- [ ] **Step 2: Update type assertions in `engine_test.go` and `order_book_bench_test.go`**

Since `Query` returns `*Future[any]`, tests must cast the result after waiting.

In `engine_test.go`:
```bash
sed -i 's/stats, err := statsFuture.Wait(ctx)/res, err := statsFuture.Wait(ctx)\n\t\trequire.NoError(t, err)\n\t\tstats := res.(\*protocol.GetStatsResponse)/g' engine_test.go
```
*Note: Depending on exact usage, manually ensure `Wait(ctx)` results are cast to `(*protocol.GetStatsResponse)` or `(*protocol.GetDepthResponse)`.*

In `order_book_bench_test.go`:
```bash
sed -i 's/if _, err := f.Wait(ctx); err != nil/if res, err := f.Wait(ctx); err != nil {\n\t\t\t\t\treturn\n\t\t\t\t}\n\t\t\t\t_ = res.(\*protocol.GetStatsResponse)/g' order_book_bench_test.go
```

- [ ] **Step 3: Run test to verify it passes**

Run: `go test -v ./...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add engine.go engine_test.go order_book_bench_test.go
git commit -m "feat: implement unified Query method and remove GetStats/Depth"
```

### Task 3: Privatize `EnqueueCommand` and `EnqueueCommandBatch`

**Files:**
- Modify: `engine.go`
- Modify: `engine_context_test.go`
- Modify: `engine_user_event_test.go`
- Modify: `order_book_bench_test.go`

- [ ] **Step 1: Update Tests to use private methods**

Update the tests to call `enqueueCommand` and `enqueueCommandBatch`.

```bash
sed -i 's/\.EnqueueCommand(/.enqueueCommand(/g' engine_context_test.go engine_user_event_test.go order_book_bench_test.go
sed -i 's/\.EnqueueCommandBatch(/.enqueueCommandBatch(/g' order_book_bench_test.go
```

- [ ] **Step 2: Run test to verify it fails (Compile Error)**

Run: `go test -v ./...`
Expected: FAIL with "engine.enqueueCommand undefined"

- [ ] **Step 3: Rename methods in `engine.go`**

Rename `EnqueueCommand` to `enqueueCommand` and `EnqueueCommandBatch` to `enqueueCommandBatch`. 
Update all internal usages within `engine.go`:
- Update `SubmitAsync` to call `engine.enqueueCommand(ctx, cmd)`
- Update `PlaceOrderBatch` to call `engine.enqueueCommandBatch(ctx, protoCmds)`

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add engine.go engine_context_test.go engine_user_event_test.go order_book_bench_test.go
git commit -m "refactor: privatize enqueue methods"
```

### Task 4: Hide `OnEvent` via internal handler struct

**Files:**
- Modify: `engine.go`

- [ ] **Step 1: Create `engineEventHandler` in `engine.go`**

In `engine.go`, define a private struct to satisfy the `EventHandler` interface and rename `engine.OnEvent` to `engine.onEvent`.

```go
// engineEventHandler is a private wrapper to hide the OnEvent method from the public API.
type engineEventHandler struct {
	engine *MatchingEngine
}

func (h *engineEventHandler) OnEvent(ev *InputEvent) {
	h.engine.onEvent(ev)
}
```

- [ ] **Step 2: Update `NewMatchingEngine` and rename `OnEvent`**

In `engine.go` inside `NewMatchingEngine`:
```go
	engine.ring = NewRingBuffer(defaultRingBufferSize, &engineEventHandler{engine})
```

Rename `func (engine *MatchingEngine) OnEvent(ev *InputEvent)` to `func (engine *MatchingEngine) onEvent(ev *InputEvent)`.

- [ ] **Step 3: Run test to verify and fix if tests break**

Run: `go test -v ./...`
If any tests are calling `engine.OnEvent` directly, update them to call `engine.onEvent`.

- [ ] **Step 4: Commit**

```bash
git add engine.go
git commit -m "refactor: hide OnEvent from public engine interface"
```

### Task 5: Update `README.md` Documentation

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update Command Semantics in `README.md`**

Replace the bullet point regarding `GetStats` and `Depth`:

```markdown
- `GetStats()` and `Depth()` return `ErrNotFound` immediately when the market does not exist.
```
With:
```markdown
- The `Query()` method (e.g., for `protocol.GetStatsRequest` or `protocol.GetDepthRequest`) returns `ErrNotFound` immediately when the market does not exist.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: update README for unified Query API"
```