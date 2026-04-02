# Feature Specification: Iceberg Orders

**Status**: Implemented
**Date**: 2025-12-29

## 1. Background

Iceberg orders allow a participant to expose only part of the remaining quantity to the public book while keeping the rest hidden inside engine state.

The current implementation targets:

- deterministic replay semantics
- correct visible-depth reporting
- compatibility with snapshot and restore

## 2. Current Payload Contract

Iceberg support is part of `PlaceOrderCommand`.

Current payload rules:

- `Size` is the total order quantity
- `VisibleSize` is the maximum visible resting quantity
- `VisibleSize == 0` means a normal non-iceberg order
- `Timestamp` must be assigned by upstream and is the canonical logical event time

Illustrative shape:

```go
type PlaceOrderCommand struct {
    OrderID     string `json:"order_id"`
    UserID      uint64 `json:"user_id"`
    Side        Side   `json:"side"`
    Type        Type   `json:"type"`
    Price       string `json:"price"`
    Size        string `json:"size"`
    QuoteSize   string `json:"quote_size"`
    VisibleSize string `json:"visible_size"`
    Timestamp   int64  `json:"timestamp"`
}
```

## 3. Order Model

Current order state persists the fields needed for replenishment and restore:

```go
type Order struct {
    // ...
    VisibleLimit udecimal.Decimal `json:"visible_limit,omitzero"`
    HiddenSize   udecimal.Decimal `json:"hidden_size,omitzero"`
    Timestamp    int64            `json:"timestamp"`
}
```

Rules:

- `VisibleLimit` is the configured display cap
- `HiddenSize` is the remaining undisclosed quantity
- snapshot serialization uses `omitzero`, not `omitempty`

## 4. Runtime Behavior

### 4.1 Placement

Current implementation behavior:

- if `VisibleSize` is set, it must be valid relative to total order size
- taker-side matching uses the full remaining quantity
- only the resting remainder is split into visible and hidden portions

This preserves expected execution semantics for aggressive orders while still hiding the resting reserve.

### 4.2 Replenishment

When the visible portion is fully consumed and hidden quantity remains:

- the engine replenishes up to `VisibleLimit`
- the hidden balance is reduced
- the order is re-queued at the tail of the same price level
- the order timestamp is updated from the triggering command timestamp, not from local wall-clock time

This keeps replay deterministic and makes the priority reset explicit.

### 4.3 Market Data

Public depth only reflects currently visible quantity.

Implications:

- hidden size is never included in depth aggregation
- trade logs still report the actual executed quantity
- cancel logs account for both visible and hidden remainder when applicable

## 5. Amend and Cancel Semantics

Iceberg orders follow the same amend framework described in [amend.md](./amend.md).

Current behavior:

- size decrease consumes hidden quantity first when possible
- price change loses priority
- size increase loses priority
- cancel removes both visible and hidden remainder from engine state

## 6. Snapshot and Restore

Iceberg state is part of snapshot persistence.

Current guarantees:

- `VisibleLimit` and `HiddenSize` are serialized in order snapshots
- restore reconstructs iceberg state in memory
- post-restore replenishment behavior remains deterministic

## 7. Timestamp Rules

Canonical time semantics follow [arch.md](./arch.md).

For iceberg orders specifically:

- command `Timestamp` is provided by upstream
- replenishment-related timestamp updates derive from command logical time
- the engine must not synthesize canonical iceberg event time with `time.Now()`

## 8. Validation Guidance

Useful verification scenarios:

- taker iceberg order matches using full available quantity before resting
- depth only shows visible quantity
- full depletion of visible size triggers replenishment
- replenishment moves the order to the back of the queue
- snapshot and restore preserve hidden state

## 9. Notes

- This document describes current implemented behavior.
- Historical review notes, migration TODOs, and obsolete references to `omitempty` or `doc/design/...` paths are intentionally removed from this version.
