# CHANGELOG

## [unschedule]

- breaking: `OrderBookLog.Size` and `OldSize` are gone; each quantity has one meaning. `MatchSize` is the traded size of a match. `Remaining` is an order's total remaining size (visible plus hidden) on an open, an amend, a cancel and a base-sized reject. `OldRemaining` is an amend's total before the amend. `VisibleAdded` is the visible size an open puts on the book. `QuoteRemaining` is the quote a quote-sized market reject did not spend. `VisibleRemoved` is unchanged. Before, `Size` was a trade's size on a match, the visible size on an open, the total on a cancel and an amend, and the base or quote remainder on a reject, and `OldSize` was read only on an amend. The table is in the README under "Log quantity fields". **Action required:** read `MatchSize` where a consumer read `Size` on a match; read `Remaining` for the total of an open, cancel or amend (an open's visible part is `VisibleAdded`, which the depth rule reads); read `OldRemaining` for `OldSize`; read `QuoteRemaining` for the quote of a reject. Depth consumers that use `DepthDeltaBuilder` or `AggregatedBook` need no change.
- breaking: `OrderBookLog.SeqID` means only the depth version. A user event (`LogTypeUser`) used to carry the upstream message queue's command sequence there; it now carries it in the new field `CmdSeqID` (`json:"cmd_seq_id,omitempty"`), and its `SeqID` is 0. `NewUserEventLog` takes the sequence as `cmdSeqID` and sets only that field. **Action required:** a consumer that read a user event's `SeqID` for the command sequence must read `CmdSeqID`.
- refactor: a priority-losing or in-place amend has one depth rule in `CalculateDepthChange`: the visible part the amend takes off the book (`VisibleRemoved`) is removed from `OldPrice`. Before, the rule compared `Size` with `OldSize` on one branch. The results do not change for any log the book produces; a pinned test covers a price change with a shrink, which the old branch would have removed at the new price.
- breaking: `AggregatedBook` takes its snapshots as `protocol.DepthDelta`: `ApplySnapshot` and the `OnRebuild` callback use the same type and the same `DepthLevel` (`udecimal` price and size) as `ApplyDelta`, with `Seq` as the version. The `Snapshot` type is removed. Before, string prices and sizes were parsed with the errors discarded, so a malformed level became zero. **Action required:** an `OnRebuild` callback returns a `*protocol.DepthDelta`, and a snapshot with a negative size or a price written twice on one side is refused with `ErrInvalidDepthDelta` and leaves the book unchanged.
- refactor: `AggregatedBook` guards its version and levels with one lock. The atomic version and the double-checked deduplication were for the removed `Replay`, and are gone. `SequenceID` reads the version under the lock.
- fix: a price is one level however it is written. `OrderBook` keyed its price-level map by the value of `udecimal.Decimal`, which includes the written scale, while the price-ordered skiplist compares by value. Two orders at `100` and `100.00` therefore got two map entries but one skiplist entry: the second order was missing from the snapshot, and an opposing taker could reach only the first. `DepthDeltaBuilder` had the same split and reported two levels. Both now key the map by a canonical price with trailing zeros removed, lossless and with no market precision, and `LevelSize` uses the same key. Prices on orders and logs keep the caller's written form. The key of a price whose coefficient needs more than 128 bits is computed through its text and allocates; prices within 128 bits do not allocate.
- breaking: one SDK call, one depth version. `OrderBook.SeqID()`, `GetDepth().UpdateID` and `OrderBookSnapshot.SeqID` advance by exactly one per call that moves visible depth, and every depth log of that call carries the new version (a sweep over several levels, an iceberg replenish, and the re-match of a priority-losing amend are one version). Before, each depth log advanced it. Refusals, admin commands, a no-op amend and a shrink inside the hidden size still do not advance it. **Action required:** as with the earlier version change, discard snapshots taken before this change or reset the readers that follow the version.
- breaking: remove `AggregatedBook.Replay` (per-log deduplication, gap detection and depth rules), together with its benchmark. The depth feed is now produced by the book and applied as a whole: `protocol.DepthDelta{Seq, Asks, Bids}` with `protocol.DepthLevel{Price, Size}`, where each size is the level's visible size after the call and zero removes the level. `DepthDeltaBuilder` (`NewDepthDeltaBuilder`, `Add`, `AddBatch`, `Flush`, `Reset`) produces it from the logs of a call and reads the sizes from the book; `AggregatedBook.ApplyDelta` applies it: one past the version sets the levels, a resend is skipped, a gap calls `OnRebuild` and is `ErrSequenceGap` if the snapshot does not connect (book unchanged), and a malformed delta is `ErrInvalidDepthDelta` (book unchanged). Both rules are shared with `CalculateDepthChange`, the single depth rule. **Action required:** a consumer must feed deltas, not logs, and redeploy from the same binary as the matching engine.
- feature: `OrderBook.LevelSize(side, price)` returns the visible size at a price (zero when absent) and `OrderBook.BestPrice(side)` the best price of a side; both allocate nothing. Price units are pooled, and a recycled unit starts with no total, count or hidden size.
- fix: fill-or-kill feasibility counts iceberg hidden size. A level's hidden size is kept in its price unit and maintained on insert, removal, replenish and in-place amend. Before, a FOK was refused when the visible size alone was short, although a sweep reloads the hidden part: a sell iceberg at 100 with total 10 and visible 2, and a FOK buy at 100 for 5, was refused `insufficient_size`; it now fills 2 + 2 + 1 as the same limit order does.
- breaking: `AmendOrderRequest` carries `StepSize`, and the binary encoding writes and reads it. A new size that is not a whole number of steps is refused `invalid_payload`; a zero step checks nothing, as for a new order. This closes the gap noted for quantity-sized orders above. **Action required:** redeploy every producer and consumer from the same binary; old amend bytes do not decode.
- breaking: book logs state their own depth effect. `OrderBookLog` gains `VisibleRemoved`, the visible size a cancel or amend takes off the book (`json:"visible_removed,omitzero"`). `Size` and `OldSize` keep meaning the order total (visible plus hidden), which is what releasing funds needs. Before this, `AggregatedBook` read `Size`/`OldSize` as depth, so it double-counted a priority-losing amend (an amend and the open that follows both added the new size), and removed an iceberg's hidden size on cancel and in-place shrink. `AggregatedBook` now calls `CalculateDepthChange`, the only depth rule, and no longer has its own copy. A decrease that stays inside an iceberg's hidden size changes no visible depth, so it repeats the depth version like a no-op amend instead of advancing it. `AggregatedBook` now matches `OrderBook` level by level for iceberg, amend and every market and time-in-force order, pinned by a differential test over 120k commands with a mid-stream snapshot. **Action required:** redeploy every `AggregatedBook` consumer from the same binary as the matching engine; a cancel or amend log without `VisibleRemoved` is read as removing no visible size.
- refactor: `handleIOCOrder` and `handleMarketOrder` now share one internal taker walk (`takeLiquidity`) instead of two near-identical loops. Each handler stays the entry point of its order type and only decides the three things that differ: whether `Price` is a required bound (IOC) or an optional one (Market, where zero means none), the reject reason when the bound stops the walk (`price_mismatch` for IOC, `price_protection` for Market), and the quote-sized stop condition with its step size (Market only). Behaviour is unchanged: the match and reject logs, their fields and order, the reason codes and the public API are the same, pinned by tests that send one request down both entry points and compare the logs entry by entry.
- feature: a market order may carry a **protection price** in `Price`. A buy takes only asks at or below it and a sell takes only bids at or above it; the level exactly at the bound fills. The walk stops at the first level beyond the bound and the remainder ends as a new reject reason `price_protection` (`RejectReasonPriceProtection`), with `Size` = the unfilled quantity or quote, the same shape as the `no_liquidity` remainder. This applies to both the quantity path and the quote path, on both sides. `Price = 0` is unchanged. A market order with `Price < 0` is refused as `invalid_payload`. Limit, IOC, FOK and iceberg behaviour is unchanged, and snapshots are not affected. Market orders never rest, so the protection price is not kept in snapshots.
- breaking: carry `StepSize` on the binary wire for `PlaceOrderRequest`. The JSON tag existed but `MarshalRequest`/`UnmarshalRequest` never wrote or read the field, so a binary round trip silently zeroed it and the quote path lost its floor unit. **Action required:** redeploy every producer and consumer from the same binary — old bytes no longer decode, and there is no fallback. The round-trip test now carries a non-zero step precisely so a dropped field fails instead of passing silently.
- breaking: refuse quantity-sized orders whose size is not a whole number of the carried step (`Size % StepSize != 0` → `invalid_payload`). This covers every order that names a base size — limit, market orders sized in base (both sides), IOC, FOK and post-only — plus a non-zero iceberg `VisibleSize`, because the book never floors the quantity path and whatever arrives is what the fills carry. Measured from zero: the book holds no market rules and never sees `minQuantity`, so callers that own the rules keep checking against the minimum — the two agree wherever the minimum is itself a whole number of steps. Skipped when no step travels with the order, so callers that never send one keep the old behavior. **Behavior change:** a dust-sized FOK taker used to fill in full and now is refused at the door; resting dust offers are still taken whole, so orders placed under an older finer step stay reachable. Amended sizes are checked too (see the amend step entry below).

