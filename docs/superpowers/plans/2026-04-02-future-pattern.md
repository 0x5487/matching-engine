# Async Command Synchronization (Future Pattern) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a generic `Future[T]` and update `CreateMarket` to support synchronous-like waiting for execution results.

**Architecture:** 
- Define `Future[T]` in `models.go`.
- Enhance `MatchingEngine` to route response channels for commands.
- Modify `CreateMarket` to return a `Future` and update its handler to provide feedback.
- Update all existing callers to accommodate the breaking change.

**Tech Stack:** Go (Generics, sync.Pool, Atomic)

---

### Task 1: Define Generic Future

**Files:**
- Modify: `models.go`

- [ ] **Step 1: Add Future structure and Wait method**
Add the following to `models.go`.

```go
// Future represents a placeholder for an asynchronous operation result.
type Future[T any] struct {
	engine   *MatchingEngine
	respChan chan any
	err      error // Error during the submission phase
}

// Wait blocks until the operation completes or the context is cancelled.
func (f *Future[T]) Wait(ctx context.Context) (T, error) {
	if f.err != nil {
		var zero T
		return zero, f.err
	}

	defer func() {
		if f.engine != nil && f.respChan != nil {
			f.engine.releaseResponseChannel(f.respChan)
		}
	}()

	select {
	case res := <-f.respChan:
		if err, ok := res.(error); ok {
			var zero T
			return zero, err
		}
		if val, ok := res.(T); ok {
			return val, nil
		}
		var zero T
		return zero, errors.New("unexpected response type")
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}
```

- [ ] **Step 2: Commit**
```bash
git add models.go
git commit -m "feat: define generic Future[T] structure"
```

---

### Task 2: Enhance MatchingEngine Infrastructure

**Files:**
- Modify: `engine.go`

- [ ] **Step 1: Add internal channel management methods**
Add these helpers to `MatchingEngine` in `engine.go`.

```go
func (engine *MatchingEngine) acquireResponseChannel() chan any {
	val := engine.responsePool.Get()
	return val.(chan any)
}

func (engine *MatchingEngine) releaseResponseChannel(ch chan any) {
	select {
	case <-ch:
	default:
	}
	engine.responsePool.Put(ch)
}
```

- [ ] **Step 2: Add command submission with response support**
Update `MatchingEngine` to handle `Resp` channel for commands.

```go
func (engine *MatchingEngine) enqueueCommandWithResponse(cmd *protocol.Command, resp chan any) error {
	if engine.isShutdown.Load() {
		return ErrShutdown
	}

	seq, ev := engine.ring.Claim()
	if seq == -1 {
		return ErrShutdown
	}

	ev.Cmd = cmd
	ev.Query = nil
	ev.Resp = resp

	engine.ring.Commit(seq)
	return nil
}
```

- [ ] **Step 3: Commit**
```bash
git add engine.go
git commit -m "feat: enhance MatchingEngine to support command responses"
```

---

### Task 3: Implement Future-based CreateMarket

**Files:**
- Modify: `engine.go`

- [ ] **Step 1: Update CreateMarket signature and implementation**
Modify the existing `CreateMarket` method.

```go
func (engine *MatchingEngine) CreateMarket(
	_ context.Context,
	commandID string,
	userID uint64,
	marketID string,
	minLotSize string,
	timestamp int64,
) (*Future[bool], error) {
	if err := requireCommandID(commandID); err != nil {
		return nil, err
	}
	cmd := &protocol.CreateMarketCommand{
		UserID:     userID,
		MarketID:   marketID,
		MinLotSize: minLotSize,
		Timestamp:  timestamp,
	}
	bytes, err := engine.serializer.Marshal(cmd)
	if err != nil {
		return nil, err
	}
	
	respChan := engine.acquireResponseChannel()
	protoCmd := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		MarketID:  marketID,
		CommandID: commandID,
		Payload:   bytes,
	}
	
	if err := engine.enqueueCommandWithResponse(protoCmd, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}

	return &Future[bool]{
		engine:   engine,
		respChan: respChan,
	}, nil
}
```

- [ ] **Step 2: Update processCommand and handleCreateMarket**
Ensure the result is sent back to the `Resp` channel.

```go
// In processCommand
if cmd.Type == protocol.CmdCreateMarket {
    engine.handleCreateMarket(ev) // Pass ev instead of cmd
    return
}

// In handleCreateMarket
func (engine *MatchingEngine) handleCreateMarket(ev *InputEvent) {
    cmd := ev.Cmd
    // ... logic ...
    // Replace rejections with: engine.respondQueryError(ev, err)
    // Add success:
    if ev.Resp != nil {
        select {
        case ev.Resp <- true:
        default:
        }
    }
}
```

- [ ] **Step 3: Commit**
```bash
git add engine.go
git commit -m "feat: migrate CreateMarket to Future-based implementation"
```

---

### Task 4: Fix Breaking Changes and Update Documentation

**Files:**
- Modify: `engine_test.go`, `order_book_bench_test.go`, `engine_user_event_test.go`, `README.md`

- [ ] **Step 1: Update all test callers**
Update `engine.CreateMarket` calls to include `context.Background()` and optionally `.Wait()`.

- [ ] **Step 2: Update README.md**
Replace the polling loop with `future.Wait()`.

- [ ] **Step 3: Verify all tests pass**
Run: `go test ./...`

- [ ] **Step 4: Commit**
```bash
git add .
git commit -m "refactor: update all callers of CreateMarket and documentation"
```
