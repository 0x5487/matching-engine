# Feature Specification: Amend Order

**Status**: Implemented
**Date**: 2025-12-29
**Reviewer**: Bob, Architect

## 1. Background

`AmendOrder` allows a trader to modify a resting order without first issuing a separate cancel. The current implementation supports changing:

- price
- total remaining size

## 2. Current Payload Contract

Current protocol payload:

```go
type AmendOrderCommand struct {
    OrderID   string `json:"order_id"`
    UserID    uint64 `json:"user_id"`
    NewPrice  string `json:"new_price"`
    NewSize   string `json:"new_size"`
    Timestamp int64  `json:"timestamp"`
}
```

`NewPrice` and `NewSize` are string-encoded and parsed inside the engine. Malformed values are rejected as `invalid_payload`.

## 3. Current Behavior

### 3.1 Validation

- order must exist
- `UserID` must match the owner
- `NewPrice` and `NewSize` must parse successfully

Failures emit `RejectLog` with the appropriate reject reason.

### 3.2 Priority Rules

Current implementation behavior:

- **price change**: loses priority and is re-matched as a fresh limit order
- **size increase**: loses priority and is re-matched as a fresh limit order
- **same price + size decrease**: retains priority via in-place queue update

### 3.4 Amend Log Fields

The amend log carries two kinds of size, and they must not be mixed:

- `Size` and `OldSize` are the order total (visible plus hidden). They are the order's facts, for example for releasing funds.
- `VisibleRemoved` is the visible (depth) size the amend takes off the book. Losing priority removes the whole visible part at `OldPrice`. An in-place decrease removes only the part that reaches past the hidden size, so a decrease that stays inside the hidden size has `VisibleRemoved` 0.

The depth version advances only when the amend changes visible depth (a price change, a size increase, or a visible decrease). A no-op amend, and a decrease that only touches hidden size, repeat the current version.

### 3.3 Concurrency Model

The engine processes amend commands on the single event loop, so the order state at amend time is deterministic with respect to prior fills and cancels.

## 4. Iceberg Interaction

For iceberg orders:

- decreases consume hidden size first when possible
- increases or price changes trigger the priority-loss path
- the resulting order may be re-matched or re-queued depending on market conditions

## 5. Validation Guidance

Useful verification scenarios:

- decrease size keeps queue position
- increase size loses queue position
- price change immediately crosses and matches when appropriate
- non-existent or wrong-owner amend rejects
- malformed `NewPrice` / `NewSize` rejects without silently coercing values

## 6. Notes

- Historical examples that model amend payload fields as decimals instead of strings are obsolete.
- This document reflects the current implemented amend contract and behavior.
