# Matching Engine SDK

A high-performance, in-memory matching engine SDK written in Go. Designed for crypto exchanges, trading simulations, and financial systems requiring precise and fast order execution.

## 🚀 Features

- **High Performance**: Pure in-memory matching using efficient SkipList data structures ($O(\log N)$) and **Disruptor** pattern (RingBuffer) for **microsecond latency**.
- **Deterministic Execution (RSM Ready)**: **100% free of `time.Now()`** in engine logic. All state transitions and event timestamps strictly derive from authoritative external logical clocks (e.g. sequencer timestamps), guaranteeing bit-for-bit deterministic replay across nodes in Replicated State Machines (RSM) and event sourcing architectures.
- **Dual Integration Modes**:
  - **Synchronous `OrderBook`**: Direct in-memory matching without background workers, ideal for embedding into single-goroutine state machines, Raft loops, or partitioned workers.
  - **Asynchronous `MatchingEngine`**: Complete multi-market actor with Disruptor MPSC ring buffer for high-concurrency standalone services.
- **Market Data Depth Feed (`DepthDeltaBuilder` / `AggregatedBook`)**: The book produces one `DepthDelta` per SDK call that moves visible depth, with the new absolute size of each changed level. A downstream `AggregatedBook` applies it with `ApplyDelta`, which checks the version, skips resends, and reloads a snapshot on a gap (`OnRebuild`). Thread-safe depth queries (`GetDepth`).
- **Single Thread Actor**: Adopts a **Lock-Free** architecture where a single pinned goroutine processes all state mutations. This eliminates context switching and mutex contention, maximizing CPU cache locality.
- **Concurrency Safe**: All state mutations are serialized through the RingBuffer or single-actor loops, eliminating race conditions without mutex lock contention.
- **Low Allocation Hot Paths**: Uses `udecimal` (uint64-based), intrusive lists, and object pooling to achieve **0 allocs/op** on core matching paths.
- **Multi-Market Support**: Manages multiple trading pairs (e.g., BTC-USDT, ETH-USDT) within a single `MatchingEngine` instance.
- **Comprehensive Order Types**:
  - `Limit`, `Market` (Size or QuoteSize), `IOC`, `FOK`, `Post Only`
  - **Iceberg Orders**: Support for hidden size with automatic replenishment.
- **Event Sourcing**: Generates detailed `OrderBookLog` events allowing for deterministic replay and state reconstruction.

## 📦 Installation

```bash
go get github.com/0x5487/matching-engine
```

## 🛠 Usage

### 1. Synchronous OrderBook (Direct State Machine / Event Sourcing)

For Replicated State Machines (RSM), event-sourced loops, or partitioned single-goroutine workers, use `match.OrderBook` directly. Matching executes synchronously on the caller's goroutine with zero queue hops, returning the resulting `*LogBatch` immediately.

