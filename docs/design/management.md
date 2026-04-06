# Technical Specification: Matching Engine Management Commands

## 1. Background

Management requests are part of the same event-driven write path as trading requests. This preserves replayability, keeps administrative actions observable, and avoids direct state mutation outside the engine event loop.

The current engine already supports management requests for:

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
    CmdPlaceOrder    CommandType = 1
    CmdCancelOrder   CommandType = 2
    CmdAmendOrder    CommandType = 3
    CmdCreateMarket  CommandType = 11
    CmdSuspendMarket CommandType = 12
    CmdResumeMarket  CommandType = 13
    CmdUpdateConfig  CommandType = 14
    CmdUserEvent     CommandType = 21
)
```

### 2.2 Typed Requests

All management requests carry upstream-assigned logical timestamps and use `uint64` operator identity.

```go
type CreateMarketRequest struct {
    BaseCommand
    MinLotSize udecimal.Decimal `json:"min_lot_size"`
}

type SuspendMarketRequest struct {
    BaseCommand
    Reason string `json:"reason"`
}

type ResumeMarketRequest struct {
    BaseCommand
}

type UpdateConfigRequest struct {
    BaseCommand
    MinLotSize udecimal.Decimal `json:"min_lot_size"`
}
```

## 3. Runtime Behavior

### 3.1 Write Path

Management helper methods on `MatchingEngine` accept typed requests directly and enqueue them into the shared engine ring buffer.

Current helper methods:

- `CreateMarket(ctx, *protocol.CreateMarketRequest) (*Future[bool], error)`
- `SuspendMarket(ctx, *protocol.SuspendMarketRequest) (*Future[bool], error)`
- `ResumeMarket(ctx, *protocol.ResumeMarketRequest) (*Future[bool], error)`
- `UpdateConfig(ctx, *protocol.UpdateConfigRequest) (*Future[bool], error)`

These methods are asynchronous with respect to business execution but return a `Future` that can be used to wait for the result. A returned `error` from the helper method itself means enqueue or request-validation failure, not business rejection. Business-level errors (e.g., duplicate market) are returned when calling `future.Wait(ctx)`.
A request with an empty `CommandID` is rejected before enqueue.

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

Management actions are part of deterministic replay semantics because they are represented as typed requests and applied on the event loop.

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
- helper methods require non-empty `CommandID`
- management timestamps must be strictly positive
- create / suspend / resume / config updates are serialized through the engine event loop
- duplicate or malformed management requests emit reject logs
- successful management requests emit `LogTypeAdmin`
- reject logs keep the operator `UserID`
- suspended state blocks place / amend but still allows cancel
- snapshot / restore preserves state and lot-size configuration

## 7. Notes

- Historical references to `AddOrderBook`, `sync.Map`, a public `ExecuteCommand`, or direct imperative market registration are obsolete.
- This document describes the current implemented behavior rather than a future refactor plan.
