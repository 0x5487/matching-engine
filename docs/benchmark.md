# Benchmark Report v0.8.0

## System Information
- **OS**: Linux (amd64)
- **CPU**: AMD Ryzen 7 PRO 4750U with Radeon Graphics
- **Go Version**: go1.25.4

## Benchmark Results

```text
go test -v -run=none -benchmem -bench . -count 1
goos: linux
goarch: amd64
pkg: github.com/0x5487/matching-engine
cpu: AMD Ryzen 7 PRO 4750U with Radeon Graphics
BenchmarkPlaceOrders

Final Order Book State: Bids=500 levels, Asks=499 levels
BenchmarkPlaceOrders-16          5842400               385.3 ns/op         2595481 orders/sec         50 B/op          2 allocs/op
PASS
ok      github.com/0x5487/matching-engine       2.983s
```

## Performance Highlights

### 1. Throughput
- **Orders Per Second**: ~2,595,000 ops/sec
- **Latency**: ~385 ns per order

### 2. Memory Efficiency
- **Allocations**: 2 allocs/op
- **Memory Usage**: 50 B/op


