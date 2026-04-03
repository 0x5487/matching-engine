# Technical Specification: Matching Engine Management Commands

## 1. Background

Management commands are part of the same event-driven write path as trading commands. This preserves replayability, keeps administrative actions observable, and avoids direct state mutation outside the engine event loop.

The current engine already supports management commands for:

- market creation
- market suspension
- market resume
- market configuration update

## 2. Current Contract

### 2.1 Command Types

Management command types currently defined in `protocol/command.go`:

```go
const (
    CmdUnknown       CommandType = 0
    CmdCreateMarket  CommandType = 1
    CmdSuspendMarket CommandType = 2
    CmdResumeMarket  CommandType = 3
    CmdUpdateConfig  CommandType = 4

    CmdPlaceOrder    CommandType = 51
    CmdCancelOrder   CommandType = 52
    CmdAmendOrder    CommandType = 53
)
```

### 2.2 Payloads

All management payloads carry upstream-assigned logical timestamps and use `uint64` operator identity.

```go
type CreateMarketCommand struct {
    UserID     uint64 `json:"user_id"`
    MarketID   string `json:"market_id"`
    MinLotSize string `json:"min_lot_size"`
    Timestamp  int64  `json:"timestamp"`
}

type SuspendMarketCommand struct {
    UserID    uint64 `json:"user_id"`
    MarketID  string `json:"market_id"`
    Reason    string `json:"reason"`
    Timestamp int64  `json:"timestamp"`
}

type ResumeMarketCommand struct {
    UserID    uint64 `json:"user_id"`
    MarketID  string `json:"market_id"`
    Timestamp int64  `json:"timestamp"`
}

type UpdateConfigCommand struct {
    UserID     uint64 `json:"user_id"`
    MarketID   string `json:"market_id"`
    MinLotSize string `json:"min_lot_size,omitempty"`
    Timestamp  int64  `json:"timestamp"`
}
```

## 3. Runtime Behavior

### 3.1 Write Path

Management helper methods on `MatchingEngine` construct these payloads, wrap them in `protocol.Command`, and enqueue them into the shared engine ring buffer.

Current helper methods:

- `CreateMarket(ctx, commandID, userID, marketID, minLotSize, timestamp) (*Future[bool], error)`
- `SuspendMarket(ctx, commandID, userID, marketID, timestamp) (*Future[bool], error)`
- `ResumeMarket(ctx, commandID, userID, marketID, timestamp) (*Future[bool], error)`
- `UpdateConfig(ctx, commandID, userID, marketID, minLotSize, timestamp) (*Future[bool], error)`

These methods are asynchronous with respect to business execution but return a `Future` that can be used to wait for the result. A returned `error` from the helper method itself means enqueue or serialization failure, not business rejection. Business-level errors (e.g., duplicate market) are returned when calling `future.Wait(ctx)`.
An empty `commandID` is rejected before enqueue.

### 3.2 Business Failures

Business-level failures are emitted as `OrderBookLog` rejects.

Current behaviors:

- duplicate market creation emits `RejectReasonMarketAlreadyExists`
- invalid `MinLotSize` emits `RejectReasonInvalidPayload`
- missing `CommandID` is rejected as `invalid_payload`
- missing or non-positive `Timestamp` emits `RejectReasonInvalidPayload`
- trading commands sent to a missing market emit `RejectReasonMarketNotFound`
- reject logs preserve the management actor `UserID` for audit correlation

### 3.3 Successful Management Events

Successful management commands are emitted through `PublishLog` as `LogTypeAdmin`.

Current event types:

- `market_created`
- `market_suspended`
- `market_resumed`
- `market_config_updated`

### 3.4 State Enforcement

Current order book state rules:

| Market State | PlaceOrder | CancelOrder | AmendOrder |
|--------------|------------|-------------|------------|
| `Running`    | Allowed    | Allowed     | Allowed    |
| `Suspended`  | Reject     | Allowed     | Reject     |
| `Halted`     | Reject     | Reject      | Reject     |

Rejected operations produce `OrderBookLog` entries with `market_suspended` or `market_halted`.

## 4. Recovery and Replay

Management actions are part of deterministic replay semantics because they are represented as commands and applied on the event loop.

Implications:

- market creation state is recoverable from snapshot + replay
- market suspension / resume state is restorable from snapshot
- configuration changes such as `MinLotSize` are part of replayed state transitions

## 5. Query Semantics

Read-only methods are not management commands and do not participate in replay:

- `GetStats`
- `Depth`
- `TakeSnapshot`
- `RestoreFromSnapshot`

For missing markets:

- read methods return `ErrNotFound`
- write methods emit reject logs when appropriate

## 6. Validation Guidance

When verifying management command behavior, confirm:

- helper methods require upstream timestamps
- helper methods require non-empty `commandID`
- management timestamps must be strictly positive
- create / suspend / resume / config updates are serialized through the engine event loop
- duplicate or malformed management commands emit reject logs
- successful management commands emit `LogTypeAdmin`
- reject logs keep the operator `UserID`
- suspended state blocks place / amend but still allows cancel
- snapshot / restore preserves state and lot-size configuration

## 7. Notes

- Historical references to `AddOrderBook`, `sync.Map`, a public `ExecuteCommand`, or direct imperative market registration are obsolete.
- This document describes the current implemented behavior rather than a future refactor plan.