```go
package main

import (
	"fmt"

	match "github.com/0x5487/matching-engine"
	"github.com/0x5487/matching-engine/protocol"
	"github.com/quagmt/udecimal"
)

func main() {
	// 1. Initialize OrderBook with optional configurations
	book := match.NewOrderBook(
		"BTC-USDT",
		match.WithEngineID("engine-1"),
	)

	// 2. Place a Sell Order synchronously
	// Note: Timestamp must come from your sequencer (e.g. NATS JetStream meta.Timestamp)
	sellBatch, err := book.PlaceOrder(&protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			CommandID: "cmd-sell-1",
			MarketID:  "BTC-USDT",
			UserID:    1001,
			Timestamp: 1774000000000000000, // Authoritative logical timestamp
		},
		OrderID:   "sell-1",
		OrderType: protocol.OrderTypeLimit,
		Side:      protocol.SideSell,
		Price:     udecimal.MustFromInt64(50000, 0),
		Size:      udecimal.MustFromInt64(1, 0),
	})
	if err != nil {
		panic(err)
	}
	// sellBatch is the book's own batch: valid until the next call on this book.

	// 3. Place a Matching Buy Order synchronously (matches immediately)
	buyBatch, err := book.PlaceOrder(&protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			CommandID: "cmd-buy-1",
			MarketID:  "BTC-USDT",
			UserID:    1002,
			Timestamp: 1774000000000001000,
		},
		OrderID:   "buy-1",
		OrderType: protocol.OrderTypeLimit,
		Side:      protocol.SideBuy,
		Price:     udecimal.MustFromInt64(50000, 0),
		Size:      udecimal.MustFromInt64(1, 0),
	})
	if err != nil {
		panic(err)
	}

	// 4. Immediately inspect match results (e.g. for atomic balance settlement)
	for _, log := range buyBatch.Logs {
		if log.Type == protocol.LogTypeMatch {
			fmt.Printf("[MATCH] TradeID: %d, Price: %s, Size: %s, Maker: %s, Taker: %s\n",
				log.TradeID, log.Price, log.MatchSize, log.MakerOrderID, log.OrderID)
		}
	}

	// 5. Query Depth or Take In-Memory Snapshot
	depth := book.GetDepth(10)
	if len(depth.Asks) > 0 {
		fmt.Printf("Best Ask: %s\n", depth.Asks[0].Price)
	}

	snapshot := book.Snapshot()
	restoredBook := match.NewOrderBook("BTC-USDT")
	restoredBook.Restore(snapshot)
}
```

### 2. Asynchronous Matching Engine (Disruptor / Actor Pattern)

```go
package main

import (
	"context"
	"fmt"
	"time"

	match "github.com/0x5487/matching-engine"
	"github.com/0x5487/matching-engine/protocol"
	"github.com/quagmt/udecimal"
)

func main() {
	ctx := context.Background()

	// 1. Create a PublishLog handler (implement your own for non-memory usage)
	publish := match.NewMemoryPublishLog()

	// 2. Initialize the Matching Engine
	engine := match.NewMatchingEngine("engine-1", publish)

	// 3. Start the Engine (Actor Loop)
	// This must be run in a separate goroutine
	go func() {
		if err := engine.Run(); err != nil {
			panic(err)
		}
	}()

	// 4. Markets arrive with a snapshot. There is no create-market command: the
	// market list belongs to whoever serves the API, and this library's job is
	// matching. To start from scratch rather than from a snapshot, drive
	// NewOrderBook directly and register it.

	// 5. Place a Sell Limit Order
	sellReq := &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			CommandID: "sell-1-cmd",
			MarketID:  "BTC-USDT",
			UserID:    1001,
			Timestamp: time.Now().UnixNano(),
		},
		OrderID:   "sell-1",
		OrderType: protocol.OrderTypeLimit,
		Side:      protocol.SideSell,
		Price:     udecimal.MustFromInt64(50000, 0), // 50000
		Size:      udecimal.MustFromInt64(1, 0),     // 1.0
	}
	if err := engine.PlaceOrderAsync(ctx, sellReq); err != nil {
		fmt.Printf("Error placing sell order: %v\n", err)
	}

	// 6. Place a Buy Limit Order (Matches immediately)
	buyReq := &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			CommandID: "buy-1-cmd",
			MarketID:  "BTC-USDT",
			UserID:    1002,
			Timestamp: time.Now().UnixNano(),
		},
		OrderID:   "buy-1",
		OrderType: protocol.OrderTypeLimit,
		Side:      protocol.SideBuy,
		Price:     udecimal.MustFromInt64(50000, 0), // 50000
		Size:      udecimal.MustFromInt64(1, 0),     // 1.0
	}
	if err := engine.PlaceOrderAsync(ctx, buyReq); err != nil {
		fmt.Printf("Error placing buy order: %v\n", err)
	}

	// Allow some time for async processing
	time.Sleep(100 * time.Millisecond)

	// 7. Check Logs
	fmt.Printf("Total events: %d\n", publish.Count())
	logs := publish.Logs()
	for _, log := range logs {
		switch log.Type {
		case protocol.LogTypeMatch:
			fmt.Printf("[MATCH] TradeID: %d, Price: %s, Size: %s\n",
				log.TradeID, log.Price, log.MatchSize)
		case protocol.LogTypeOpen:
			fmt.Printf("[OPEN] OrderID: %s, Price: %s\n", log.OrderID, log.Price)
		}
	}
}
```

