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

### 1. Initialize the Engine

You need to implement the `PublishTrader` interface to handle the output logs (trades, order updates).

```go
package main

import (
 "context"
 "fmt"
 "time"

 match "github.com/0x5487/matching-engine"
 "github.com/shopspring/decimal"
)

// MyTradeHandler implements match.PublishTrader
type MyTradeHandler struct{}

func (h *MyTradeHandler) Publish(logs ...*match.BookLog) {
 for _, log := range logs {
  fmt.Printf("Event: %s | OrderID: %s | Price: %s | Size: %s\n", 
            log.Type, log.OrderID, log.Price, log.Size)
 }
}

func main() {
 publishTrader := NewMemoryPublishTrader() // save trade into memory, if you want to pulish the trade to MQ, you can implement the interface
 engine := NewMatchingEngine(publishTrader)

 // market1
 market1 := "BTC-USDT"
 order1 := &Order{
  ID:       "order1",
  MarketID: market1,
  Type:     Limit,
  Side:     Buy,
  Price:    decimal.NewFromInt(100),
  Size:     decimal.NewFromInt(2),
 }

 _, err := suite.engine.AddOrder(order1)
}
```

## Benchmark

Please refer to [doc](./doc/benchmark/bench.md)
