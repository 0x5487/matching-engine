# Fix response signaling in OrderBook handlers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ensure OrderBook handlers send responses through the provided channel to support the unified Submit (Future) API and prevent hangs.

**Architecture:** Modify `OrderBook` handlers to capture outcomes (success/failure) and send them back via the `resp` channel if it exists. Update `processCommand` to handle unmarshaling errors by responding immediately.

**Tech Stack:** Go, udecimal, testify

---

### Task 1: Research and Setup

- [ ] **Step 1: Verify current behavior of handlers and processCommand**
  Read `order_book.go` to confirm which paths are missing response signaling.
  Confirm `handlePlaceOrder`, `handleCancelOrder`, `handleAmendOrder` signatures and internal calls.

- [ ] **Step 2: Check protocol definitions for error types**
  Check `protocol/types.go` or similar to see if there are specific error constants to use.

### Task 2: Update OrderBook.processCommand

- [ ] **Step 1: Add respondError to unmarshaling failure paths**
  Modify `order_book.go`: `OrderBook.processCommand` to call `book.respondError(ev, err)` when `UnmarshalBinary` fails for `CmdPlaceOrder`, `CmdCancelOrder`, and `CmdAmendOrder`.

```go
// Example for CmdPlaceOrder
		if err := payload.UnmarshalBinary(cmd.Payload); err != nil {
			placeOrderCmdPool.Put(payload)
			book.rejectInvalidPayload(cmd.CommandID, "unknown", 0, protocol.RejectReasonInvalidPayload, cmd.Timestamp)
			book.respondError(ev, err) // Add this
			return
		}
```

### Task 3: Refactor placeOrder, cancelOrder, amendOrder to return results

- [ ] **Step 1: Modify placeOrder to return (*Order, error)**
  Update `placeOrder` to return the `Order` object on success (resting or matched) or an error if rejected.
  *Note:* Be careful with `releaseOrder`. If we return the order, we might need to be careful about when it's released. Actually, for a response, maybe we should return a snapshot or just `true` if it was accepted. The instructions say "send the result object (e.g., the new Order)". I'll send the `Order` pointer but note that it might be released. Better: return a copy or just success/fail as the engine handles the logs anyway. I'll stick to the instruction: "Success: send the result object (e.g., the new Order)".

- [ ] **Step 2: Modify cancelOrder to return error**
  Update `cancelOrder` to return `nil` on success or an error if order not found.

- [ ] **Step 3: Modify amendOrder to return error**
  Update `amendOrder` to return `nil` on success or an error if order not found or invalid payload.

### Task 4: Update Handlers to send responses

- [ ] **Step 1: Update handlePlaceOrder**
  Call `placeOrder`, capture result/error, and use `book.sendResponse(resp, ...)`.

- [ ] **Step 2: Update handleCancelOrder**
  Call `cancelOrder`, capture error, and use `book.sendResponse(resp, ...)`.

- [ ] **Step 3: Update handleAmendOrder**
  Call `amendOrder`, capture error, and use `book.sendResponse(resp, ...)`.

### Task 5: Update Unit Tests

- [ ] **Step 1: Update test helpers in order_book_test.go**
  Modify `testPlace`, `testCancel`, `testAmend` to accept an optional response channel and verify it if provided.

- [ ] **Step 2: Update existing tests to verify responses**
  Add assertions in `TestOrderBook_HandlePlaceOrder`, `TestOrderBook_HandleCancelOrder`, etc., to check the response channel.

- [ ] **Step 3: Run all tests**
  `go test -v order_book_test.go order_book.go ...` (or just `go test ./...`)

---
