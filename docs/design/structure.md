# Design Decision: Price Level Data Structure

**Status**: Implemented
**Date**: 2025-12-28
**Reviewer**: Architect

## 1. Decision

The engine currently uses `structure.PooledSkiplist` for ordered price-level indexing inside `queue.go`.

This decision was made to balance:

- good delete performance under high cancel rates
- low allocation behavior on hot paths
- predictable ordered traversal for best-price lookup and depth iteration

## 2. Current Queue Integration

`queue` currently maintains:

- `orderedPrices *structure.PooledSkiplist`
- `priceList map[udecimal.Decimal]*priceUnit`
- per-price FIFO via intrusive linked-list pointers on `Order`

Responsibilities are split as follows:

- `orderedPrices` maintains sorted price keys
- `priceList` stores aggregated price-level state
- linked-list pointers preserve FIFO order among orders at the same price

## 3. Why This Structure

The current design favors:

- fast best-price lookup through `Min()`
- ordered iteration for depth and snapshot generation
- efficient removal when a price level becomes empty
- low allocation compared with generic skiplist implementations

## 4. Runtime Notes

- buyer queues use a descending skiplist configuration so the best bid is returned first
- seller queues use ascending ordering so the best ask is returned first
- FIFO at a price level is independent of skiplist ordering and is maintained by `head` / `tail` links in `priceUnit`

## 5. Validation Guidance

Validate the current structure with:

- insert / delete / contains behavior in `structure` tests
- queue best-price lookup correctness
- snapshot iteration order
- mixed insertion and cancellation workloads

## 6. Notes

- The pooled skiplist is already integrated; references to future replacement of `huando/skiplist` are obsolete.
- Future work, if any, should focus on performance tuning and operational limits rather than initial integration.
