# Benchmark Report v0.5.1

```go
go test -v -run=none -benchmem -bench . -count 1
goos: windows
goarch: amd64
pkg: github.com/0x5487/matching-engine
cpu: AMD Ryzen 7 PRO 4750U with Radeon Graphics
BenchmarkPlaceOrders
BenchmarkPlaceOrders/goroutines-32000
BenchmarkPlaceOrders/goroutines-32000-16                 2867136              2038 ns/op             529 B/op         15 allocs/op
    engine_bench_test.go:57: order count: 2695827
    engine_bench_test.go:58: depth count: 1000
    engine_bench_test.go:59: error count: 0
BenchmarkPlaceOrders/goroutines-35200
BenchmarkPlaceOrders/goroutines-35200-16                 2908399              2167 ns/op             520 B/op         15 allocs/op
    engine_bench_test.go:57: order count: 2601177
    engine_bench_test.go:58: depth count: 1000
    engine_bench_test.go:59: error count: 0
BenchmarkPlaceOrders/goroutines-38400
BenchmarkPlaceOrders/goroutines-38400-16                 2754332              1480 ns/op             470 B/op         13 allocs/op
    engine_bench_test.go:57: order count: 2019581
    engine_bench_test.go:58: depth count: 1000
    engine_bench_test.go:59: error count: 0
BenchmarkPlaceOrders/goroutines-41600
BenchmarkPlaceOrders/goroutines-41600-16                 2737024              1504 ns/op             470 B/op         13 allocs/op
    engine_bench_test.go:57: order count: 2001215
    engine_bench_test.go:58: depth count: 1000
    engine_bench_test.go:59: error count: 0
BenchmarkPlaceOrders/goroutines-44800
BenchmarkPlaceOrders/goroutines-44800-16                 2855335              1734 ns/op             459 B/op         13 allocs/op
    engine_bench_test.go:57: order count: 2336669
    engine_bench_test.go:58: depth count: 1000
    engine_bench_test.go:59: error count: 0
PASS
ok      github.com/0x5487/matching-engine       63.573s
```

```
go test -v -run=none -benchmem -bench . -count 1
goos: linux
goarch: amd64
pkg: github.com/0x5487/matching-engine
cpu: AMD Ryzen 7 PRO 4750U with Radeon Graphics
BenchmarkPlaceOrders
BenchmarkPlaceOrders/goroutines-32000
BenchmarkPlaceOrders/goroutines-32000-16                 2073391              6176 ns/op             546 B/op         14 allocs/op
    engine_bench_test.go:57: order count: 1780942
    engine_bench_test.go:58: depth count: 1000
    engine_bench_test.go:59: error count: 0
BenchmarkPlaceOrders/goroutines-35200
BenchmarkPlaceOrders/goroutines-35200-16                 1964409              6982 ns/op             565 B/op         15 allocs/op
    engine_bench_test.go:57: order count: 1700222
    engine_bench_test.go:58: depth count: 1000
    engine_bench_test.go:59: error count: 0
BenchmarkPlaceOrders/goroutines-38400
BenchmarkPlaceOrders/goroutines-38400-16                 3453805              4978 ns/op             475 B/op         14 allocs/op
    engine_bench_test.go:57: order count: 2940436
    engine_bench_test.go:58: depth count: 1000
    engine_bench_test.go:59: error count: 0
BenchmarkPlaceOrders/goroutines-41600
BenchmarkPlaceOrders/goroutines-41600-16                 3279264              3717 ns/op             459 B/op         13 allocs/op
    engine_bench_test.go:57: order count: 2745648
    engine_bench_test.go:58: depth count: 1000
    engine_bench_test.go:59: error count: 0
BenchmarkPlaceOrders/goroutines-44800
BenchmarkPlaceOrders/goroutines-44800-16                 3134860              5829 ns/op             468 B/op         13 allocs/op
    engine_bench_test.go:57: order count: 2473900
    engine_bench_test.go:58: depth count: 1000
    engine_bench_test.go:59: error count: 0
PASS
ok      github.com/0x5487/matching-engine       120.966s
```
