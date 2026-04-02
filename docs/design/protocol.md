# Unified Command Protocol Specification

## 1. Background

The matching engine exposes a shared `protocol` package as the boundary between upstream producers and the engine runtime. Producers construct command payloads, serialize them, and submit them into the engine write path. The engine then routes and processes them on a single event loop.

The design goal of this protocol is:

- keep payloads language-neutral and precision-safe
- preserve deterministic replay semantics
- separate enqueue errors from business rejections
- support both trading commands and management commands with one envelope model

## 2. Current Architecture

### 2.1 Command Envelope

The canonical write envelope is `protocol.Command`.

Key properties of the current implementation:

- `MarketID` is part of the envelope and is used for routing
- `CommandID` is part of the envelope and is propagated to emitted logs
- `Payload` is lazily deserialized after routing
- `Metadata` is optional contextual information
- `SeqID` is optional and used when upstream replay / deduplication semantics require it

Illustrative shape:

```go
type Command struct {
    Version   uint8             `json:"version"`
    MarketID  string            `json:"market_id"`
    SeqID     uint64            `json:"seq_id"`
    Type      CommandType       `json:"type"`
    CommandID string            `json:"command_id"`
    Payload   []byte            `json:"payload"`
    Metadata  map[string]string `json:"metadata,omitempty"`
}
```

### 2.2 Public Engine API

The SDK does not expose a public `ExecuteCommand` method. Instead, the `MatchingEngine` provides façade methods such as:

- `PlaceOrder`
- `PlaceOrderBatch`
- `CancelOrder`
- `AmendOrder`
- `CreateMarket`
- `SuspendMarket`
- `ResumeMarket`
- `UpdateConfig`
- `SendUserEvent`

These helpers wrap payloads into `protocol.Command` and enqueue them into the engine ring buffer.

Helper-level contract:

- trading helper payloads must provide a non-empty helper `CommandID` used to populate the envelope
- management helpers require `commandID` as an explicit method argument
- `SendUserEvent` requires an explicit `commandID`

### 2.3 Read Path

The read path uses internal query envelopes rather than `protocol.Command`.

Current query behavior:

- `GetStats()` returns synchronous market statistics
- `Depth()` returns synchronous depth snapshots
- missing markets return `ErrNotFound` immediately
- queries do not participate in replay or command persistence

## 3. Serialization Rules

The engine uses an internal `Serializer` abstraction. The current default is `FastBinarySerializer`, which prefers custom binary encoding for supported payloads and falls back to JSON when needed.

Protocol requirements:

- payload encoding must match the serializer used by the engine
- prices and sizes are represented as strings in the public payload schema
- malformed binary payloads must be rejected rather than partially accepted

## 4. Timestamp Rules

Canonical time semantics are defined in [arch.md](./arch.md).

Protocol-level rule:

- every command that changes engine state must carry an upstream-assigned logical `Timestamp`

This includes:

- trading commands
- management commands
- user events

The engine uses `Timestamp` for deterministic log emission and replay-stable behavior. Non-deterministic local observation time must not be part of the canonical command or log schema.

Current enforcement rule:

- state-changing commands with missing or non-positive `Timestamp` are rejected as `invalid_payload`
- commands with missing `CommandID` are rejected as `invalid_payload`

## 5. Output Event Model

The engine emits `OrderBookLog` records through `PublishLog`.

Current design rules:

- `OrderBookLog` is the deterministic event model
- business failures are emitted as `LogTypeReject`
- successful management commands are emitted as `LogTypeAdmin`
- enqueue / serialization failures are returned as method errors before the command enters the event loop
- downstream systems may attach local observation time in their own `PublishLog` implementation, but that metadata is outside the canonical event schema
- malformed `UserEvent` payloads also emit standardized reject logs rather than being silently dropped
- management-command rejects preserve operator identity via `OrderBookLog.UserID`
- successful management commands preserve operator identity and lifecycle event type via `OrderBookLog.UserID` and `EventType`

Common reject reasons include:

- `invalid_payload`
- `duplicate_order_id`
- `order_not_found`
- `market_not_found`
- `market_already_exists`
- `market_suspended`
- `market_halted`

## 6. Read / Write Responsibility Split

- **Write path**: `protocol.Command` into the shared ring buffer
- **Read path**: internal query envelopes into the same ring buffer

This keeps all state access serialized on the engine event loop while avoiding replay pollution from read-only operations.

## 7. Validation Guidance

When validating protocol compatibility, confirm:

- payload fields match the actual structs in `protocol/command.go`
- all state-changing payloads include `Timestamp`
- all commands include a non-empty `CommandID`
- state-changing payloads with `Timestamp <= 0` are rejected
- commands with empty `CommandID` are rejected
- missing-market write commands emit reject logs rather than silently disappearing
- missing-market read requests return `ErrNotFound`
- binary payload truncation is rejected
- malformed `UserEvent` payloads emit reject logs
- successful management commands emit `LogTypeAdmin`

## 8. Notes

- This document describes the current protocol contract and runtime behavior.
- Historical references to a public `ExecuteCommand` API, `WithSerializer` option, or direct `OrderBook` actor API are obsolete and should not be treated as current integration guidance.
