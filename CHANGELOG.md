# CHANGELOG

## [unreleased]

- perf: implement thread-safe `GetStats()` using unified command channel to eliminate race conditions in tests.
- refactor: replace `time.Sleep` with `assert.Eventually` in unit tests for faster and more deterministic execution.
- feature: implement non-blocking snapshot/restore and refactor order structure/API for better performance and maintainability.
- perf: replace `shopspring/decimal` with `udecimal` (uint64-based) for **zero-allocation arithmetic**, reducing hot-path allocations from 27/op to 4/op.
- perf: optimize core engine performance to achieve **~2M orders/sec throughput** and **<5 allocs/op** via intrusive lists, map key optimization, and strict object pooling.

## v0.7.0 (2025-12-14)

- **breaking**: replace `OrderBookID` with `SequenceID` in `BookLog`. All events now have a globally increasing `SequenceID` for ordering, deduplication, and rebuild synchronization. Use `LogType` to determine if the event affects order book state.
- feature: add `QuoteSize` field to `Order` for Market orders to specify amount in quote currency (e.g., USDT). Mutually exclusive with `Size`.
- feature: add `RejectReason` field to `BookLog` with constants for reject scenarios (`no_liquidity`, `price_mismatch`, `insufficient_size`, `would_cross_spread`)
- feature: add `TradeID` field to `BookLog` for sequential trade identification (only set on Match events)
- feature: add `Amount` field to `BookLog` with pre-calculated `Price × Size` (only set on Match events)
- perf: optimize atomic operations by using `Add()` return value instead of separate `Load()` call
- refactor: split `handleOrder` into separate handlers for each order type
- refactor: replace `Trade` with `BookLog` to capture Open, Match, and Cancel events for full order book reconstruction
- feature: add `AmendOrder` to support modifying order price and size
- perf: implement `sync.Pool` for `BookLog` to reduce GC pressure and achieve zero-allocation for log events
- feature: add `LogTypeReject` to distinguish orders that never entered the book (e.g. failed PostOnly/IOC/FOK) from cancellations
- feature: add `CalculateDepthChange` helper to simplify downstream depth updates
- docs: improve readme document and provide more detail info
- fix: FOK order validation incorrectly used single order size instead of price level total size
- fix: FOK order validation did not properly reject when price doesn't match
- fix: `depth()` function off-by-one error returning `limit-1` items instead of `limit`
- feature: add `OrderBook.Shutdown(ctx)` for graceful shutdown with pending order drain
- feature: add `MatchingEngine.Shutdown(ctx)` for graceful shutdown of all markets in parallel
- perf: reduce channel buffer sizes from 1,000,000 to 10,000 for orders and 100 for depth queries to reduce memory usage
- perf: pre-allocate slice capacity in order handlers to reduce dynamic allocations
- perf: cache `time.Now().UTC()` per order to reduce syscall overhead in high-frequency scenarios
- perf: replace atomic operations with direct variable access in queue (single-goroutine access pattern)

## v0.6.0 (2023-10-20)

- feature: PublishTrader
- refactor: test case can run parallel

## v0.5.1 (2023-09-02)

- refactor: remove all mutex loc

## v0.5.0 (2023-07-02)

- feature: get orderbook's depth
- refactor: change orderbook to async
- refactor: rename `OrderType`, `Side` constants
- refactor: rename `PlaceOrder` to `AddOrder` in orderbook
- refactor: allow `tradeChan` pass from outside when engine is created
- refactor: benchmark test

## v0.4.1 (2023-04-09)

- feature: New matching engine which manages multiple order books.
- feature: add `CancelOrder` function to orderbook

## v0.4 (2023-04-08)

- refactor: add OrderType field (`market`, `limit`, `ioc`, `post_only`, `fok`)
- refactor: add more testcase
- fix: fix market order size issue

## v0.3 (2022-06-05)

- add order book update event
- add time in force strategy (gtc, ioc)
- add "post only" behavior

## v0.2 (2022-05-11)

- use skiplist algorithm for order queue
- redesign matching engine arch
- add benchmark test

## v0.1

- just for fun
