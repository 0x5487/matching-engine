# Benchmark Workloads Design

## Goal

Define a stable benchmark matrix for the matching engine so performance numbers can be compared across releases and code changes without mixing different workload shapes or submit modes.

The benchmark suite should answer these questions:

- How fast is the engine in a pure crossing scenario?
- How fast is the engine under a production-like warm book where most orders still match?
- How much does `SubmitAsyncBatch` improve throughput relative to single-order `SubmitAsync` under the same workload?

## Scope

This design covers benchmark workload definitions, timer boundaries, metrics, and benchmark naming.

This design does not change engine behavior, queue data structures, or matching rules.

## Benchmark Matrix

The first version of the suite should contain four end-to-end benchmarks:

- `BenchmarkCrossing_EndToEnd_Single`
- `BenchmarkCrossing_EndToEnd_Batch10`
- `BenchmarkProductionWarmBook_EndToEnd_Single`
- `BenchmarkProductionWarmBook_EndToEnd_Batch10`

Each benchmark measures submit-to-drain latency and throughput, not setup cost.

## Timer Boundary

Each benchmark must follow the same timing contract:

1. Prepare the engine, market, command pools, and warmup state before `b.ResetTimer()`.
2. Start timing immediately before the measured submit loop.
3. Stop timing only after a sentinel query confirms that all previously submitted commands have been processed.

This keeps the benchmark end-to-end while excluding setup and warmup.

## Workload 1: Crossing

### Purpose

Measure matching throughput in a shallow-book scenario where nearly every order pair crosses immediately.

### Shape

- Each measured loop produces one resting order and one immediately matching order.
- Use a fixed price and fixed size for the first version.
- Keep the book shallow by consuming liquidity almost immediately.
- Measure both single submit and batch submit modes with the same logical order flow.

### Expected Use

This benchmark acts as the upper-bound matching throughput check for the current engine.

## Workload 2: Production Warm Book

### Purpose

Measure end-to-end throughput under a production-like book shape where liquidity is concentrated near the touch and most measured orders still match.

### Warmup Phase

Warmup runs before `b.ResetTimer()` and establishes a stable book shape:

- Build bid and ask liquidity around the touch.
- Roughly 80% of resting volume should be concentrated in the nearest 20 price levels.
- The remaining 20% should be distributed across deeper levels behind those top 20 levels.
- Warmup should leave both sides populated so the measured phase does not start from an empty book.

### Measured Phase

The measured flow should maintain a target order-flow mix:

- 70% of submitted orders should cross existing liquidity and trade immediately.
- 30% of submitted orders should rest near the touch and replenish the book.
- Buy and sell flow should stay approximately balanced.
- The measured phase should not drain the book into an unrealistic state.

### Expected Use

This benchmark represents the primary production-like benchmark for release-to-release comparisons.

## Submit Modes

Each workload should be executed in two submit modes:

- `Single`: one command per `SubmitAsync` call
- `Batch10`: ten commands per `SubmitAsyncBatch` call

The logical order sequence must remain equivalent between the two modes so the benchmark compares submission overhead rather than changing the workload.

The first version should use `Batch10` only because the product-level batch limit is expected to be ten orders per submission.

## Data Model

The first benchmark version should keep data simple and deterministic:

- Fixed order size of `1`
- Deterministic command pools built before timing
- Stable random seed when randomness is used
- Unique order identifiers throughout the measured phase

This avoids introducing extra variance from size distributions or random identifier collisions.

## Metrics

Every benchmark should report:

- `ns/op`
- `orders/sec`
- `B/op`
- `allocs/op`

Each benchmark should also log the final order-book depth so the resulting book shape can be checked during benchmark review.

## Validation Rules

The benchmark suite should satisfy these validation rules:

- The same workload run twice should produce similar final book depth.
- Single and batch benchmarks for the same workload should operate on equivalent command streams.
- The production warm-book benchmark should preserve a plausible near-touch depth throughout the run.
- No warmup or command generation work should be included inside the timer.

## Risks

- If the replenishment ratio is too low, the warm-book benchmark will slowly degrade into a drained crossing benchmark.
- If the replenishment ratio is too high, the benchmark will become a resting-heavy maintenance test instead of a production-like matching test.
- If single and batch modes do not share the same logical stream, throughput differences will be hard to interpret.

## Recommendation

Implement the four benchmarks above first and use them as the standard benchmark matrix.

Do not add additional workloads until these four produce stable and interpretable numbers.
