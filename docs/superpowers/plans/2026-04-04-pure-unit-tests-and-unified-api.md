# Pure Unit Tests & Unified Submission API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor `order_book_test.go` to test business logic handlers directly, then remove all convenience shims from `engine.go` to leave `Submit` and `SubmitAsync` as the sole entry points.

**Architecture:** 
1. **The Unit Way**: `order_book_test.go` helpers (`testPlace`, `testCancel`, `testAmend`) will call `book.handlePlaceOrder` etc. directly, bypassing `processCommand` and the `Command` envelope.
2. **Unified Entry Point**: Remove `PlaceOrder`, `CreateMarket`, etc. from `MatchingEngine`. Tests that need integration-level submission (like `engine_test.go`) will manually construct and submit `protocol.Command` envelopes.

**Tech Stack:** Go, udecimal, testify.

---

### Task 1: Refactor `order_book_test.go` Helpers

**Files:**
- Modify: `order_book_test.go`

- [ ] **Step 1: Modify `testPlace`**
Change `testPlace` to call `book.handlePlaceOrder` directly instead of building an `InputEvent` for `processCommand`. Note that `handlePlaceOrder` expects `resp chan<- any`, which can be `nil` for these tests.

```go
// testPlace calls the business logic handler synchronously.
func testPlace(book *OrderBook, cmd *protocol.PlaceOrderParams) {
	// We pass cmd.Timestamp or a default value since the envelope is bypassed
	ts := cmd.Timestamp
	if ts == 0 {
		ts = 1 // Default for legacy tests
	}
	book.handlePlaceOrder(cmd.CommandID, ts, cmd, nil)
}
```

- [ ] **Step 2: Modify `testCancel`**
Change `testCancel` to call `book.handleCancelOrder` directly.

```go
// testCancel calls the business logic handler synchronously.
func testCancel(book *OrderBook, cmd *protocol.CancelOrderParams) {
	ts := cmd.Timestamp
	if ts == 0 {
		ts = 1
	}
	book.handleCancelOrder(cmd.CommandID, ts, cmd, nil)
}
```

- [ ] **Step 3: Modify `testAmend`**
Change `testAmend` to call `book.handleAmendOrder` directly.

```go
// testAmend calls the business logic handler synchronously.
func testAmend(book *OrderBook, cmd *protocol.AmendOrderParams) {
	ts := cmd.Timestamp
	if ts == 0 {
		ts = 1
	}
	book.handleAmendOrder(cmd.CommandID, ts, cmd, nil)
}
```

- [ ] **Step 4: Run unit tests**
Ensure the direct handler calls work exactly as before.
Run: `go test -race -v ./order_book_test.go ./order_book.go ./helper.go ./models.go ./snapshot.go ./error.go ./logger.go ./order_book_log.go ./queue.go`
Expected: PASS

### Task 2: Remove Shims from `engine.go`

**Files:**
- Modify: `engine.go`

- [ ] **Step 1: Remove all convenience shims**
Delete the following methods from `engine.go`:
- `CreateMarket`
- `PlaceOrder`
- `CancelOrder`
- `AmendOrder`
- `SuspendMarket`
- `ResumeMarket`
- `UpdateConfig`
- `SendUserEvent`

- [ ] **Step 2: Verify build failure**
Run: `go build ./...`
Expected: FAIL (because tests and examples still use these methods)

### Task 3: Refactor Tests to use `Submit` and `SubmitAsync`

**Files:**
- Modify: `engine_test.go`, `engine_user_event_test.go`, `engine_regression_test.go`, `order_book_bench_test.go`

- [ ] **Step 1: Refactor `engine_test.go`**
For every call to a removed shim, replace it with manual envelope construction and `Submit`/`SubmitAsync`. 
*Note: Due to the file size, use search-and-replace scripts or manual refactoring to construct `protocol.Command` and call `SetPayload`.*

Example replacement for `PlaceOrder`:
```go
cmd := &protocol.Command{CommandID: order1.CommandID, MarketID: market1, Timestamp: order1.Timestamp}
_ = cmd.SetPayload(order1)
err = engine.SubmitAsync(ctx, cmd)
```

Example replacement for `CreateMarket`:
```go
cmd := &protocol.Command{CommandID: "create-1", MarketID: market1, Timestamp: time.Now().UnixNano()}
_ = cmd.SetPayload(&protocol.CreateMarketParams{UserID: 1, MinLotSize: "0.01"})
future, err := engine.Submit(ctx, cmd)
```

- [ ] **Step 2: Refactor `engine_user_event_test.go`**
Replace `SendUserEvent` and `PlaceOrder` calls.

- [ ] **Step 3: Refactor `engine_regression_test.go`**
Replace `CreateMarket` and `ResumeMarket` calls.

- [ ] **Step 4: Refactor `order_book_bench_test.go`**
Replace `CreateMarket` calls in benchmarks.

- [ ] **Step 5: Run all tests**
Run: `go test -race ./...`
Expected: PASS

### Task 4: Clean up Documentation and Final Verification

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update `README.md` Usage section**
Change the examples to show the "Envelope-First" pattern using `Submit` and `SubmitAsync`. Show how to use `Command.SetPayload()`.

- [ ] **Step 2: Run final checks**
Run: `make check`
Expected: PASS
