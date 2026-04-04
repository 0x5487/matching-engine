# Global Test and Example Refactoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor all tests and documentation to use the new `Submit`/`SubmitAsync` API with `protocol.Command` and `SetPayload`.

**Architecture:** Transition from direct `MatchingEngine` methods (e.g., `PlaceOrder`) to a unified `Submit` entry point. Use `protocol.Command` as an envelope and `protocol.Params` for payload data.

**Tech Stack:** Go, Testify, udecimal.

---

### Task 1: Refactor `order_book_test.go` Helpers

**Files:**
- Modify: `order_book_test.go`

- [ ] **Step 1: Refactor `testPlace`, `testCancel`, `testAmend` to use `SetPayload`.**
- [ ] **Step 2: Update `TestLimitOrders` and other tests in `order_book_test.go` that manually construct `InputEvent`.**

### Task 2: Refactor `engine_test.go`

**Files:**
- Modify: `engine_test.go`

- [ ] **Step 1: Replace `CreateMarket` calls with `Submit`.**
- [ ] **Step 2: Replace `PlaceOrder` calls with `SubmitAsync` or `Submit`.**
- [ ] **Step 3: Replace `CancelOrder`, `SuspendMarket`, `ResumeMarket`, `UpdateConfig` calls with `Submit`.**
- [ ] **Step 4: Update `TestCommandAndEngineIDPropagation` and `TestManagement_LateResponsePollution`.**

### Task 3: Refactor `engine_regression_test.go` and `engine_user_event_test.go`

**Files:**
- Modify: `engine_regression_test.go`
- Modify: `engine_user_event_test.go`

- [ ] **Step 1: Update all `MatchingEngine` method calls to the new `Submit` pattern.**
- [ ] **Step 2: Fix any `protocol.Params` struct field issues (e.g., `CommandID`, `MarketID`, `Timestamp` moved to `Command` envelope).**

### Task 4: Refactor `engine_context_test.go`

**Files:**
- Modify: `engine_context_test.go`

- [ ] **Step 1: Update `CreateMarket` and `PlaceOrder` calls to `Submit`.**

### Task 5: Update `README.md`

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update usage examples to show the new `Submit` API.**

### Task 6: Final Verification

- [ ] **Step 1: Run `go build ./...`**
- [ ] **Step 2: Run `go test ./...`**
- [ ] **Step 3: Run `make check`**
