# Matching Engine SDK

A high-performance, in-memory order matching engine written in Go. Designed for crypto exchanges, trading simulations, and financial systems requiring precise and fast order execution.

## 🚀 Features

- **High Performance**: Pure in-memory matching using efficient SkipList data structures ($O(\log N)$) and **Disruptor** pattern (RingBuffer) for **microsecond latency**.
- **Concurrency Safe**: Built on the Actor model to ensure thread safety without heavy lock contention.
- **Zero-Allocation**: Uses `udecimal` (uint64-based) and extensive object pooling to minimize GC pressure on hot paths.
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
	engine := match.NewMatchingEngine(publish)

	// 3. Create a Market (Event Sourcing Command)
	// MarketID: "BTC-USDT", MinLotSize: "0.00000001"
	if err := engine.CreateMarket("BTC-USDT", "0.00000001"); err != nil {
		panic(err)
	}

	// 4. Place a Sell Limit Order
	sellCmd := &protocol.PlaceOrderCommand{
		OrderID:   "sell-1",
		OrderType: protocol.OrderTypeLimit,
		Side:      protocol.SideSell,
		Price:     udecimal.MustFromInt64(50000, 0).String(), // 50000
		Size:      udecimal.MustFromInt64(1, 0).String(),     // 1.0
		UserID:    1001,
	}
	if err := engine.PlaceOrder(ctx, "BTC-USDT", sellCmd); err != nil {
		fmt.Printf("Error placing sell order: %v\n", err)
	}

	// 5. Place a Buy Limit Order (Matches immediately)
	buyCmd := &protocol.PlaceOrderCommand{
		OrderID:   "buy-1",
		OrderType: protocol.OrderTypeLimit,
		Side:      protocol.SideBuy,
		Price:     udecimal.MustFromInt64(50000, 0).String(), // 50000
		Size:      udecimal.MustFromInt64(1, 0).String(),     // 1.0
		UserID:    1002,
	}
	if err := engine.PlaceOrder(ctx, "BTC-USDT", buyCmd); err != nil {
		fmt.Printf("Error placing buy order: %v\n", err)
	}

	// Allow some time for async processing
	time.Sleep(100 * time.Millisecond)

	// 6. Check Logs
	fmt.Printf("Total events: %d\n", publish.Count())
	for _, log := range publish.Logs() {
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

### Management Commands

The engine supports dynamic market management:

```go
// Suspend a market (rejects new Place/Amend orders)
engine.SuspendMarket("BTC-USDT")

// Resume a market
engine.ResumeMarket("BTC-USDT")

// Update market configuration (e.g. MinLotSize)
newLotSize := "0.01"
engine.UpdateConfig("BTC-USDT", &newLotSize)
```

### Supported Order Types

| Type | Description |
|------|-------------|
| `Limit` | Buy/sell at a specific price or better |
| `Market` | Execute immediately at best available price. Supports `Size` (base currency) or `QuoteSize` (quote currency). |
| `IOC` | Fill immediately, cancel unfilled portion. |
| `FOK` | Fill entirely immediately or cancel completely. |
| `PostOnly` | Add to book as maker only, reject if would match immediately. |

### Event Handling

Implement `PublishLog` interface to handle order book events:

```go
type MyHandler struct{}

func (h *MyHandler) Publish(logs []*match.OrderBookLog) {
	for _, log := range logs {
		// Send to WebSocket, save to DB, publish to MQ, etc.
		fmt.Printf("Event: %s | OrderID: %s\n", log.Type, log.OrderID)
	}
}
```

## Benchmark

Please refer to [doc](./doc/benchmark.md) for detailed benchmarks.
