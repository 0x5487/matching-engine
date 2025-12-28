# Matching Engine SDK

A high-performance, in-memory order matching engine written in Go. Designed for crypto exchanges, trading simulations, and financial systems requiring precise and fast order execution.

## 🚀 Features

- **High Performance**: Pure in-memory matching using efficient SkipList data structures ($O(\log N)$) for order queues.
- **Concurrency Safe**: Built on the Actor model using Go channels to ensure thread safety without heavy lock contention.
- **Precision**: Uses `shopspring/decimal` to handle prices and quantities, avoiding floating-point arithmetic errors.
- **Multiple Markets**: Supports managing multiple trading pairs (e.g., BTC-USDT, ETH-USDT) within a single engine instance.
- **Comprehensive Order Types**:
  - `Limit`: Buy or sell at a specific price or better.
  - `Market`: Buy or sell immediately at the best available price.
  - `IOC` (Immediate or Cancel): Try to fill immediately, cancel any unfilled portion.
  - `FOK` (Fill or Kill): Fill the entire order immediately or cancel it entirely.
  - `Post Only`: Ensure the order is added to the book as a Maker; cancels if it would cross the spread.
- **Order Management**:
  - **Cancel**: Remove an active order.
  - **Amend**: Modify the price or quantity of an existing order. Handles priority logic (priority kept for size decrease, lost for price change/size increase).
- **Event Sourcing Ready**: Generates detailed `BookLog` events (`Open`, `Match`, `Cancel`, `Amend`, `Reject`) allowing for full order book state reconstruction and downstream integration (e.g., WebSocket feeds, persistence).
- **Zero-Allocation Logging**: Utilizes `sync.Pool` for log events to minimize GC pressure.

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
	"github.com/shopspring/decimal"
)

func main() {
	ctx := context.Background()

	// 1. Create a publish trader to handle order book events
	publishTrader := match.NewMemoryPublishTrader()

	// 2. Create and start the order book
	orderBook := match.NewOrderBook(publishTrader)
	go orderBook.Start()

	// 3. Add a limit buy order
	buyOrder := &match.Order{
		ID:       "buy-1",
		MarketID: "BTC-USDT",
		Type:     match.Limit,
		Side:     match.Buy,
		Price:    decimal.NewFromInt(50000),
		Size:     decimal.NewFromInt(1),
		UserID:   1001,
	}
	_ = orderBook.AddOrder(ctx, buyOrder)

	// 4. Add a limit sell order (this will match with the buy order)
	sellOrder := &match.Order{
		ID:       "sell-1",
		MarketID: "BTC-USDT",
		Type:     match.Limit,
		Side:     match.Sell,
		Price:    decimal.NewFromInt(50000),
		Size:     decimal.NewFromInt(1),
		UserID:   1002,
	}
	_ = orderBook.AddOrder(ctx, sellOrder)

	time.Sleep(100 * time.Millisecond)

	// 5. Check the results
	fmt.Printf("Total events: %d\n", publishTrader.Count())
	for i := 0; i < publishTrader.Count(); i++ {
		log := publishTrader.Get(i)
		fmt.Printf("[%s] OrderID: %s, Price: %s, Size: %s\n",
			log.Type, log.OrderID, log.Price, log.Size)
	}

	// 6. Get current order book depth
	depth, _ := orderBook.Depth(10)
	fmt.Printf("Bids: %d, Asks: %d\n", len(depth.Bids), len(depth.Asks))
}
```

### Order Types

| Type | Description |
|------|-------------|
| `Limit` | Buy/sell at a specific price or better |
| `Market` | Execute immediately at best available price |
| `IOC` | Fill immediately, cancel unfilled portion |
| `FOK` | Fill entirely or cancel completely |
| `PostOnly` | Add to book as maker only, reject if would cross |

### Order Management

```go
// Cancel an order
orderBook.CancelOrder(ctx, "order-id")

// Amend an order (change price or size)
orderBook.AmendOrder(ctx, "order-id", newPrice, newSize)
```

### Custom Event Handler

Implement `PublishTrader` interface to handle events your way:

```go
type MyHandler struct{}

func (h *MyHandler) Publish(logs ...*match.BookLog) {
	for _, log := range logs {
		// Send to WebSocket, save to DB, publish to MQ, etc.
		fmt.Printf("Event: %s | OrderID: %s\n", log.Type, log.OrderID)
	}
}
```

## Benchmark

Please refer to [doc](./doc/benchmark.md)
