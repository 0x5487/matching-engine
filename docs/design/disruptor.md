# Design Decision: Disruptor Pattern

**Status**: Approved
**Date**: 2025-12-28
**Version**: v1.0

## 1. Context

The matching engine uses a shared MPSC ring buffer to serialize all writes and internal queries onto a single consumer goroutine. This supports:

- strict ordering
- low coordination overhead
- contention reduction across producers
- deterministic single-thread state transitions

## 2. Current Architecture

The current runtime uses a **single shared ring buffer owned by `MatchingEngine`**, not one ring buffer per order book.

Model:

- multiple producers call engine helper methods concurrently
- each producer claims slots in the shared ring buffer
- the engine event loop consumes events sequentially
- the engine routes commands to the target order book on the consumer goroutine

This means the consumer is logically the engine event loop, not an independently running `OrderBook` goroutine.

## 3. API Surface

Current ring buffer API:

- `Claim()`
- `ClaimN(n)`
- `Commit(seq)`
- `CommitN(startSeq, endSeq)`
- `Publish(event)`
- `Run()`
- `Shutdown(ctx)`
- `ConsumerSequence()`
- `ProducerSequence()`
- `GetPendingEvents()`

Historical names such as `Next()`, `PublishNext()`, and `Start()` are obsolete.

## 4. Claim / Commit Pattern

The engine uses direct slot claims for low-overhead writes.

Conceptually:

```go
seq, slot := rb.Claim()
if seq == -1 {
    return ErrShutdown
}

slot.Cmd = cmd
rb.Commit(seq)
```

Batch submission uses `ClaimN` / `CommitN` to amortize synchronization overhead.

## 5. Operational Characteristics

Pros:

- low-latency MPSC write path
- deterministic FIFO processing on the consumer side
- explicit backpressure when the buffer is full
- simple engine-level state ownership

Trade-offs:

- fixed power-of-two capacity
- busy-wait behavior using `runtime.Gosched()`
- single consumer limits parallel state mutation
- not literally zero allocation for every end-to-end path, though hot-path allocations are heavily reduced

## 6. Shutdown Semantics

`Shutdown(ctx)` sets the shutdown flag, blocks new claims, and waits until the consumer catches up with all claimed events or the context expires.

This ensures already-accepted events are drained before shutdown completes.

## 7. Validation Guidance

When validating ring-buffer behavior, confirm:

- multiple goroutines can publish concurrently
- consumer ordering remains FIFO by sequence
- shutdown drains already-claimed events
- batch enqueues preserve contiguous slot ownership
- query events and command events share the same serialization point

## 8. Notes

- This document describes the current engine-wide shared-ring-buffer design.
- References to per-order-book consumers or deprecated API names should not be used as implementation guidance.
