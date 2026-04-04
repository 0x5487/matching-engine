# Fix Test Failures Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix multiple functional test failures in `order_book_test.go`, `engine_test.go`, and `engine_user_event_test.go` by correcting timestamp validation, ensuring metadata consistency, and using unique market IDs.

**Architecture:**
1.  **Protocol Correction:** Update binary serialization for all command param types to include `Timestamp`.
2.  **Validation Reinforcement:** Add explicit `Timestamp > 0` checks in all `OrderBook` and `MatchingEngine` command handlers.
3.  **Metadata Alignment:** Ensure all emitted logs use `cmd.Timestamp` and `cmd.CommandID` from the envelope `protocol.Command` where appropriate, while still respecting payload-level overrides if required by specific business logic tests.
4.  **Test Isolation:** Refactor `engine_test.go` to use unique market IDs (e.g., prefixing with subtest name) to prevent `market_already_exists` errors.
5.  **Response Reliability:** Ensure all code paths that handle `Future`-based commands (like `CreateMarket`) always send a response (success or error) to avoid timeouts.

**Tech Stack:** Go (1.26), udecimal, testify, Disruptor (RingBuffer).

---

### Task 1: Protocol Update - Include Timestamp in Params Binary Format

**Files:**
- Modify: `protocol/command.go`

- [ ] **Step 1: Add Timestamp to `PlaceOrderParams` serialization**
- [ ] **Step 2: Add Timestamp to `CancelOrderParams` serialization**
- [ ] **Step 3: Add Timestamp to `AmendOrderParams` serialization**
- [ ] **Step 4: Add Timestamp to `CreateMarketParams` serialization**
- [ ] **Step 5: Add Timestamp to `SuspendMarketParams` serialization**
- [ ] **Step 6: Add Timestamp to `ResumeMarketParams` serialization**
- [ ] **Step 7: Add Timestamp to `UpdateConfigParams` serialization**
- [ ] **Step 8: Add Timestamp to `UserEventParams` serialization**
- [ ] **Step 9: Run `go test ./protocol/...` to verify serialization tests still pass (or update them)**

---

### Task 2: Validation - Enforce Positive Timestamps in Handlers

**Files:**
- Modify: `order_book.go`
- Modify: `engine.go`

- [ ] **Step 1: Add validation to `OrderBook.handlePlaceOrder`, `handleCancelOrder`, `handleAmendOrder` in `order_book.go`**
- [ ] **Step 2: Add validation to `MatchingEngine.handleUserEvent` in `engine.go`**
- [ ] **Step 3: Ensure `OrderBook.processCommand` correctly passes envelope timestamp on unmarshal failure**

---

### Task 3: Fix `order_book_test.go` Failures

**Files:**
- Modify: `order_book_test.go`

- [ ] **Step 1: Fix `RejectInvalidPlacePayloadUsesCommandTimestamp` logic in `order_book.go`**
- [ ] **Step 2: Run `go test -v order_book_test.go` to verify validation tests pass**

---

### Task 4: Fix `engine_test.go` Market Collision Failures

**Files:**
- Modify: `engine_test.go`

- [ ] **Step 1: Update market constants to be unique or use a helper to generate unique IDs**
- [ ] **Step 2: Add `defer engine.Shutdown(ctx)` where missing**

---

### Task 5: Final Verification

- [ ] **Step 1: Run `make check`**
- [ ] **Step 2: Verify all functional tests pass**
