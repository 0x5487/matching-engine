# CHANGELOG

## [unreleased]

- feature: extend **Future Pattern** to all management commands (`CreateMarket`, `SuspendMarket`, `ResumeMarket`, `UpdateConfig`) and query commands (`Depth`, `GetStats`) for synchronous-like waiting and consistent API experience.
- feature: introduce **IdleStrategy** (BusySpin, Yielding) for the RingBuffer to allow flexible waiting behaviors and move waiting logic out of the core Disruptor.
- feature: implement **Context-Aware Command Submission** in `MatchingEngine`, allowing `PlaceOrder`, `CancelOrder`, etc., to respect context deadlines or cancellations during the RingBuffer submission phase.
- breaking: standardize all public `MatchingEngine` methods (`GetStats`, `Depth`, `TakeSnapshot`) to require a `context.Context` parameter for better consistency and resource control.
- breaking: remove `Metadata` field from all command structures to improve performance and enforce separation of concerns between business logic and transport-layer tracing.
- fix: resolve **Response Channel Pollution (ABA issue)** by abandoning pooled channels upon `Future.Wait` timeout or cancellation, ensuring late responses from the engine do not interfere with subsequent requests.
- fix: ensure all management commands resolve their Future with an error immediately upon **payload deserialization failure**, preventing the caller from hanging until context timeout.
- fix: resolve Future hanging in `processCommand` when targeting a non-existent market by immediately reporting `ErrNotFound`.
- fix: unify missing market errors to `match.ErrNotFound` across both query and command paths for consistent error handling using `errors.Is`.
- refactor: translate all Traditional Chinese comments in `engine.go` and `engine_test.go` to English to comply with project engineering standards.

## v0.8.0 (2026-04-02)

- feature: add **Iceberg Orders** support with `VisibleSize` parameter, automatic replenishment, priority reset, and deterministic timestamps. See [doc/features/iceberg.md](doc/features/iceberg.md).
- feature: add **Management Commands** (Create/Suspend/Resume/UpdateConfig) with centralized event sourcing routing, strict state management, and snapshot persistence.
- feature: add **LotSize configuration** via `WithLotSize()` option for `NewOrderBook()`. Prevents infinite loops in Market orders using `quoteSize` by checking minimum trade unit. Default: `1e-8`. See [doc/features/precision.md](doc/features/precision.md).
- perf: integrate custom Disruptor (MPSC RingBuffer) replacing Go channels for **~13x lower latency** (44µs → 3µs) and **zero-allocation** on hot paths. See [doc/disruptor.md](doc/disruptor.md).
- perf: implement thread-safe `GetStats()` using unified command channel to eliminate race conditions in tests.
- refactor: replace `time.Sleep` with `assert.Eventually` in unit tests for faster and more deterministic execution.
- feature: implement non-blocking snapshot/restore and refactor order structure/API for better performance and maintainability.
- perf: replace `shopspring/decimal` with `udecimal` (uint64-based) for **zero-allocation arithmetic**, reducing hot-path allocations from 27/op to 4/op.
- perf: optimize core engine performance to achieve **~3M orders/sec throughput** and **0 allocs/op** via intrusive lists, map key optimization, and strict object pooling.
- refactor: unify `UserID` type to `uint64` across all command payloads, order book logs, and internal models for better type consistency and to prevent signed integer issues.
- refactor: consolidate `MatchingEngine` into a **single-thread actor model**, enabling external goroutine CPU pinning and eliminating unnecessary context switches between order book goroutines.
- feature: add `UserEventCommand` to support generic user events (e.g., EndOfBlock, Audit).
- feat: add `PlaceOrderBatch` command to place multiple orders at once.
- refactor: add `EngineID` and `CommandID` to `MatchingEngine` and `OrderBook` to support multiple engines.
- feat: optimize high-frequency commands (`PlaceOrder`, `CancelOrder`, `AmendOrder`) with manual binary serialization (BigEndian, 17x speedup, zero-allocation).
- feat: add `FastBinarySerializer` with automatic JSON fallback for legacy compatibility and admin commands.
- fix: fix `make lint` issue.
- fix: return `ErrNotFound` immediately for `GetStats()` and `Depth()` on missing markets instead of timing out.
- fix: emit standardized `RejectLog` with `RejectReasonMarketNotFound` when write commands target a missing market.
- fix: reject invalid amend payloads instead of silently coercing malformed `NewPrice` or `NewSize` to zero values.
- fix: preserve command timestamps in reject logs and `UserEvent` logs for deterministic replay behavior.
- fix: reject invalid `CreateMarket` requests with standard reject events for duplicate markets and malformed `MinLotSize`.
- fix: harden binary command decoding to reject truncated payloads instead of accepting partial string fields.
- fix: validate snapshot footer and segment bounds during restore to prevent malformed snapshot files from causing invalid reads or excessive allocations.
- breaking: require non-empty upstream `CommandID` for all commands; remove engine-side fallback generation in helper APIs.
- breaking: update management helper signatures to require explicit `commandID` arguments; trading helpers now require payload-level `CommandID`.
- feature: emit successful management lifecycle events through `PublishLog` as `LogTypeAdmin` with `market_created`, `market_suspended`, `market_resumed`, and `market_config_updated`.
- refactor: preserve management actor identity in canonical event logs and align management command `UserID` to `uint64`.
- docs: update `README.md` to document asynchronous market creation, `ErrNotFound` read semantics, reject-log behavior, and snapshot usage.
- docs: align design documents and README with required `CommandID`, strict timestamp validation, and management success-event semantics.


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
