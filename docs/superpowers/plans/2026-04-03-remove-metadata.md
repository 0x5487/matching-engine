# Remove Command Metadata Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Completely remove the `Metadata` field from the `Command` protocol and all internal SDK propagation paths to simplify the architecture and improve performance.

**Architecture:** 
1. **Protocol**: Remove `Metadata map[string]string` from `protocol.Command`.
2. **Logs**: Revert `OrderBookLog` to not include `Metadata` and simplify `NewRejectLog`/`NewAdminLog` signatures.
3. **Handlers**: Clean up `MatchingEngine` and `OrderBook` to stop passing metadata.
4. **Consistency**: Rely solely on `CommandID` for external correlation.

**Tech Stack:** Go (Refactoring, Protocol cleanup)

---

### Task 1: Clean up Protocol and Log Structures

**Files:**
- Modify: `protocol/command.go` (or `types.go` where Command is defined)
- Modify: `order_book_log.go`

- [ ] **Step 1: Remove Metadata from protocol.Command**
Find the `Command` struct and delete the `Metadata` field.

- [ ] **Step 2: Remove Metadata from OrderBookLog and simplify constructors**
Revert `OrderBookLog` struct by removing the `Metadata` field. Update `NewRejectLog` and `NewAdminLog` to remove the `metadata` parameter.

```go
// In order_book_log.go
func NewRejectLog(
    seqID uint64,
    commandID, engineID, marketID string,
    orderID string,
    userID uint64,
    reason protocol.RejectReason,
    timestamp int64, // Remove metadata param
) *OrderBookLog { ... }
```

- [ ] **Step 3: Update releaseBookLog**
Ensure `Metadata` is no longer mentioned in pool release logic.

- [ ] **Step 4: Commit**
```bash
git add protocol/ order_book_log.go
git commit -m "refactor: remove Metadata from protocol and logs"
```

---

### Task 2: Clean up SDK Propagation Paths

**Files:**
- Modify: `order_book.go`
- Modify: `engine.go`

- [ ] **Step 1: Update rejectInvalidPayload in order_book.go**
Remove the `metadata` parameter and the `_ map[string]string` placeholder. Update the internal call to `NewRejectLog`.

- [ ] **Step 2: Update management handlers in order_book.go**
Remove all `cmd.Metadata` or `ev.Cmd.Metadata` references when calling log constructors or `rejectInvalidPayload`.

- [ ] **Step 3: Update rejectCommandWithMarket in engine.go**
Remove `cmd.Metadata` from the `NewRejectLog` call.

- [ ] **Step 4: Commit**
```bash
git add order_book.go engine.go
git commit -m "refactor: remove Metadata propagation in engine and order book"
```

---

### Task 3: Update Tests and Verify

**Files:**
- Modify: `engine_test.go`
- Modify: `engine_regression_test.go`
- Modify: `order_book_test.go`

- [ ] **Step 1: Remove Metadata from test data**
Search for and remove any `Metadata: map[string]string{...}` initializations in all test files.

- [ ] **Step 2: Verify with tests and make check**
Run `go test -race ./...` and `make check`.

- [ ] **Step 3: Commit**
```bash
git add *_test.go
git commit -m "test: clean up Metadata usage in tests"
```

---

### Task 4: Final Documentation Update

- [ ] **Step 1: Update CHANGELOG.md**
Add a note about the removal of `Metadata` for better performance and separation of concerns.

- [ ] **Step 2: Commit**
```bash
git add CHANGELOG.md
git commit -m "docs: document removal of command metadata"
```