- breaking: remove market lifecycle from the library — `CreateMarket`, `SuspendMarket`, `ResumeMarket` and `UpdateConfig` are gone, along with `OrderBookState` (three states, none of which any remaining command could set), `RejectReasonMarketSuspended`, `RejectReasonMarketHalted`, and the whole `lotSize` mechanism (`DefaultLotSize`, `WithLotSize`, `OrderBookSnapshot.MinLotSize`). An order book matched orders routed to it; the set of tradeable markets is decided upstream, so `MatchingEngine` now obtains books only by restoring them from a snapshot, and callers that manage books themselves use the synchronous `NewOrderBook` API. **Action required:** discard snapshots taken before this change — `SnapshotSchemaVersion` stays `1` and `Restore` accepts an old snapshot silently, but the removed fields are simply not there. The suspended-cancel behaviour that went with it was not a defect: it is what a maintenance-window freeze means, and a caller that needs one should refuse orders upstream rather than here.
- breaking: `lotSize` was a correctness hole, not just a stale feature. `UpdateConfig` assigned it unconditionally, without looking at whether the book held orders or whether anyone had a balance, and `stepSize` is the unit of account for the base asset — a balance has to be a whole number of steps or it cannot be reconciled against the ledger. Nothing sent that command, so it had never fired.
- breaking: add `StepSize` to `PlaceOrderRequest`, and floor the quote conversion with it. The floor happens **before** the minimum with the resting order, not after: flooring after sends an offer placed under an older, finer step to zero, rejects an order the book could have filled, and lets one holder's dust stop every order behind it. Only the quote path is floored — when both sizes are whole steps the result already is, so flooring a quantity-based order would change nothing and cost a division on the hot path. `IsZero` then replaces the old below-lot-size check as the loop guard, and is a stricter one: after that point a non-zero match size is always at least one whole step. A quote-sized market order with no step size is rejected as `invalid_payload` rather than defaulted, because a default unit would silently write balances the ledger cannot represent.
- breaking: redefine an order book's `SeqID` from "increments on every emitted log" to the **book depth version**, advancing only when resting size is added or removed (open, match, cancel, amend). Refusal logs, admin logs (`suspend`, `resume`, `update_config`) and `market_created` now carry `0`, and a no-op amend (new price and size both equal the current ones, which mutates no order field) repeats the current version. This makes `GetDepth().UpdateID` and `OrderBookSnapshot.SeqID` a per-market "did depth change" signal rather than "did anything happen", and it lets a consumer project depth by forwarding only the four depth-changing log types and still hand `AggregatedBook.Replay` a contiguous version — previously every refused order produced a version jump that a projector would read as a lost update and rebuild from. `LogTypeUser` is the one documented exception: `MatchingEngine.handleUserEvent` puts the upstream MQ command sequence in the same field, so a depth projection must filter user events out. **Action required:** discard snapshots taken before this change (or reset the downstream reader) — `SnapshotSchemaVersion` stays `1`, `Restore` accepts an old snapshot silently, and the first depth log after recovery reports a jump that costs the reader one rebuild.
- breaking: `LogBatch` has no `Release`. A book returns its own batch from `PlaceOrder`, `CancelOrder` and `AmendOrder`, and that batch and its logs are valid until the next call on the same book, which reclaims them when it starts. The batch is no longer taken from a pool, so the caller has nothing to hand back: a caller that called `Release` once per batch could return the next call's logs to the pool by calling it twice, and nothing enforced the rule. The engine keeps its own batch for the rejects and user events it publishes itself, under the same rule. Different books keep different batches, so calls on several books may interleave. A `Publisher` or caller that keeps `*OrderBookLog` or the `Logs` slice past the next call on that book reads another call's logs; copy first. `MemoryPublishLog` already copies and is unaffected. Steady-state allocations do not rise: `AllocsPerRun` over 1000 calls drops from 3 to 0 for a fill and from 3 to 0 for a cancel, and from 4 to 1 for an order that rests. **Action required:** delete every `batch.Release()` call; copy any log the caller keeps past the next call on its book.
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
