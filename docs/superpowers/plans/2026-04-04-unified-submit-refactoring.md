# Unified MatchingEngine Submission API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor `MatchingEngine` to provide a unified, metadata-purified submission API using `Submit` and `SubmitAsync`, removing legacy specific methods and ensuring handlers use the `protocol.Command` envelope for metadata.

**Architecture:** The `MatchingEngine` will now accept `protocol.Command` directly. Metadata like `CommandID`, `MarketID`, and `Timestamp` are extracted from the command envelope rather than the business payloads. The new API supports both synchronous (via `Future`) and high-performance asynchronous paths.

**Tech Stack:** Go (Golang)

---

### Task 1: Metadata Purification and Helper Cleanup

**Files:**
- Modify: `/home/jason/_repository/public/matching-engine/engine.go`

- [ ] **Step 1: Remove redundant metadata extraction helpers**
Remove `commandTimestamp`, `commandOrderID`, and `commandUserID` as they attempt to unmarshal payloads to get metadata that is now available in the `protocol.Command` envelope.

- [ ] **Step 2: Update `rejectCommand` and `rejectCommandWithMarket`**
Update these methods to take metadata from the `protocol.Command` envelope. Since `commandOrderID` and `commandUserID` were removed, we need to decide how to populate these in the `RejectLog`. For now, use "n/a" or 0 if they are not easily available without unmarshalling (or unmarshal only if absolutely necessary for the log, but the objective is to purify). Actually, `OrderID` and `UserID` are business data, so they might still need unmarshalling if we want them in logs, but `CommandID`, `MarketID`, and `Timestamp` are definitely in the envelope.

- [ ] **Step 3: Update `processCommand`**
Ensure `processCommand` uses `cmd.CommandID` and `cmd.Timestamp` directly.

### Task 2: Implement Unified Submission API

**Files:**
- Modify: `/home/jason/_repository/public/matching-engine/engine.go`

- [ ] **Step 1: Implement `Submit`**
```go
// Submit sends a command to the engine and returns a Future for the result.
func (engine *MatchingEngine) Submit(ctx context.Context, cmd *protocol.Command) (*Future[any], error) {
	if engine.isShutdown.Load() {
		return nil, ErrShutdown
	}

	respChan := engine.acquireResponseChannel()
	if err := engine.enqueueCommandWithResponse(ctx, cmd, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}

	return &Future[any]{
		engine:   engine,
		respChan: respChan,
	}, nil
}
```

- [ ] **Step 2: Implement `SubmitAsync`**
```go
// SubmitAsync sends a command to the engine without waiting for a result.
func (engine *MatchingEngine) SubmitAsync(ctx context.Context, cmd *protocol.Command) error {
	return engine.EnqueueCommand(ctx, cmd)
}
```

### Task 3: Refactor Engine Handlers

**Files:**
- Modify: `/home/jason/_repository/public/matching-engine/engine.go`

- [ ] **Step 1: Refactor `handleCreateMarket`**
Extract logic into a modular function that takes metadata and params explicitly. Ensure it sends `true` or an `error` to the response channel.

- [ ] **Step 2: Update `handleUserEvent`**
Ensure it uses `cmd.CommandID` and `cmd.Timestamp`.

### Task 4: Remove Legacy API Methods

**Files:**
- Modify: `/home/jason/_repository/public/matching-engine/engine.go`

- [ ] **Step 1: Delete legacy methods**
Remove `PlaceOrder`, `CancelOrder`, `AmendOrder`, `CreateMarket`, `SuspendMarket`, `ResumeMarket`, `UpdateConfig`, and `SendUserEvent`.
Keep `PlaceOrderBatch` (as instructed).

### Task 5: Verification

- [ ] **Step 1: Verify compilation**
Run: `go build ./...`
Expected: Success.

- [ ] **Step 2: Run existing tests**
Run: `go test ./...`
Expected: Most tests should pass, though some might need updates if they call the removed methods.