### 3. Depth Feed: `DepthDeltaBuilder` and `AggregatedBook` (Downstream Market Data)

Downstream read services (Market Data Services, WebSocket gateways, Redis snapshot updaters) keep a live, aggregated view of the depth. The book itself produces the changes; the view only applies them.

Key capabilities:
- **One version per call**: `OrderBook.SeqID()` is the depth version. One SDK call (`PlaceOrder`, `CancelOrder`, `AmendOrder`) that moves visible depth advances it by exactly one, and every depth log of that call carries the new value. A refusal, an admin command, a no-op amend or a shrink inside the hidden size does not advance it.
- **Producer (`DepthDeltaBuilder`)**: `Add` or `AddBatch` the logs of a call, then `Flush(book, &delta)` reads the touched levels from the book and fills a `protocol.DepthDelta` (`Seq`, `Asks` ascending, `Bids` descending; a size of zero means the level is gone), then `Reset`. Which levels a log moves comes from `CalculateDepthChange`, the single depth rule. Flush must run before the next call changes the book, because it reads sizes as they are then. Several calls may be added before one Flush; the delta then holds each level once at its final size and carries the last version.
- **Follower (`AggregatedBook.ApplyDelta`)**: a delta with `Seq` one past the follower's version sets each level and advances the version. A resend (`Seq` at or below the version) is skipped. A gap calls `OnRebuild` for a snapshot; the snapshot is applied only if it reaches the delta, and otherwise `ErrSequenceGap` is returned with the book unchanged. A malformed delta is refused with `ErrInvalidDepthDelta` before anything changes. A snapshot is a `protocol.DepthDelta` that lists every level, with `Seq` the version it was taken at; `ApplySnapshot` checks it as a whole, so a negative size or a price written twice on one side leaves the book as it was.
- **Prices are compared by value**: `100` and `100.00` are one level, on the book and in the delta. The book keys its price maps by a canonical form, so the second order written at the same price joins the first instead of resting on a level of its own.
- **Queries on the book**: `LevelSize(side, price)` is the visible size at one price, and `BestPrice(side)` the best price of a side. Both are read from the book, so a caller holds no second copy of depth.
- **Thread-Safe Queries**: `AggregatedBook` is protected by an internal `sync.RWMutex`; `GetDepth` and `Depth` may run beside `ApplyDelta`, which has one writer.

```go
package main

import (
	"fmt"

	"github.com/quagmt/udecimal"

	match "github.com/0x5487/matching-engine"
	"github.com/0x5487/matching-engine/protocol"
)

func main() {
	book := match.NewOrderBook("BTC-USDT")
	feed := match.NewDepthDeltaBuilder()
	view := match.NewAggregatedBook()

	// 1. Configure OnRebuild for a gap: a snapshot at the version the follower missed.
	view.OnRebuild = func() (*protocol.DepthDelta, error) {
		return &protocol.DepthDelta{
			Seq:  100,
			Asks: []protocol.DepthLevel{{Price: udecimal.MustParse("50100"), Size: udecimal.MustParse("2.5")}},
			Bids: []protocol.DepthLevel{{Price: udecimal.MustParse("50000"), Size: udecimal.MustParse("1.2")}},
		}, nil
	}

	// 2. After each call on the book, turn its logs into a delta and apply it.
	batch, err := book.PlaceOrder(&protocol.PlaceOrderRequest{ /* ... */ })
	if err != nil {
		panic(err)
	}
	feed.AddBatch(batch.Logs)

	var delta protocol.DepthDelta
	if feed.Flush(book, &delta) {
		if err := view.ApplyDelta(&delta); err != nil {
			panic(err)
		}
	}
	feed.Reset()

	// 3. Query top N depth levels (e.g. to write to Redis or push via WebSocket)
	depth := view.GetDepth(20)
	fmt.Printf("UpdateID: %d\n", depth.UpdateID)
	if len(depth.Asks) > 0 {
		fmt.Printf("Best Ask: %s (%s)\n", depth.Asks[0].Price, depth.Asks[0].Size)
	}
	if len(depth.Bids) > 0 {
		fmt.Printf("Best Bid: %s (%s)\n", depth.Bids[0].Price, depth.Bids[0].Size)
	}
}
```

