# Matching Engine SDK

A high-performance, in-memory order matching engine written in Go. Designed for crypto exchanges, trading simulations, and financial systems requiring precise and fast order execution.

## 🚀 Features

- **High Performance**: Pure in-memory matching using efficient SkipList data structures ($O(\log N)$) and **Disruptor** pattern (RingBuffer) for **microsecond latency**.
- **Single Thread Actor**: Adopts a **Lock-Free** architecture where a single pinned goroutine processes all state mutations. This eliminates context switching and mutex contention, maximizing CPU cache locality.
- **Concurrency Safe**: All state mutations are serialized through the RingBuffer, eliminating race conditions without heavy lock contention.
- **Low Allocation Hot Paths**: Uses `udecimal` (uint64-based), intrusive lists, and object pooling to minimize GC pressure on performance-critical paths.
- **Multi-Market Support**: Manages multiple trading pairs (e.g., BTC-USDT, ETH-USDT) within a single `MatchingEngine` instance.
- **Management Commands**: Dynamic market management (Create, Suspend, Resume, UpdateConfig) via Event Sourcing.
- **Comprehensive Order Types**:
  - `Limit`, `Market` (Size or QuoteSize), `IOC`, `FOK`, `Post Only`
  - **Iceberg Orders**: Support for hidden size with automatic replenishment.
- **Event Sourcing**: Generates detailed `OrderBookLog` events allows for deterministic replay and state reconstruction.

## 📦 Installation

```bash
go get github.com/0x5487/matching-engine
```

## 🛠 Usage

### Quick Start

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

	// 4. Create a Market
	// Management commands return a Future for synchronous-like waiting.
	future, err := engine.CreateMarket(ctx, "create-btc-usdt", 9001, "BTC-USDT", "0.00000001", time.Now().UnixNano())
	if err != nil {
		panic(err)
	}

	// Wait until the market is visible on the read path before submitting orders.
	if _, err := future.Wait(ctx); err != nil {
		panic(err)
	}

	// 5. Place a Sell Limit Order
	sellCmd := &protocol.PlaceOrderCommand{
		CommandID: "sell-1-cmd",
		OrderID:   "sell-1",
		OrderType: protocol.OrderTypeLimit,
		Side:      protocol.SideSell,
		Price:     udecimal.MustFromInt64(50000, 0).String(), // 50000
		Size:      udecimal.MustFromInt64(1, 0).String(),     // 1.0
		UserID:    1001,
		Timestamp: time.Now().UnixNano(),
	}
	if err := engine.PlaceOrder(ctx, "BTC-USDT", sellCmd); err != nil {
		fmt.Printf("Error placing sell order: %v\n", err)
	}

	// 6. Place a Buy Limit Order (Matches immediately)
	buyCmd := &protocol.PlaceOrderCommand{
		CommandID: "buy-1-cmd",
		OrderID:   "buy-1",
		OrderType: protocol.OrderTypeLimit,
		Side:      protocol.SideBuy,
		Price:     udecimal.MustFromInt64(50000, 0).String(), // 50000
		Size:      udecimal.MustFromInt64(1, 0).String(),     // 1.0
		UserID:    1002,
		Timestamp: time.Now().UnixNano(),
	}
	if err := engine.PlaceOrder(ctx, "BTC-USDT", buyCmd); err != nil {
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

### Command Semantics

- `PlaceOrder`, `CancelOrder`, `AmendOrder`, and management commands enqueue work into the engine event loop. A returned `error` means enqueue/serialization failure, not business rejection.
- Every command must carry an upstream-assigned non-empty `CommandID`. Engine helpers reject empty command IDs before enqueue.
- Every state-changing command must carry an upstream-assigned logical `Timestamp`. `Timestamp <= 0` is rejected as `invalid_payload`. For engine helper methods such as `CreateMarket`, `SuspendMarket`, `ResumeMarket`, `UpdateConfig`, and `SendUserEvent`, pass the timestamp explicitly from your Gateway / Sequencer / OMS.
- Business-level failures are emitted as `OrderBookLog` entries with `Type == protocol.LogTypeReject`.
- Commands sent to a missing market generate a reject event with `RejectReasonMarketNotFound`.
- `GetStats()` and `Depth()` return `ErrNotFound` immediately when the market does not exist.

### Management Commands

The engine supports dynamic market management:

```go
// Suspend a market (rejects new Place/Amend orders)
future, err := engine.SuspendMarket(ctx, "suspend-btc-usdt", 9001, "BTC-USDT", time.Now().UnixNano())
_, err = future.Wait(ctx)

// Resume a market
future, err = engine.ResumeMarket(ctx, "resume-btc-usdt", 9001, "BTC-USDT", time.Now().UnixNano())
_, err = future.Wait(ctx)

// Update market configuration (e.g. MinLotSize)
newLotSize := "0.01"
future, err = engine.UpdateConfig(ctx, "update-btc-usdt-lot", 9001, "BTC-USDT", newLotSize, time.Now().UnixNano())
_, err = future.Wait(ctx)
```

Successful management commands are emitted as `LogTypeAdmin`. Invalid management commands are reported through the same event stream as trading rejects. For example:

- duplicate market creation emits `RejectReasonMarketAlreadyExists`
- invalid `MinLotSize` emits `RejectReasonInvalidPayload`
- management reject logs preserve the operator `UserID`

### Supported Order Types

| Type | Description |
|------|-------------|
| `Limit` | Buy/sell at a specific price or better |
| `Market` | Execute immediately at best available price. Supports `Size` (base currency) or `QuoteSize` (quote currency). |
| `IOC` | Fill immediately, cancel unfilled portion. |
| `FOK` | Fill entirely immediately or cancel completely. |
| `PostOnly` | Add to book as maker only, reject if would match immediately. |

### Event Handling

Implement `Publisher` interface to handle order book events:

```go
type MyHandler struct{}

func (h *MyHandler) Publish(logs []*match.OrderBookLog) {
	for _, log := range logs {
		// If you need local ingest / publish time, add it here instead of relying on engine-generated fields.
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

Inject custom events into the matching engine's log stream. These events are processed sequentially with trades, ensuring deterministic ordering for valid use cases like **L1 Block Boundaries**, **Audit Checkpoints**, or **Oracle Updates**.

```go
// Example: Sending an End-Of-Block signal from an L1 Blockchain
blockHash := []byte("0x123abc...")
// SendUserEvent(commandID, userID, eventType, key, data, timestamp)
err := engine.SendUserEvent("block-100-event", 999, "EndOfBlock", "block-100", blockHash, time.Now().UnixNano())
```

The event will appear in the `PublishLog` stream as `LogTypeUser` with your custom data payload. Malformed user-event payloads are emitted as `LogTypeReject` with `RejectReasonInvalidPayload`.

### Snapshot and Restore

Use snapshots to persist engine state and restore it after restart:

```go
meta, err := engine.TakeSnapshot("./snapshot")
if err != nil {
	panic(err)
}

restored := match.NewMatchingEngine("engine-1-restored", publish)
meta, err = restored.RestoreFromSnapshot("./snapshot")
if err != nil {
	panic(err)
}
_ = meta // contains GlobalLastCmdSeqID for replay positioning
```

## Benchmark

Please refer to [docs](./docs/benchmark.md) for detailed benchmarks.
