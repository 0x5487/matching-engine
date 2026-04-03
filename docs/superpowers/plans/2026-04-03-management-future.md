# Management Interface Future Pattern Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor management interfaces (`SuspendMarket`, `ResumeMarket`, `UpdateConfig`) to return `*Future[bool]`, enabling synchronous-like waiting for asynchronous operations.

**Architecture:** Utilize the existing `Future[T]` and `responsePool` infrastructure. Modify the command delivery path to pass `InputEvent` (containing the response channel) all the way to the `OrderBook` handlers.

**Tech Stack:** Go (Generics, sync.Pool, Atomic, RingBuffer)

---

### Task 1: Update OrderBook Signature and Add Helpers

**Files:**
- Modify: `order_book.go`
- Modify: `engine.go`

- [ ] **Step 1: Update OrderBook.processCommand signature**
Change the signature to accept `*InputEvent` instead of `*protocol.Command`. Update all internal calls to handlers.

```go
// In order_book.go
func (book *OrderBook) processCommand(ev *InputEvent) {
    cmd := ev.Cmd
    // ... switch ...
    case protocol.CmdSuspendMarket:
        book.handleSuspendMarket(ev)
    // ... repeat for ResumeMarket and UpdateConfig ...
}
```

- [ ] **Step 2: Add response helper methods to OrderBook**
Add `respondSuccess` and `respondError` to `OrderBook` to safely send results back through `ev.Resp`.

```go
func (book *OrderBook) respondSuccess(ev *InputEvent, val any) {
    if ev.Resp != nil {
        select {
        case ev.Resp <- val:
        default:
        }
    }
}

func (book *OrderBook) respondError(ev *InputEvent, err error) {
    if ev.Resp != nil {
        select {
        case ev.Resp <- err:
        default:
        }
    }
}
```

- [ ] **Step 3: Update MatchingEngine call site**
Modify `MatchingEngine.processCommand` to pass the entire `ev` to `book.processCommand`.

```go
// In engine.go
func (engine *MatchingEngine) processCommand(ev *InputEvent) {
    // ...
    book.processCommand(ev) // Pass ev instead of cmd
    // ...
}
```

- [ ] **Step 4: Verify Compilation**
Run: `go build ./...`
Expected: PASS (with some "too many arguments" errors in order_book_test.go if it calls processCommand directly, which we will fix in Step 5)

- [ ] **Step 5: Fix internal OrderBook tests**
Update `order_book_test.go` and other tests that call `processCommand` directly to pass a dummy `InputEvent`.

- [ ] **Step 6: Commit**
```bash
git add order_book.go engine.go order_book_test.go
git commit -m "refactor: update OrderBook.processCommand to accept InputEvent"
```

---

### Task 2: Implement Future for SuspendMarket

**Files:**
- Modify: `engine.go`
- Modify: `order_book.go`
- Test: `engine_test.go`

- [ ] **Step 1: Update SuspendMarket in engine.go**
Refactor to use `enqueueCommandWithResponse` and return `*Future[bool]`.

```go
func (engine *MatchingEngine) SuspendMarket(
    ctx context.Context,
    commandID string, 
    userID uint64, 
    marketID string, 
    timestamp int64,
) (*Future[bool], error) {
    // ... marshal cmd ...
    respChan := engine.acquireResponseChannel()
    protoCmd := &protocol.Command{...}
    if err := engine.enqueueCommandWithResponse(protoCmd, respChan); err != nil {
        engine.releaseResponseChannel(respChan)
        return nil, err
    }
    return &Future[bool]{engine: engine, respChan: respChan}, nil
}
```

- [ ] **Step 2: Update handleSuspendMarket in order_book.go**
Update the handler to report success or error via `ev.Resp`.

```go
func (book *OrderBook) handleSuspendMarket(ev *InputEvent) {
    payload := &protocol.SuspendMarketCommand{}
    // ... unmarshal ...
    if book.state == protocol.OrderBookStateHalted {
        book.respondError(ev, errors.New(string(protocol.RejectReasonMarketHalted)))
        return
    }
    // ... execution logic ...
    book.respondSuccess(ev, true)
}
```

- [ ] **Step 3: Run existing tests to identify breakages**
Run: `go test ./engine_test.go`
Expected: FAIL (signature mismatch or build error)

- [ ] **Step 4: Fix and add test case**
Update existing tests to use `future.Wait()`.

- [ ] **Step 5: Commit**
```bash
git add engine.go order_book.go engine_test.go
git commit -m "feat: implement Future for SuspendMarket"
```

---

### Task 3: Implement Future for ResumeMarket

**Files:**
- Modify: `engine.go`
- Modify: `order_book.go`

- [ ] **Step 1: Update ResumeMarket in engine.go**
Refactor to return `*Future[bool]`.

- [ ] **Step 2: Update handleResumeMarket in order_book.go**
Update handler to call `book.respondSuccess(ev, true)`.

- [ ] **Step 3: Verify and Commit**
Run: `go test ./engine_test.go`
```bash
git add engine.go order_book.go
git commit -m "feat: implement Future for ResumeMarket"
```

---

### Task 4: Implement Future for UpdateConfig

**Files:**
- Modify: `engine.go`
- Modify: `order_book.go`

- [ ] **Step 1: Update UpdateConfig in engine.go**
Refactor to return `*Future[bool]`.

- [ ] **Step 2: Update handleUpdateConfig in order_book.go**
Update handler to report error on invalid `MinLotSize` and success on completion.

- [ ] **Step 3: Verify and Commit**
Run: `go test ./engine_test.go`
```bash
git add engine.go order_book.go
git commit -m "feat: implement Future for UpdateConfig"
```

---

### Task 5: Final Verification and Cleanup

- [ ] **Step 1: Run all tests**
Run: `go test ./...`
Expected: PASS

- [ ] **Step 2: Verify channel pooling**
Ensure no leaking channels by running tests with `-count=100` if needed.

- [ ] **Step 3: Update documentation/README if necessary**
(Already covered by the user's initial request example)
