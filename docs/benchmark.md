# Benchmark: synchronous OrderBook suite

This suite measures `*OrderBook` the way trading-poc calls it: one goroutine, with `PlaceOrder`, `CancelOrder`,
`GetDepth`, `Snapshot`, `Restore` and `AggregatedBook.Replay` called directly. It does not go through
`MatchingEngine`, the disruptor or a channel. The engine-path benchmarks (`make bench`) are kept for reference at
the end and are not comparable with these numbers.

Source: `order_book_sync_bench_test.go` (package `match`, benchmarks `BenchmarkSync*`).

## Environment

- CPU: AMD Ryzen 7 PRO 4750U with Radeon Graphics, 8 cores / 16 threads (`nproc` 16), 4 MiB L3
- OS: WSL2 (Linux 6.18, Microsoft hypervisor) on a Windows host; 12 GB RAM
- Go: go1.27.1 linux/amd64 (`go.mod`: `go 1.26.0`); GOMAXPROCS 16 (default)
- SDK commit: 326ee62 plus the suite in this change
- Runs were made alone: every heavy run took `flock -o /tmp/trading-poc-heavy.lock`, and no other build, test or
  gate was running when a run started. The WSL2 host itself is shared, so run-to-run variance is large (see
  "Variance").

## Commands

```sh
make bench-sync                     # go test -run=none -benchmem -bench "^BenchmarkSync" -count 10 .
make prof-cpu                       # CPU profile of the suite, top by flat
make prof-mem                       # memory profile of the suite, top by alloc_objects
go test -run=^$ -bench='^BenchmarkSyncS3_Sweep$/^k=10_same_level$' -benchmem -benchtime=3s -cpuprofile cpu.out .
```

Per-scenario profiles were taken with a test binary built by `go test -c`, run with
`-test.bench=<one scenario>`, `-test.benchtime=3s`, `-test.cpuprofile` and `-test.memprofile`.

## Method

- **Steady state.** A 1:1 cross and a rest followed by its cancel leave the book as they found it. Every other
  scenario runs in chunks of 1024 ops: an untimed prepare step restores the book, then only the chunk's ops are
  timed (`syncBenchRun`). The timer is stopped twice per chunk, never per op.
- **Warm book.** 200 levels per side, 5 orders each. Bids rest at 49999 and below, asks at 50002 and above;
  50001 is empty, so a maker there is the best ask (the trading-poc 1:1 pattern).
- **Outcome checks.** Each timed op counts the outcomes that do not match the scenario name (for example a cross
  that does not fill the named maker, or a cancel that does not find its order). The count must be zero after
  the timer stops, otherwise the benchmark fails.
- **Units.** `ns/op` is per op of the scenario: S2 per order (maker and taker each count one); S3 per sweep
  (divide by k for per consumed maker); S4 per cancel; S7 per log replayed or per lookup; S8 per snapshot or per
  restore of a 10000-order book; Mixed per unit, where every unit is two book ops.
- **`allocs/op`.** The testing package truncates `allocs/op` to an integer, so a benchmark that allocates one
  object per two ops reports 0. Use the alloc_objects profiles for exact counts.

## Scenarios

| Benchmark | What is timed |
| :--- | :--- |
| `BenchmarkSyncS1_PlaceResting` | `PlaceOrder` of a limit order that rests, at varied levels on both sides; refill is `CancelOrder` |
| `BenchmarkSyncS2_Cross1to1` | maker rests at 50001, taker of equal size crosses it; each order counts one |
| `BenchmarkSyncS3_Sweep/k=K_{same_level,across_levels}` | IOC taker of size K consumes K makers, all at one level or one per level; book holds only the makers |
| `BenchmarkSyncS4_CancelDeep` | `CancelOrder` at a random position in 1000 levels x 10 orders per side; refill re-places the same orders |
| `BenchmarkSyncS5_MarketSlippage` | `GetDepth(1)`, protection price = best ask x 1.01, then a market buy of size 1; refill places sells at the best ask |
| `BenchmarkSyncS6_GetDepth20` | `GetDepth(20)` on the warm book; read only |
| `BenchmarkSyncS7_ReplayCross` | `AggregatedBook.Replay` of every log the warm book and 1024 S2 crosses produce, into a fresh view per chunk |
| `BenchmarkSyncS7_DepthLookup` | `AggregatedBook.Depth(side, price)` over 400 levels of a replayed warm book |
| `BenchmarkSyncS8_Snapshot` | `Snapshot()` of 1000 levels x 5 orders per side (10000 orders) |
| `BenchmarkSyncS8_Restore` | `Restore` of that snapshot into fresh books, 8 per chunk |
| `BenchmarkSyncMixed` | seeded schedule: 80% crosses (2 ops), 10% rest and cancel (2 ops), 10% market buys with slippage limit and a replenishing sell (2 ops) |

