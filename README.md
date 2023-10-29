# matching-engine

a matching-engine for crypto exchange

## Feature

1. order type (`market`, `limit`, `ioc`, `post_only`, `fok`)
1. engine supports multiple markets
1. high-speed. (all in memory)
1. query order book depth

## Get Started

```Go
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
```

## Benchmark

Please refer to [doc](./doc/benchmark/bench.md)
