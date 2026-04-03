# Query Future Pattern & Management Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor query interfaces (`Depth`, `GetStats`) to return Futures for better safety and consistency, and ensure all management commands resolve their Futures on deserialization failure.

**Architecture:** Leverage the existing generic `Future[T]` infrastructure. Modify the query path to return a Future instead of blocking internally. Update the OrderBook command dispatcher to report errors immediately on unmarshal failure.

**Tech Stack:** Go (Generics, sync.Pool, Context)

---

### Task 1: Fix Deserialization Response in OrderBook

**Files:**
- Modify: `order_book.go`

- [ ] **Step 1: Ensure CmdSuspendMarket reports error on unmarshal failure**
Update `processCommand` to call `respondError` if `Unmarshal` fails.

```go
case protocol.CmdSuspendMarket:
    payload := &protocol.SuspendMarketCommand{}
    if err := book.serializer.Unmarshal(cmd.Payload, payload); err != nil {
        book.rejectInvalidPayload(cmd.CommandID, "unknown", 0, protocol.RejectReasonInvalidPayload, nil, 0)
        book.respondError(ev, err) // Added
        return
    }
    book.handleSuspendMarket(ev, payload)
```

- [ ] **Step 2: Ensure CmdResumeMarket reports error on unmarshal failure**
Update `processCommand` to call `respondError` if `Unmarshal` fails.

```go
case protocol.CmdResumeMarket:
    payload := &protocol.ResumeMarketCommand{}
    if err := book.serializer.Unmarshal(cmd.Payload, payload); err != nil {
        book.rejectInvalidPayload(cmd.CommandID, "unknown", 0, protocol.RejectReasonInvalidPayload, nil, 0)
        book.respondError(ev, err) // Added
        return
    }
    book.handleResumeMarket(ev, payload)
```

- [ ] **Step 3: Ensure CmdUpdateConfig reports error on unmarshal failure**
Update `processCommand` to call `respondError` if `Unmarshal` fails.

```go
case protocol.CmdUpdateConfig:
    payload := &protocol.UpdateConfigCommand{}
    if err := book.serializer.Unmarshal(cmd.Payload, payload); err != nil {
        book.rejectInvalidPayload(cmd.CommandID, "unknown", 0, protocol.RejectReasonInvalidPayload, nil, 0)
        book.respondError(ev, err) // Added
        return
    }
    book.handleUpdateConfig(ev, payload)
```

- [ ] **Step 4: Verify and Commit**
Run: `go build ./...`
```bash
git add order_book.go
git commit -m "fix: ensure management commands resolve future on unmarshal failure"
```

---

### Task 2: Refactor GetStats to Future Pattern

**Files:**
- Modify: `engine.go`
- Modify: `engine_test.go`

- [ ] **Step 1: Update GetStats signature and implementation**
Change `GetStats` to return `(*Future[*protocol.GetStatsResponse], error)`.

```go
func (engine *MatchingEngine) GetStats(marketID string) (*Future[*protocol.GetStatsResponse], error) {
    respChan := engine.acquireResponseChannel()
    engine.ring.Publish(InputEvent{
        Query: &protocol.GetStatsRequest{MarketID: marketID},
        Resp:  respChan,
    })
    return &Future[*protocol.GetStatsResponse]{engine: engine, respChan: respChan}, nil
}
```

- [ ] **Step 2: Update all call sites in engine_test.go**
Update tests to use `future.Wait(ctx)`.

- [ ] **Step 3: Verify and Commit**
Run: `go test -v ./...`
```bash
git add engine.go engine_test.go
git commit -m "feat: refactor GetStats to return Future"
```

---

### Task 3: Refactor Depth to Future Pattern

**Files:**
- Modify: `engine.go`
- Modify: `engine_test.go`

- [ ] **Step 1: Update Depth signature and implementation**
Change `Depth` to return `(*Future[*protocol.GetDepthResponse], error)`.

- [ ] **Step 2: Update all call sites in engine_test.go**
Update tests to use `future.Wait(ctx)`.

- [ ] **Step 3: Verify and Commit**
Run: `go test -v ./...`
```bash
git add engine.go engine_test.go
git commit -m "feat: refactor Depth to return Future"
```

---

### Task 4: Final Verification and Cleanup

- [ ] **Step 1: Add regression test for UpdateConfig hanging**
Add a test case that sends malformed payload and waits for the future.

- [ ] **Step 2: Run all tests with race detector**
Run: `go test -race -v ./...`

- [ ] **Step 3: Final make check**
Run: `make check`
```bash
git add engine_test.go
git commit -m "test: add regression tests and final cleanup"
```
