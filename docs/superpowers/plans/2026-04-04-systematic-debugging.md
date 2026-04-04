# Systematic Debugging Plan: Pure Unit Tests & Unified API

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor `order_book_test.go` to test business logic handlers directly (The Unit Way), ensuring stable, isolated tests before removing the legacy API shims from `engine.go`.

**Architecture:** 
1. **The Unit Way**: `order_book_test.go` helpers (`testPlace`, `testCancel`, `testAmend`) will call `book.handlePlaceOrder` etc. directly, bypassing `processCommand` and the `Command` envelope.
2. **Unified Entry Point**: Remove `PlaceOrder`, `CreateMarket`, etc. from `MatchingEngine`. Tests that need integration-level submission (like `engine_test.go`) will manually construct and submit `protocol.Command` envelopes.
3. **Systematic Approach**: We will use a systematic approach to debug any failing tests, ensuring we understand the root cause before applying fixes.

**Tech Stack:** Go, udecimal, testify.

---

### Task 1: Refactor `order_book_test.go` Helpers (TDD)

**Files:**
- Modify: `order_book_test.go`

- [ ] **Step 1: Modify `testPlace`**
Change `testPlace` to call `book.handlePlaceOrder` directly instead of building an `InputEvent` for `processCommand`. 

```go
// testPlace calls the business logic handler synchronously.
func testPlace(book *OrderBook, cmd *protocol.PlaceOrderParams) {
	// Provide a default timestamp if not set in the test
	ts := cmd.Timestamp
	if ts == 0 {
		ts = 1 
	}
	// Use the param's CommandID or a default one
	cmdID := cmd.CommandID
	if cmdID == "" {
		cmdID = "cmd-" + cmd.OrderID
	}
	book.handlePlaceOrder(cmdID, ts, cmd, nil)
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
	cmdID := cmd.CommandID
	if cmdID == "" {
		cmdID = "cmd-" + cmd.OrderID
	}
	book.handleCancelOrder(cmdID, ts, cmd, nil)
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
	cmdID := cmd.CommandID
	if cmdID == "" {
		cmdID = "cmd-" + cmd.OrderID
	}
	book.handleAmendOrder(cmdID, ts, cmd, nil)
}
```

- [ ] **Step 4: Run unit tests**
Ensure the direct handler calls work exactly as before. If any test fails, STOP and investigate the root cause (e.g., are we bypassing a validation that was previously done in `processCommand`?).
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

- [ ] **Step 1: Refactor `engine_user_event_test.go`**
Replace `SendUserEvent` and `PlaceOrder` calls with explicit `Submit` and `SubmitAsync` calls. Create helper functions if necessary to keep the code clean.

- [ ] **Step 2: Refactor `engine_regression_test.go`**
Replace `CreateMarket` and `ResumeMarket` calls.

- [ ] **Step 3: Refactor `order_book_bench_test.go`**
Replace `CreateMarket` calls in benchmarks.

- [ ] **Step 4: Refactor `engine_test.go`**
This is the largest file. Methodically replace the shim calls with `Submit` or `SubmitAsync`. Consider creating local helper functions (e.g., `submitPlaceOrder`, `submitCreateMarket`) to encapsulate the envelope creation logic and reduce boilerplate. *Ensure that market IDs are isolated per subtest to prevent `market_already_exists` errors.*

- [ ] **Step 5: Run all tests**
Run: `go test -race ./...`
If tests fail, use systematic debugging to find the root cause (e.g., missing metadata in the envelope, incorrect channel handling).
Expected: PASS

### Task 4: Clean up Documentation and Final Verification

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update `README.md` Usage section**
Change the examples to show the "Envelope-First" pattern using `Submit` and `SubmitAsync`. Show how to use `Command.SetPayload()`.

- [ ] **Step 2: Run final checks**
Run: `make check`
Expected: PASS