# Feature Specification: Depth Feed

**Status**: Implemented

## 1. Contract

An order book's depth version, `OrderBook.SeqID()`, advances by exactly one for each SDK call that moves visible depth. Every depth log of that call carries the new value: open, match, cancel, and amend logs, including the replenish open and the re-match of a priority-losing amend. Refusals, admin commands, a no-op amend, and a shrink inside the hidden size do not advance it.

A consumer follows the book with `protocol.DepthDelta`:

```go
type DepthDelta struct {
    Seq  uint64       // depth version after the call
    Asks []DepthLevel // changed sell levels, price ascending
    Bids []DepthLevel // changed buy levels, price descending
}

type DepthLevel struct {
    Price udecimal.Decimal
    Size  udecimal.Decimal // visible size of the level after the call; 0 removes it
}
```

Each level holds the absolute visible size, not a change, so a follower sets it and never needs the earlier size.

## 2. Producer

`DepthDeltaBuilder` turns the logs of one or more calls into a delta:

- `Add` and `AddBatch` record the logs. Only logs whose `CalculateDepthChange` moves visible size are kept, so the depth rule is the one in `CalculateDepthChange` and nowhere else.
- `Flush(book, &delta)` reads each recorded price from the book with `LevelSize`, sorts the levels, and sets `Seq` to the version of the last depth log. It reports false, with an empty delta, when nothing moved.
- `Reset` forgets the recorded logs.

`Flush` must run before the next call changes the book, because it reads the sizes that hold then. A caller that adds the logs of several calls before one `Flush` gets a single delta with each level once at its final size; its `Seq` is the last version, and such a delta is the caller's own output, not a step a follower can apply.

## 3. Follower

`AggregatedBook.ApplyDelta(*DepthDelta)` with the follower at version `cur`:

- A malformed delta (nil, `Seq` zero, a negative size, or a side not strictly ordered by price) returns `ErrInvalidDepthDelta` before anything is read.
- `Seq == cur+1` sets the levels and the version.
- `Seq <= cur` is a resend and returns nil with no change.
- `Seq > cur+1` is a gap. Without `OnRebuild`, `ErrSequenceGap`. With it, the snapshot is fetched; if it reaches `Seq-1` or further it is applied and the delta is applied or skipped as above; otherwise `ErrSequenceGap` is returned and the snapshot is not applied.

Every error leaves the follower unchanged.

## 4. Queries

`OrderBook.LevelSize(side, price)` is the visible size at a price, zero when absent. `OrderBook.BestPrice(side)` is the best price of a side and false when the side is empty. Neither allocates.
