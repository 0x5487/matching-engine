# CHANGELOG

## [unschedule]

- breaking: remove market lifecycle from the library — `CreateMarket`, `SuspendMarket`, `ResumeMarket` and `UpdateConfig` are gone, along with `OrderBookState` (three states, none of which any remaining command could set), `RejectReasonMarketSuspended`, `RejectReasonMarketHalted`, and the whole `lotSize` mechanism (`DefaultLotSize`, `WithLotSize`, `OrderBookSnapshot.MinLotSize`). An order book matched orders routed to it; the set of tradeable markets is decided upstream, so `MatchingEngine` now obtains books only by restoring them from a snapshot, and callers that manage books themselves use the synchronous `NewOrderBook` API. **Action required:** discard snapshots taken before this change — `SnapshotSchemaVersion` stays `1` and `Restore` accepts an old snapshot silently, but the removed fields are simply not there. The suspended-cancel behaviour that went with it was not a defect: it is what a maintenance-window freeze means, and a caller that needs one should refuse orders upstream rather than here.
- breaking: `lotSize` was a correctness hole, not just a stale feature. `UpdateConfig` assigned it unconditionally, without looking at whether the book held orders or whether anyone had a balance, and `stepSize` is the unit of account for the base asset — a balance has to be a whole number of steps or it cannot be reconciled against the ledger. Nothing sent that command, so it had never fired.
- breaking: add `StepSize` to `PlaceOrderRequest`, and floor the quote conversion with it. The floor happens **before** the minimum with the resting order, not after: flooring after sends an offer placed under an older, finer step to zero, rejects an order the book could have filled, and lets one holder's dust stop every order behind it. Only the quote path is floored — when both sizes are whole steps the result already is, so flooring a quantity-based order would change nothing and cost a division on the hot path. `IsZero` then replaces the old below-lot-size check as the loop guard, and is a stricter one: after that point a non-zero match size is always at least one whole step. A quote-sized market order with no step size is rejected as `invalid_payload` rather than defaulted, because a default unit would silently write balances the ledger cannot represent.
- breaking: redefine an order book's `SeqID` from "increments on every emitted log" to the **book depth version**, advancing only when resting size is added or removed (open, match, cancel, amend). Refusal logs, admin logs (`suspend`, `resume`, `update_config`) and `market_created` now carry `0`, and a no-op amend (new price and size both equal the current ones, which mutates no order field) repeats the current version. This makes `GetDepth().UpdateID` and `OrderBookSnapshot.SeqID` a per-market "did depth change" signal rather than "did anything happen", and it lets a consumer project depth by forwarding only the four depth-changing log types and still hand `AggregatedBook.Replay` a contiguous version — previously every refused order produced a version jump that a projector would read as a lost update and rebuild from. `LogTypeUser` is the one documented exception: `MatchingEngine.handleUserEvent` puts the upstream MQ command sequence in the same field, so a depth projection must filter user events out. **Action required:** discard snapshots taken before this change (or reset the downstream reader) — `SnapshotSchemaVersion` stays `1`, `Restore` accepts an old snapshot silently, and the first depth log after recovery reports a jump that costs the reader one rebuild.
- breaking: `LogBatch.Release()` now returns the batch's logs to the `OrderBookLog` pool along with the batch itself. Logs are zeroed and recycled, so a `Publisher` or caller that retains `*OrderBookLog` — or the `Logs` slice itself — past `Release()` will read recycled memory; copy first if the logs are needed afterwards. `MemoryPublishLog` already copies and is unaffected. This also fixes the pool being unreachable for callers of the synchronous `NewOrderBook` API: `releaseBookLog` was unexported and `Release()` never touched the logs, so every synchronous caller paid one permanent ~376 B heap allocation per log. Synchronous callers now drop from 5 to 3 allocs/op and 1062 to 292 B/op on a place+cancel round trip (~1.9 µs → ~1.3 µs, -32%), removing roughly 557 MB/s of garbage per single-threaded actor, and batches of any size reach 0 allocs/op.
- feature: export pure synchronous `OrderBook` API (`NewOrderBook`, `PlaceOrder`, `CancelOrder`, `AmendOrder`, `Snapshot`, `GetDepth`) returning `*LogBatch` directly for seamless single-actor state machine integration.
- feature: implement `AggregatedBook` read-replica state machine with event replay (`Open`, `Match`, `Cancel`, `Amend`), sequence deduplication, automatic gap recovery with `OnRebuild` callback, and thread-safe `GetDepth` queries for downstream market data services.
- perf: optimize `AggregatedBook.Replay` to achieve over 7M events/sec throughput with 0 B/op and 0 allocs/op.
- test: add `bench-aggrbook` Makefile target and comprehensive unit, concurrency (`-race`), and gap detection tests for `AggregatedBook`.
- feature: decouple `OrderBook` from background I/O and `Publisher`, giving callers direct memory ownership of matching logs.
- breaking: remove all `time.Now()` calls across the entire production codebase to guarantee 100% deterministic Replicated State Machine (RSM) execution and replay consistency.
- feature: make skiplist PRNG seed deterministic and configurable via `WithSkiplistSeed(seed int64)` option, eliminating random height variances between nodes during event replay.
- feature: allow passing authoritative logical timestamps to `TakeSnapshot(ctx, outputDir, timestamp...)` for consistent snapshot metadata.
- breaking: refactor `MatchingEngine` API to a unified "Full-Struct" pattern and implement high-performance manual binary serialization for all command parameters.
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
