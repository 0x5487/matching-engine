# Benchmark Report v0.5.1

```go
go test -v -run=none -benchmem -bench . -count 1
goos: linux
goarch: amd64
pkg: github.com/0x5487/matching-engine
cpu: AMD Ryzen 7 PRO 4750U with Radeon Graphics
BenchmarkPlaceOrders
BenchmarkPlaceOrders/goroutines-32000
BenchmarkPlaceOrders/goroutines-32000-16                  636009              2651 ns/op             754 B/op         16 allocs/op
    engine_bench_test.go:53: order count: 636230
    engine_bench_test.go:54: depth count: 1000
    engine_bench_test.go:55: error count: 0
BenchmarkPlaceOrders/goroutines-35200
BenchmarkPlaceOrders/goroutines-35200-16                  671277              2650 ns/op             715 B/op         16 allocs/op
    engine_bench_test.go:53: order count: 671641
    engine_bench_test.go:54: depth count: 1000
    engine_bench_test.go:55: error count: 0
BenchmarkPlaceOrders/goroutines-38400
BenchmarkPlaceOrders/goroutines-38400-16                  546318              2765 ns/op             728 B/op         16 allocs/op
    engine_bench_test.go:53: order count: 546598
    engine_bench_test.go:54: depth count: 1000
    engine_bench_test.go:55: error count: 0
BenchmarkPlaceOrders/goroutines-41600
BenchmarkPlaceOrders/goroutines-41600-16                  476806              3108 ns/op             747 B/op         16 allocs/op
    engine_bench_test.go:53: order count: 473422
    engine_bench_test.go:54: depth count: 1000
    engine_bench_test.go:55: error count: 0
BenchmarkPlaceOrders/goroutines-44800
BenchmarkPlaceOrders/goroutines-44800-16                  434683              2619 ns/op             761 B/op         16 allocs/op
    engine_bench_test.go:53: order count: 431439
    engine_bench_test.go:54: depth count: 1000
    engine_bench_test.go:55: error count: 0
PASS
ok      github.com/0x5487/matching-engine       12.335s
```
