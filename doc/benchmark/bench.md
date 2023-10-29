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

```
go test -v -run=none -benchmem -bench . -count 1
goos: linux
goarch: amd64
pkg: github.com/0x5487/matching-engine
cpu: AMD Ryzen 7 PRO 4750U with Radeon Graphics
BenchmarkPlaceOrders
BenchmarkPlaceOrders/goroutines-32000
BenchmarkPlaceOrders/goroutines-32000-16                 2828812              2324 ns/op             474 B/op         12 allocs/op
    engine_bench_test.go:56: order count: 2540067
    engine_bench_test.go:57: depth count: 1000
    engine_bench_test.go:58: error count: 0
BenchmarkPlaceOrders/goroutines-35200
BenchmarkPlaceOrders/goroutines-35200-16                 2971916              2654 ns/op             477 B/op         12 allocs/op
    engine_bench_test.go:56: order count: 2687213
    engine_bench_test.go:57: depth count: 1000
    engine_bench_test.go:58: error count: 0
BenchmarkPlaceOrders/goroutines-38400
BenchmarkPlaceOrders/goroutines-38400-16                 3699148              2586 ns/op             485 B/op         11 allocs/op
    engine_bench_test.go:56: order count: 3329420
    engine_bench_test.go:57: depth count: 1000
    engine_bench_test.go:58: error count: 0
BenchmarkPlaceOrders/goroutines-41600
BenchmarkPlaceOrders/goroutines-41600-16                 3220308              2153 ns/op             412 B/op         10 allocs/op
    engine_bench_test.go:56: order count: 2684616
    engine_bench_test.go:57: depth count: 1000
    engine_bench_test.go:58: error count: 0
BenchmarkPlaceOrders/goroutines-44800
BenchmarkPlaceOrders/goroutines-44800-16                 2375593              1879 ns/op             384 B/op         10 allocs/op
    engine_bench_test.go:56: order count: 1458535
    engine_bench_test.go:57: depth count: 1000
    engine_bench_test.go:58: error count: 0
PASS
ok      github.com/0x5487/matching-engine       99.032s
```