### Wire Transport (MQ / Cross-Process)

`MarshalRequest` and `UnmarshalRequest` serialize typed requests to and from a compact binary format for use in message queues and cross-process communication.

```go
// Serialize a request for MQ publishing.
// MarshalRequest derives the wire CommandType from the concrete Go type,
// so cross-process dispatch is always correct.
req := &protocol.PlaceOrderRequest{
    BaseCommand: protocol.BaseCommand{
        CommandID: "sell-1-cmd",
        MarketID:  "BTC-USDT",
        UserID:    1001,
        Timestamp: time.Now().UnixNano(),
    },
    OrderID:   "sell-1",
    OrderType: protocol.OrderTypeLimit,
    Side:      protocol.SideSell,
    Price:     udecimal.MustFromInt64(50000, 0),
    Size:      udecimal.MustFromInt64(1, 0),
}

data, err := protocol.MarshalRequest(req)
// ... publish data to MQ ...

// On the consumer side:
decoded, err := protocol.UnmarshalRequest(data)
// decoded is typed as any; use GetRequestBase or a type switch to dispatch.
```

### Request Semantics

- **Strict Authoritative Timestamp**: Every state-changing request (`PlaceOrderRequest`, `CancelOrderRequest`, `AmendOrderRequest`, management commands) must carry an upstream-assigned non-zero logical `Timestamp` (Unix nanoseconds, e.g. from sequencer metadata). Requests with `Timestamp <= 0` are strictly rejected with `protocol.RejectReasonInvalidPayload`. The engine never calls `time.Now()` internally.
- **Synchronous vs. Asynchronous**:
  - **Synchronous Mode (`book.PlaceOrder`, `book.CancelOrder`, `book.AmendOrder`)**: Immediately performs in-memory matching on the caller's goroutine and returns `(*LogBatch, error)`. The batch belongs to the book, so there is nothing to release: the batch and its logs stay valid until the next call on the same book, which reclaims them when it starts. Consume them before that call, and do not retain any `*OrderBookLog` or the `Logs` slice across it. Different books have different batches, so calls on several books may interleave.
  - **Asynchronous Mode (`engine.PlaceOrderAsync`, etc.)**: Enqueues work into the Disruptor MPSC ring buffer. A returned `error` means enqueue failure, not business rejection.
- Every request must carry an upstream-assigned non-empty `CommandID`. Engine helpers reject empty command IDs before enqueue.
- Business-level failures are emitted as `OrderBookLog` entries with `Type == protocol.LogTypeReject`.
- Requests sent to a missing market generate a reject event with `RejectReasonMarketNotFound`.
- Unknown query types will return `ErrUnknownQuery` through the `Future.Wait()` call.
- The `Query()` method uses a `*protocol.Query` request and returns `ErrNotFound` immediately when the market does not exist.

### Querying Market State

Query the engine for read-only state such as order book depth or statistics:

