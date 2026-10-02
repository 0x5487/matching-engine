# Typed Request Protocol Specification

## 1. Background

The matching engine exposes a shared `protocol` package as the boundary between upstream producers and the engine runtime. Producers construct typed requests, serialize them, and submit them into the engine write path. The engine then routes and processes them on a single event loop.

The design goal of this protocol is:

- keep typed requests language-neutral and precision-safe
- preserve deterministic replay semantics
- separate enqueue errors from business rejections
- support trading requests and user events with one shared request schema
  (market lifecycle commands were removed; see `docs/design/management.md`)

## 2. Current Architecture

### 2.1 Typed Request Model

The canonical write contract is `protocol.BaseCommand` plus typed request structs such as `protocol.PlaceOrderRequest` and `protocol.AmendOrderRequest`.

Key properties of the current implementation:

- `MarketID` is part of the shared request header and is used for routing
- `CommandID` is part of the shared request header and is propagated to emitted logs
- `Type` remains part of the shared request header for binary decoding
- request bodies are strongly typed rather than caller-defined payload blobs
- `SeqID` is optional and used when upstream replay / deduplication semantics require it

Illustrative shape:

```go
type BaseCommand struct {
    SeqID     uint64 `json:"seq_id"`
    CommandID string `json:"command_id"`
    UserID    uint64 `json:"user_id"`
    MarketID  string `json:"market_id"`
    Timestamp int64  `json:"timestamp"`
}

type PlaceOrderRequest struct {
    BaseCommand
    OrderID     string           `json:"order_id"`
    Side        Side             `json:"side"`
    OrderType   OrderType        `json:"order_type"`
    Price       udecimal.Decimal `json:"price"`
    Size        udecimal.Decimal `json:"size"`
    VisibleSize udecimal.Decimal `json:"visible_size"`
    QuoteSize   udecimal.Decimal `json:"quote_size"`
    StepSize    udecimal.Decimal `json:"step_size"`
}
```

`StepSize` travels on every order so that a replay cannot disagree with
itself about which unit was in force. A quantity-sized order carrying a
non-zero step must be a whole number of it (`Size % StepSize == 0`,
likewise for a non-zero iceberg `VisibleSize`), otherwise the book refuses
it as `invalid_payload`; a zero step skips the check. The quote path floors
each level's conversion to the step before taking the minimum with the
resting size.

### 2.2 Public Engine API

The SDK exposes typed facade methods instead of a generic command submission API. `MatchingEngine` provides the following core methods:

- `PlaceOrderAsync(ctx, *protocol.PlaceOrderRequest) error`
- `PlaceOrderBatchAsync(ctx, []*protocol.PlaceOrderRequest) error`
- `CancelOrderAsync(ctx, *protocol.CancelOrderRequest) error`
- `AmendOrderAsync(ctx, *protocol.AmendOrderRequest) error`
- `SendUserEvent(ctx, *protocol.UserEventRequest) error`

Market lifecycle commands (`CreateMarket`, `SuspendMarket`, `ResumeMarket`,
`UpdateConfig`) were removed: the set of tradeable markets is decided
upstream, and `MatchingEngine` obtains books only by restoring them from a
snapshot (see `docs/design/management.md`).

These methods validate typed requests and enqueue them into the engine ring buffer.

Contract:

- Binary serialization across process boundaries is performed through `MarshalRequest` and `UnmarshalRequest`.
- The `CommandID` must be explicitly populated on the typed request.
- `Timestamp` must be explicitly populated on the typed request for logical time ordering.

### 2.3 Read Path

The read path uses `protocol.Query` rather than the typed write-request API.

Current query behavior:

- `GetStats()` returns synchronous market statistics
- `Depth()` returns synchronous depth snapshots
- missing markets return `ErrNotFound` immediately
- queries do not participate in replay or command persistence

## 3. Serialization Rules

The engine uses direct binary marshaling in `protocol.MarshalRequest` and `protocol.UnmarshalRequest`.

Protocol requirements:

- payload encoding must match the binary request format used by `protocol`
- prices and sizes are represented as `udecimal.Decimal` values in the public Go schema
- malformed binary payloads must be rejected rather than partially accepted

## 4. Timestamp Rules

Canonical time semantics are defined in [arch.md](./arch.md).

Protocol-level rule:

- every request that changes engine state must carry an upstream-assigned logical `Timestamp`

This includes:

- trading commands
- user events

The engine uses `Timestamp` for deterministic log emission and replay-stable behavior. Non-deterministic local observation time must not be part of the canonical command or log schema.

Current enforcement rule:

- state-changing requests with missing or non-positive `Timestamp` are rejected as `invalid_payload`
- requests with missing `CommandID` are rejected as `invalid_payload`

## 5. Output Event Model

The engine emits `OrderBookLog` records through `PublishLog`.

Current design rules:

- `OrderBookLog` is the deterministic event model
- business failures are emitted as `LogTypeReject`
- enqueue / serialization failures are returned as method errors before the request enters the event loop
- downstream systems may attach local observation time in their own `PublishLog` implementation, but that metadata is outside the canonical event schema
- malformed `UserEvent` payloads also emit standardized reject logs rather than being silently dropped
- rejects preserve the caller-supplied actor identity via `OrderBookLog.UserID`

Common reject reasons include:

- `invalid_payload`
- `duplicate_order_id`
- `order_not_found`
- `market_not_found`
- `no_liquidity`
- `price_mismatch`
- `insufficient_size`
- `post_only_match`

## 6. Read / Write Responsibility Split

- **Write path**: typed `protocol.*Request` values into the shared ring buffer
- **Read path**: `protocol.Query` values into the same ring buffer

This keeps all state access serialized on the engine event loop while avoiding replay pollution from read-only operations.

## 7. Validation Guidance

When validating protocol compatibility, confirm:

- request fields match the actual structs in `protocol/command.go`
- all state-changing requests include `Timestamp`
- all requests include a non-empty `CommandID`
- state-changing requests with `Timestamp <= 0` are rejected
- requests with empty `CommandID` are rejected
- missing-market write requests emit reject logs rather than silently disappearing
- missing-market read requests return `ErrNotFound`
- binary payload truncation is rejected
- malformed `UserEvent` payloads emit reject logs

## 8. Notes

- This document describes the current protocol contract and runtime behavior.
- Historical references to `protocol.Command`, `Submit*`, a public `ExecuteCommand` API, `WithSerializer`, or direct `OrderBook` actor APIs are obsolete and should not be treated as current integration guidance.
