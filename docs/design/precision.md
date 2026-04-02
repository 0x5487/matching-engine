# Feature Specification: Market Precision Configuration

**Status**: Implemented
**Date**: 2025-12-29
**Reviewer**: Bob, Architect

## 1. Background

Market orders submitted with `QuoteSize` can leave very small residual notional after repeated division by price. Relying only on `matchSize.IsZero()` is insufficient because high-precision decimal arithmetic may continue producing tiny but non-zero quantities.

## 2. Current Solution

The engine uses `LotSize` as the minimum executable trade unit for market-order quote-mode termination.

Current implementation:

- `OrderBook` has a `lotSize` field
- `DefaultLotSize` is the fallback minimum trade unit
- `WithLotSize()` configures a market-specific minimum trade unit
- `handleMarketOrder()` rejects the remaining balance when computed `matchSize` falls below `lotSize`

This prevents pathological micro-remainder loops while preserving deterministic behavior.

## 3. Current API

```go
type OrderBookOption func(*OrderBook)

var DefaultLotSize = udecimal.MustFromInt64(1, 8)

func WithLotSize(size udecimal.Decimal) OrderBookOption {
    return func(book *OrderBook) {
        book.lotSize = size
    }
}
```

`MinLotSize` is also persisted in snapshots and can be configured through management commands.

## 4. Behavior

For market orders using quote mode:

- if available liquidity exists but computed `matchSize` drops below `lotSize`, the engine emits a reject log for the remainder
- the reject log preserves the remaining unfilled notional in `Size`

## 5. Validation Guidance

Validate:

- default fallback behavior when no lot size is configured
- market-specific lot-size overrides
- partial fill followed by reject when residual quote amount is too small
- snapshot / restore preserving lot size

## 6. Notes

- This feature is already implemented.
- Historical references to a still-planned lot-size design or constructors that no longer exist are obsolete.
