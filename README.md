# Matching Engine SDK

A high-performance, in-memory matching engine SDK written in Go. Designed for crypto exchanges, trading simulations, and financial systems requiring precise and fast order execution.

## 🚀 Features

- **High Performance**: Pure in-memory matching using efficient SkipList data structures ($O(\log N)$) and **Disruptor** pattern (RingBuffer) for **microsecond latency**.
- **Deterministic Execution (RSM Ready)**: **100% free of `time.Now()`** in engine logic. All state transitions and event timestamps strictly derive from authoritative external logical clocks (e.g. sequencer timestamps), guaranteeing bit-for-bit deterministic replay across nodes in Replicated State Machines (RSM) and event sourcing architectures.
- **Dual Integration Modes**:
  - **Synchronous `OrderBook`**: Direct in-memory matching without background workers, ideal for embedding into single-goroutine state machines, Raft loops, or partitioned workers.
  - **Asynchronous `MatchingEngine`**: Complete multi-market actor with Disruptor MPSC ring buffer for high-concurrency standalone services.
- **Market Data Read-Replica (`AggregatedBook`)**: Downstream aggregated order book state machine supporting event replay (`Open`, `Match`, `Cancel`, `Amend`), sequence deduplication, automatic gap recovery (`OnRebuild`), and thread-safe depth queries (`GetDepth`) with over 7M events/sec replay throughput.
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
	defer sellBatch.Release() // Always release pooled LogBatch back to sync.Pool

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
	defer buyBatch.Release()

	// 4. Immediately inspect match results (e.g. for atomic balance settlement)
	for _, log := range buyBatch.Logs {
		if log.Type == protocol.LogTypeMatch {
			fmt.Printf("[MATCH] TradeID: %d, Price: %s, Size: %s, Maker: %s, Taker: %s\n",
				log.TradeID, log.Price, log.Size, log.MakerOrderID, log.OrderID)
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
				log.TradeID, log.Price, log.Size)
		case protocol.LogTypeOpen:
			fmt.Printf("[OPEN] OrderID: %s, Price: %s\n", log.OrderID, log.Price)
		}
	}
}
```

### 3. AggregatedBook (Downstream Market Data Read-Replica)

`AggregatedBook` is designed for downstream read services (such as Market Data Services, WebSocket broadcast gateways, or Redis snapshot updaters) that consume `OrderBookLog` events from a message queue (e.g. NATS JetStream, Kafka) to maintain a live, aggregated view of the order book depth.

Key capabilities:
- **Event Replay**: Accurately tracks price level depth by replaying `LogTypeOpen`, `LogTypeMatch`, `LogTypeCancel`, and `LogTypeAmend`.
- **Depth Version**: An order book's `SeqID` is its per-market depth version, advancing only when resting size is added or removed. Refusals and admin logs carry no version, so a consumer may forward only the four depth-changing log types above and still hand `Replay` a contiguous version — which is what keeps a version jump meaningful as a lost-update signal.
- **Deduplication & Gap Detection**: Guarantees deterministic state sync using `SequenceID`. Duplicates (`SeqID <= current`) are automatically skipped; sequence gaps (`SeqID > current + 1`) trigger the `OnRebuild` callback to reload a snapshot.
- **Thread-Safe Queries**: Protected by internal `sync.RWMutex`, allowing concurrent background event replay and foreground depth queries (`GetDepth`).
- **High Throughput**: Capable of replaying over 7,000,000 events/sec with **0 B/op and 0 allocs/op**.

```go
package main

import (
	"fmt"

	match "github.com/0x5487/matching-engine"
	"github.com/0x5487/matching-engine/protocol"
)

func main() {
	ab := match.NewAggregatedBook()

	// 1. Configure OnRebuild callback for automatic gap recovery
	ab.OnRebuild = func() (*match.Snapshot, error) {
		// Fetch latest snapshot from Engine API or Redis
		return &match.Snapshot{
			SequenceID: 100,
			Asks: []*protocol.DepthItem{
				{Price: "50100", Size: "2.5"},
			},
			Bids: []*protocol.DepthItem{
				{Price: "50000", Size: "1.2"},
			},
		}, nil
	}

	// 2. Replay incoming OrderBookLog events from MQ.
	// Forward only Open, Match, Cancel and Amend. Refusals and admin logs carry
	// no depth version and are dropped anyway; a user event carries the upstream
	// command sequence in SeqID and would read as a gap.
	if err := ab.Replay(logEvent); err != nil {
		panic(err)
	}

	// 3. Query top N depth levels (e.g. to write to Redis or push via WebSocket)
	depth := ab.GetDepth(20)
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
  - **Synchronous Mode (`book.PlaceOrder`, `book.CancelOrder`, `book.AmendOrder`)**: Immediately performs in-memory matching on the caller's goroutine and returns `(*LogBatch, error)`. Callers **MUST** call `batch.Release()` when finished. `Release()` returns both the batch and the logs it holds to their `sync.Pool`s, and it zeroes those logs, so they must be fully consumed before the call and no `*OrderBookLog` or `Logs` slice may be retained across it. This is what lets synchronous callers pool their logs without knowing the pool exists.
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

The step size is the one market rule that reaches this library, and it arrives on
each order rather than living on the book. That is what keeps a base balance
expressible as a whole number of steps: a quote-sized market order divides the
quote by each level's price and floors the result. Carrying it per order also
means a replay cannot disagree with itself about which unit was in force, and no
producer can pick a unit the market does not use.

```go
// A quote-sized market order needs a step size to convert against.
req := &protocol.PlaceOrderRequest{
	OrderType: protocol.OrderTypeMarket,
	Side:      protocol.SideBuy,
	QuoteSize: udecimal.MustFromInt64(100, 0), // 100 USDT
	StepSize:  udecimal.MustFromInt64(1, 4),   // 0.0001
}
```

### Supported Order Types

| Type | Description |
|------|-------------|
| `Limit` | Buy/sell at a specific price or better |
| `Market` | Execute immediately at best available price using either `Size` or `QuoteSize`. |
| `IOC` | Fill immediately, cancel unfilled portion. |
| `FOK` | Fill entirely immediately or cancel completely. |
| `PostOnly` | Add to book as maker only, reject if would match immediately. |

### Event Handling

Implement `Publisher` interface to handle order book events.

`Publish` **must not** retain anything it is given. The engine always calls `LogBatch.Release()` immediately after `Publish` returns, and `Release` zeroes the logs and returns them to a `sync.Pool`. A log that is read after that point reads recycled memory — usually zeros. Note that `Publish` receives only `[]*OrderBookLog`, not the `*LogBatch`, so an implementation cannot call `Release` itself; doing so is not a supported option and never necessary.

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
