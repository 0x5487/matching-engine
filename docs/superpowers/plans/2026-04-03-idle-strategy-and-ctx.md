# Idle Strategy & Context-Aware Submission Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor `RingBuffer` to support non-blocking claims, introduce `IdleStrategy` for flexible waiting, and update `MatchingEngine` to fully respect `context.Context` during command submission. Also, ensure metadata preservation and error consistency.

**Architecture:** 
1. **RingBuffer**: Move busy-wait logic out of `ClaimN` into a new `TryClaimN` method.
2. **IdleStrategy**: Define an interface for waiting strategies (BusySpin, Yield, Sleep).
3. **Engine**: Implement a retry loop in the submission path that checks `ctx.Done()` and uses `IdleStrategy`.
4. **API**: Standardize all query/management methods to accept `context.Context`.

**Tech Stack:** Go (Generics, Atomic, Context, Strategy Pattern)

---

### Task 1: Enhance RingBuffer with TryClaim and IdleStrategy

**Files:**
- Modify: `disruptor.go`

- [ ] **Step 1: Add TryClaimN to RingBuffer**
Add a non-blocking version of sequence claiming.

```go
func (rb *RingBuffer[T]) TryClaimN(n int64) (start int64, end int64) {
	if rb.isShutdown.Load() || n <= 0 {
		return -1, -1
	}
	if n > rb.capacity {
		n = rb.capacity
	}
	currentProducerSeq := rb.producerSequence.Load()
	nextSeq := currentProducerSeq + n
	wrapPoint := nextSeq - rb.capacity
	if wrapPoint > rb.consumerSequence.Load() {
		return -1, -1 // Buffer full
	}
	if rb.producerSequence.CompareAndSwap(currentProducerSeq, nextSeq) {
		return currentProducerSeq + 1, nextSeq
	}
	return -1, -1 // CAS failed
}
```

- [ ] **Step 2: Define IdleStrategy interface and implementations**
Add `IdleStrategy` at the bottom of `disruptor.go`.

```go
type IdleStrategy interface {
	Idle(retries int)
}

type YieldingIdleStrategy struct{}
func (s YieldingIdleStrategy) Idle(retries int) { runtime.Gosched() }

type BusySpinIdleStrategy struct{}
func (s BusySpinIdleStrategy) Idle(retries int) {}
```

- [ ] **Step 3: Update existing ClaimN to use the new pattern (Internal)**
Optional: Refactor `ClaimN` to use a default `YieldingIdleStrategy` to maintain internal consistency.

- [ ] **Step 4: Commit**
```bash
git add disruptor.go
git commit -m "feat: add TryClaimN and IdleStrategy to RingBuffer"
```

---

### Task 2: Implement Context-Aware Submission in Engine

**Files:**
- Modify: `engine.go`

- [ ] **Step 1: Update enqueueCommandWithResponse to accept Context**
Implement the retry loop with `ctx` check and `IdleStrategy`.

```go
func (engine *MatchingEngine) enqueueCommandWithResponse(ctx context.Context, cmd *protocol.Command, resp chan any) error {
	if engine.isShutdown.Load() {
		return ErrShutdown
	}
	strategy := YieldingIdleStrategy{} // Default
	for i := 0; ; i++ {
		seq, slot := engine.ring.TryClaim() // Helper for TryClaimN(1)
		if seq != -1 {
			slot.Cmd = cmd
			slot.Resp = resp
			engine.ring.Commit(seq)
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		strategy.Idle(i)
	}
}
```

- [ ] **Step 2: Update EnqueueCommand and EnqueueCommandBatch to accept Context**
Ensure all entry points are context-aware.

- [ ] **Step 3: Update all public management methods to pass ctx**
Ensure `CreateMarket`, `SuspendMarket`, etc., pass their `ctx` to the submission logic.

- [ ] **Step 4: Commit**
```bash
git add engine.go
git commit -m "feat: implement context-aware command submission in engine"
```

---

### Task 3: Standardize Query API Signatures

**Files:**
- Modify: `engine.go`
- Modify: `engine_test.go`

- [ ] **Step 1: Add context to GetStats and Depth**
```go
func (engine *MatchingEngine) GetStats(ctx context.Context, marketID string) (*Future[*protocol.GetStatsResponse], error)
func (engine *MatchingEngine) Depth(ctx context.Context, marketID string, limit uint32) (*Future[*protocol.GetDepthResponse], error)
```

- [ ] **Step 2: Update internal query submission to respect Context**
Use the same `TryClaim` pattern for queries.

- [ ] **Step 3: Update all call sites in tests**
Update `engine_test.go` to pass `ctx` to `GetStats` and `Depth`.

- [ ] **Step 4: Commit**
```bash
git add engine.go engine_test.go
git commit -m "refactor: align Query API signatures with context"
```

---

### Task 4: Fix Metadata and Error Consistency

**Files:**
- Modify: `order_book.go`
- Modify: `engine.go`

- [ ] **Step 1: Restore Metadata in OrderBook handlers**
In `processCommand`, when calling `rejectInvalidPayload`, pass `cmd.Metadata` instead of `nil`.

- [ ] **Step 2: Unify missing market error to ErrNotFound**
Change `errors.New("market_not_found")` to `ErrNotFound` in `processCommand`.

- [ ] **Step 3: Verify and Commit**
Run all tests.
```bash
git add order_book.go engine.go
git commit -m "fix: restore metadata preservation and unify ErrNotFound"
```

---

### Task 5: Final Verification

- [ ] **Step 1: Add test for submission timeout**
Create a test that fills the RingBuffer and verifies that `CreateMarket` returns `context.DeadlineExceeded`.

- [ ] **Step 2: Final make check**
Run: `make check`
```bash
git commit -m "test: add submission timeout verification"
```
