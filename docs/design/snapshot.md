# Feature Specification: Snapshot & Restore

**Status**: Implemented
**Date**: 2025-12-29
**Reviewer**: Bob, Architect

## 1. Background

Snapshots provide a recovery point for the in-memory matching engine. After restart, the engine can restore from snapshot state and then resume replay from the external command source using the returned `GlobalLastCmdSeqID`.

The current implementation aims for:

- consistent point-in-time capture on the engine event loop
- no direct concurrent reads of mutable order book state
- compact on-disk storage with integrity checks

## 2. Current Mechanism

The engine does **not** use a `CmdSnapshot` marker command or a Chandy-Lamport-style broadcast.

Current flow:

1. `TakeSnapshot()` sends an internal `engineSnapshotQuery` into the shared ring buffer.
2. The engine event loop handles that query on the same consumer goroutine that processes writes.
3. For each managed market, the engine builds an `OrderBookSnapshot` synchronously from current in-memory state.
4. The snapshots are serialized and written into `snapshot.bin`.
5. A footer is appended to `snapshot.bin` describing each market segment.
6. `metadata.json` is written with global metadata, including `GlobalLastCmdSeqID` and whole-file checksum.
7. A temporary directory is atomically renamed into place.

This gives consistent capture relative to the event loop without introducing a public snapshot command into the replay stream.

## 3. File Layout

### 3.1 `metadata.json`

`metadata.json` currently stores global metadata:

- `schema_version`
- `timestamp`
- `global_last_cmd_seq_id`
- `engine_version`
- `snapshot_checksum`

It does **not** store per-market offsets.

### 3.2 `snapshot.bin`

`snapshot.bin` stores:

1. serialized market snapshots written sequentially
2. a JSON footer containing per-market `{market_id, offset, length, checksum}`
3. a 4-byte footer length trailer

The footer is the source of truth for per-market segment lookup during restore.

## 4. Snapshot Data Model

Current snapshot content includes:

```go
type OrderBookSnapshot struct {
    MarketID     string
    SeqID        uint64
    LastCmdSeqID uint64
    TradeID      uint64
    Bids         []*Order
    Asks         []*Order
    State        protocol.OrderBookState
    MinLotSize   udecimal.Decimal
}
```

`Order` snapshot content includes current visible size, timestamp, and iceberg-related fields used by restore.

## 5. Restore Behavior

Current restore flow:

1. read and validate `metadata.json`
2. compute and verify whole-file checksum of `snapshot.bin`
3. read and validate footer length
4. load footer and validate each segment bound
5. verify per-segment checksum
6. deserialize each market snapshot
7. rebuild managed order books in memory
8. return `SnapshotMetadata` to the caller

The engine does not automatically restart replay. The caller is responsible for using `GlobalLastCmdSeqID` as the replay watermark in the external message source.

## 6. Consistency Rules

- snapshots are taken on the same event loop that processes writes
- `GlobalLastCmdSeqID` is the maximum observed `LastCmdSeqID` across markets
- restored market state includes order book lifecycle state and lot-size configuration
- malformed footer or segment bounds must fail restore rather than risking invalid reads or excessive allocations

## 7. Validation Guidance

Validate the current implementation with the following checks:

- snapshot files are atomically written via temporary directory rename
- restore detects whole-file checksum mismatch
- restore detects invalid footer length or invalid segment bounds
- restored engine state matches pre-snapshot order counts and state
- replay coordinator can resume from `GlobalLastCmdSeqID` returned by restore

## 8. Notes

- Historical references to `CmdSnapshot`, marker broadcasts, or per-market offsets embedded in `metadata.json` are obsolete.
- This document describes the current implemented format and restore contract.