```go
// 1. Query Market Statistics
statsQuery := &protocol.Query{
	Type:     protocol.QueryGetStats,
	MarketID: "BTC-USDT",
}
future, err := engine.Query(ctx, statsQuery)
res, err := future.Wait(ctx)
if err == nil {
	stats := res.(*protocol.GetStatsResponse)
	fmt.Printf("Bids: %d, Asks: %d\n", stats.BidOrderCount, stats.AskOrderCount)
}

// 2. Query Order Book Depth
depthQuery := &protocol.Query{
	Type:     protocol.QueryGetDepth,
	MarketID: "BTC-USDT",
	Payload:  &protocol.GetDepthRequest{Limit: 10},
}
future, err = engine.Query(ctx, depthQuery)
res, err = future.Wait(ctx)
if err == nil {
	depth := res.(*protocol.GetDepthResponse)
	if len(depth.Bids) > 0 {
		fmt.Printf("Top Bid: %s\n", depth.Bids[0].Price)
	}
}
```

### Market Lifecycle

There is none, by design. Market creation, suspension and configuration are not
this library's business: an order book matched orders that were already routed
to it, and the set of tradeable markets is decided upstream. The top-level
`MatchingEngine` therefore has one way to obtain a book - restoring it from a
snapshot - and the synchronous `OrderBook` API is the entry point for callers
that manage books themselves.

### Supported Order Types

