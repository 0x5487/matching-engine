# Task 5 Refactor Tests, Examples and Final Quality Check Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor tests, examples, and documentation to use the new `xxxParams` struct-based API and verify system integrity.

**Architecture:** Update all test files to use the single-struct argument for `MatchingEngine` methods, and update `README.md` and `engine.go` internal unmarshaling.

**Tech Stack:** Go (Matching Engine)

---

### Task 1: Fix `engine.go` Internal Unmarshaling

**Files:**
- Modify: `engine.go`

- [ ] **Step 1: Replace `engine.serializer.Unmarshal` with `payload.UnmarshalBinary`**
Since `engine.serializer` was removed from the struct but its usages remain, this is a compilation error.
Replace all occurrences in `commandTimestamp`, `commandOrderID`, and `commandUserID`.

- [ ] **Step 2: Verify `PlaceOrderBatch` uses `cmd.MarshalBinary()`**
It should already be updated, but confirm.

### Task 2: Refactor `engine_test.go`

**Files:**
- Modify: `engine_test.go`

- [ ] **Step 1: Update `CreateMarket` calls**
From: `engine.CreateMarket(ctx, commandID, userID, marketID, minLotSize, timestamp)`
To: `engine.CreateMarket(ctx, &protocol.CreateMarketParams{CommandID: commandID, UserID: userID, MarketID: marketID, MinLotSize: minLotSize, Timestamp: timestamp})`

- [ ] **Step 2: Update `PlaceOrder` calls**
From: `engine.PlaceOrder(ctx, marketID, params)`
To: `params.MarketID = marketID; engine.PlaceOrder(ctx, params)` or similar.

- [ ] **Step 3: Update `CancelOrder` calls**
From: `engine.CancelOrder(ctx, marketID, params)`
To: `params.MarketID = marketID; engine.CancelOrder(ctx, params)`

- [ ] **Step 4: Update `SuspendMarket`, `ResumeMarket`, `UpdateConfig` calls**
Similar to `CreateMarket`.

### Task 3: Refactor `order_book_test.go` and `order_book_iceberg_test.go`

**Files:**
- Modify: `order_book_test.go`, `order_book_iceberg_test.go`

- [ ] **Step 1: Update any calls that manually construct `protocol.Command`**
Ensure they use `params.MarshalBinary()`.

### Task 4: Refactor other test files

**Files:**
- Modify: `engine_regression_test.go`, `engine_user_event_test.go`, `engine_context_test.go`, `order_book_bench_test.go`

- [ ] **Step 1: Update calls in `engine_regression_test.go`**
- [ ] **Step 2: Update calls in `engine_user_event_test.go`**
- [ ] **Step 3: Update calls in `engine_context_test.go`**
- [ ] **Step 4: Update `order_book_bench_test.go`**

### Task 5: Update Documentation

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update API examples in `README.md`**

### Task 6: Final Verification

- [ ] **Step 1: Run `go build ./...`**
- [ ] **Step 2: Run `make check` (or `go test -race ./...`)**
- [ ] **Step 3: Fix any remaining linting or test failures**