## Results

`go test -run=^$ -bench=^BenchmarkSync -benchmem -count=10`, summarised with benchstat v0.0.0-20261009192801-be2c69fb417e
(`±` is benchstat's 95% interval). Rows whose `±` reached 5% were rerun once with the same command; both values
are shown.

| Benchmark | sec/op | rerun sec/op | B/op | allocs/op |
| :--- | ---: | ---: | ---: | ---: |
| S1 PlaceResting | 448.3 ns ±10% | 407.6 ns ±5% | 0 | 0 |
| S2 Cross1to1 (per order) | 950.9 ns ±21% | 714.1 ns ±7% | 32 | 0 |
| S3 k=1 same_level | 809.7 ns ±14% | 839.4 ns ±29% | 0 | 0 |
| S3 k=1 across_levels | 679.9 ns ±6% | 672.4 ns ±5% | 0 | 0 |
| S3 k=10 same_level | 4.678 µs ±12% | 5.491 µs ±7% | 0 | 0 |
| S3 k=10 across_levels | 4.423 µs ±5% | 4.592 µs ±6% | 0 | 0 |
| S3 k=100 same_level | 40.18 µs ±5% | 40.17 µs ±2% | 4 | 0 |
| S3 k=100 across_levels | 42.22 µs ±3% | 42.44 µs ±6% | 35 | 0 |
| S4 CancelDeep | 943.4 ns ±11% | 927.3 ns ±5% | 0 | 0 |
| S5 MarketSlippage | 1.200 µs ±2% | - | 196 | 7 |
| S6 GetDepth20 | 6.559 µs ±2% | - | 2.46 KiB | 83 |
| S7 ReplayCross (per log) | 699.0 ns ±3% | - | 33 | 0 |
| S7 DepthLookup | 267.7 ns ±7% | 264.2 ns ±4% | 0 | 0 |
| S8 Snapshot (10k orders) | 1.828 ms ±7% | 1.863 ms ±3% | 2.06 MiB | 10000 |
| S8 Restore (10k orders) | 5.275 ms ±6% | 5.308 ms ±10% | 3.30 MiB | 12140 |
| Mixed (per unit) | 1.332 µs ±5% | 1.337 µs ±5% | 69 | 1 |

S3 rows are per sweep; per consumed maker, divide by K.

### Variance

Rows still above 5% after the rerun, and the spread between the two runs, show that this host is not quiet
enough for a 5% gate: S2 moved from 951 ns to 714 ns, and S3 k=1 same_level from 810 ns to 839 ns with a
±29% interval in the rerun. No number here should be read at better than roughly 10% without a repeat.

## Engine path (reference)

`make bench`, `-count 1`. These go through `MatchingEngine`, the disruptor and a channel, so they are not the
numbers above.

| Benchmark | ns/op | B/op | allocs/op |
| :--- | ---: | ---: | ---: |
| BenchmarkCrossing_EndToEnd_Single | 1614 | 64 | 1 |
| BenchmarkCrossing_EndToEnd_Batch10 | 2655 | 96 | 1 |
| BenchmarkProductionWarmBook_EndToEnd_Single | 2055 | 108 | 0 |
| BenchmarkProductionWarmBook_EndToEnd_Batch10 | 1458 | 124 | 0 |

## Known limits

- CPU and memory profiles include the untimed refill (for example `CancelOrder` in S1's profile and the
  `NewPooledSkiplistWithOptions` call in S4's), because `pprof` does not see the timer. Read profile shares with
  that in mind.
- The profiles were taken with `-test.benchtime=3s`, so their ns/op is slower than the table (profiling adds
  overhead).
- The suite measures a single goroutine on a shared WSL2 host. It is not a cloud measurement.