| Type | Description |
|------|-------------|
| `Limit` | Buy/sell at a specific price or better |
| `Market` | Execute immediately at best available price. With `Size` no `StepSize` is needed; with `QuoteSize` it is required — see [Step Size](#step-size). |
| `IOC` | Fill immediately, cancel unfilled portion. |
| `FOK` | Fill entirely immediately or cancel completely. |
| `PostOnly` | Add to book as maker only, reject if would match immediately. |

### Step Size

`StepSize` is the unit of account for the base asset. A base balance has to be a
whole number of steps or it cannot be reconciled against a ledger, which makes it
more than an order-size rule: it is what the balance is denominated in.

It travels on `PlaceOrderRequest` rather than living on the book. That means a
replay cannot disagree with itself about which unit was in force, and no producer
can pick a unit the market does not use — the caller that fills it in is the one
that knows the market's rules.

**It is required when, and only when, `QuoteSize` is used.** A quote-sized order
divides the quote by each level's price and floors the result; without a step there
is nothing to floor to and no unit to hold the balance in. Such an order is
rejected as `invalid_payload` rather than defaulted, because a default unit would
silently write balances the ledger cannot represent. An order with `Size` set
carries no step: the size was already chosen, and flooring it would change
nothing.

```go
// Buy as much as 100 USDT buys. The step size floors each level's conversion so
// the resulting balance is a whole number of steps; anything left over stays as
// quote and is reported back to the caller through the reject log.
req := &protocol.PlaceOrderRequest{
	OrderType: protocol.OrderTypeMarket,
	Side:      protocol.SideBuy,
	QuoteSize: udecimal.MustFromInt64(100, 0), // 100 USDT
	StepSize:  udecimal.MustFromInt64(1, 4),   // 0.0001
}
```

The floor happens **before** the minimum with the resting order, not after. That
ordering is what keeps an offer placed under an older, finer step reachable:
flooring after sends `0.0003` to zero at a `0.001` step, rejects an order the book
could have filled, and lets one holder's dust refuse every order behind it.

### Event Handling

Implement `Publisher` interface to handle order book events.

`Publish` **must not** retain anything it is given. The logs stay valid until the next call on the book that produced them (or the engine's next event), and that call reclaims them: the logs are zeroed and returned to a `sync.Pool`. A log that is read after that point reads recycled memory — usually zeros. `Publish` receives only `[]*OrderBookLog`, not the `*LogBatch`, so an implementation cannot reclaim the logs itself and never needs to.

If the logs are needed beyond the call — for a channel, a goroutine, or a queue — copy them before returning:

```go
type MyHandler struct {
	mu   sync.Mutex
	kept []match.OrderBookLog
}

func (h *MyHandler) Publish(logs []*match.OrderBookLog) {
	for _, log := range logs {
		copyLog := *log // copy before returning; the original is recycled on return
		h.mu.Lock()
		h.kept = append(h.kept, copyLog)
		h.mu.Unlock()
	}
}
```

A user event keeps the upstream message queue's command sequence in `log.CmdSeqID`; its `SeqID` is 0, since a user event has no depth version.

#### Log quantity fields

Each quantity field of `OrderBookLog` has one meaning. A log leaves the fields that do not apply at zero, so a field that is non-zero says what its log did:

| Field | Meaning | Set by |
| :--- | :--- | :--- |
| `MatchSize` | the base quantity traded; the visible size the maker gives up (a match never consumes an iceberg's hidden part) | `Match` |
| `Remaining` | the order's total remaining base size, visible plus hidden: the total after an open rests, the new total of an amend, the total a cancel removes, the base quantity a reject did not get | `Open`, `Amend`, `Cancel`, `Reject` (base-sized) |
| `OldRemaining` | the total before the amend | `Amend` |
| `VisibleAdded` | the visible size an open puts on the book (the visible part of `Remaining`) | `Open` |
| `VisibleRemoved` | the visible size a cancel or an amend takes off the book, at `OldPrice` | `Cancel`, `Amend` |
| `QuoteRemaining` | the quote a quote-sized market order did not spend | `Reject` (quote-sized) |

`Price`, `OldPrice`, `Amount` and the identifiers keep their usual meaning. `Open` and `Cancel` do not use `Amount`; `Amount` is the quote of a match only.

A publisher that only inspects the logs in place, as below, is also correct:

```go
func (h *MyHandler) Publish(logs []*match.OrderBookLog) {
	for _, log := range logs {
		if log.Type == protocol.LogTypeUser {
			fmt.Printf("User Event: %s, Data: %s\n", log.EventType, string(log.Data))
		} else if log.Type == protocol.LogTypeAdmin {
			fmt.Printf("Admin Event: %s | Market: %s\n", log.EventType, log.MarketID)
		} else {
			fmt.Printf("Event: %s | OrderID: %s\n", log.Type, log.OrderID)
		}
	}
}
```

### Generic User Events (Extension Protocol)

Inject custom events into the matching engine's log stream. 

```go
// Example: Sending an End-Of-Block signal
userEventReq := &protocol.UserEventRequest{
	BaseCommand: protocol.BaseCommand{
		CommandID: "block-100-event",
		UserID:    999,
		Timestamp: time.Now().UnixNano(),
	},
	EventType: "EndOfBlock",
	Key:       "block-100",
	Data:      []byte("0x123abc..."),
}
err := engine.SendUserEvent(ctx, userEventReq)
```

### Snapshot and Restore

#### 1. Synchronous OrderBook (In-Memory Struct)
For custom storage layers, Raft state machines, or embedding into external snapshots:

```go
// 1. Capture in-memory snapshot
snapshot := book.Snapshot()

// 2. Restore state into a new OrderBook instance
restoredBook := match.NewOrderBook("BTC-USDT")
restoredBook.Restore(snapshot)
```

#### 2. MatchingEngine (Disk-based Multi-Market Snapshot)
Persist full multi-market engine state to disk and restore after restarts:

```go
// Take disk snapshot with optional authoritative logical timestamp
meta, err := engine.TakeSnapshot("./snapshot", seqTimestamp)
if err != nil {
	panic(err)
}

// Restore entire engine from disk snapshot
restored := match.NewMatchingEngine("engine-1-restored", publish)
meta, err = restored.RestoreFromSnapshot("./snapshot")
if err != nil {
	panic(err)
}
_ = meta // contains GlobalLastCmdSeqID for replay positioning
```

## Benchmark

Run engine benchmarks:
```bash
make bench
```

Run aggregated book read-replica benchmarks:
```bash
make bench-aggrbook
```

Please refer to [docs](./docs/benchmark.md) for detailed benchmarks.
