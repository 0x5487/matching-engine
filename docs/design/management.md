# Technical Specification: Matching Engine Management Commands

## 1. Background

Management requests used to be part of the same event-driven write path as
trading requests. That is no longer the case.

`CreateMarket`, `SuspendMarket`, `ResumeMarket` and `UpdateConfig` were
removed, along with `OrderBookState`, `RejectReasonMarketSuspended`,
`RejectReasonMarketHalted`, and the whole `lotSize` mechanism. Rationale and
migration notes live in the CHANGELOG (`[unschedule]` section). In short: an
order book matches orders routed to it, and the set of tradeable markets is
decided upstream — so `MatchingEngine` now obtains books only by restoring
them from a snapshot, and callers that manage books themselves use the
synchronous `NewOrderBook` API.

If a maintenance-window freeze is ever needed, a suspend command should be
deliberately added back rather than accidentally retained. Its semantics are
a full freeze: while suspended, place, amend **and** cancel are all refused.

## 2. Current Contract

### 2.1 Command Types

The only command types in `protocol/command.go` are (excluding the
`CmdUnknown` zero-value sentinel):

```go
const (
    CmdPlaceOrder  CommandType = 1
    CmdCancelOrder CommandType = 2
    CmdAmendOrder  CommandType = 3
    CmdUserEvent   CommandType = 21
)
```

`AmendOrderRequest` (price/size amendment) and `UserEventRequest` (generic
side channel) were kept: neither belongs to market lifecycle.

All requests carry upstream-assigned logical timestamps and use `uint64`
operator identity. `CommandID` is validated before enqueue (a helper returns
an error); `Timestamp` is validated on the event loop by each book command,
which emits an `invalid_payload` reject log for a missing or non-positive
value. Callers must therefore handle two failure shapes: a Go error from the
helper itself (enqueue or request-validation failure) and a reject log for
business-level refusal.

### 2.2 Write Path

`MatchingEngine` write helpers accept typed requests directly and enqueue
them into the shared engine ring buffer:

- `PlaceOrderAsync(ctx, *protocol.PlaceOrderRequest) error`
- `PlaceOrderBatchAsync(ctx, []*protocol.PlaceOrderRequest) error`
- `CancelOrderAsync(ctx, *protocol.CancelOrderRequest) error`
- `AmendOrderAsync(ctx, *protocol.AmendOrderRequest) error`
- `SendUserEvent(ctx, *protocol.UserEventRequest) error`

These methods are asynchronous with respect to business execution.
A returned `error` from the helper method itself means enqueue or
request-validation failure, not business rejection.

### 2.3 Business Failures

Business-level failures are emitted as `OrderBookLog` rejects.
Trading commands sent to a missing market emit
`RejectReasonMarketNotFound`. Reject logs preserve the actor `UserID` for
audit correlation.

## 3. Recovery and Replay

Books are recovered from snapshot plus replay of the surviving command
types. Market lifecycle is not part of replayed state: there is no market
registry in the library to rebuild, and none is needed.

## 4. Query Semantics

Read-only methods are not commands and do not participate in replay:

- `GetStats`
- `Depth`
- `TakeSnapshot`
- `RestoreFromSnapshot`

For missing markets:

- read methods return `ErrNotFound`
- write methods emit reject logs when appropriate

## 5. Notes

- Historical references to `CreateMarket`, `SuspendMarket`, `ResumeMarket`,
  `UpdateConfig`, `OrderBookState`, `MinLotSize`, `WithLotSize`, market
  suspension, or lot-size configuration are obsolete.
- This document describes the current implemented behavior rather than a
  future refactor plan.
