# matching-engine

a matching-engine for crypto exchange

## Feature

1. order type (`market`, `limit`, `ioc`, `post_only`, `fok`)
1. engine supports multiple markets
1. high-speed. (all in memory)
1. order book update events

## Get Started

```Go
 suite.engine = NewMatchingEngine()

 // market1
 market1 := "BTC-USDT"
 order1 := Order{
  ID:       "order1",
  MarketID: market1,
  Type:     Limit,
  Side:     Buy,
  Price:    decimal.NewFromInt(100),
  Size:     decimal.NewFromInt(2),
 }

 _, err := suite.engine.PlaceOrder(&order1)
```

## Benchmark

Please refer to [doc](./doc/benchmark/v0.5.0.md)
