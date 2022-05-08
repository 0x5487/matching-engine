# matching-engine

a matching-engine for crypto exchange

## Feature

1. place limited order
1. place market order

## Benchmark

Device: Apple Mac Pro (M1)

1. How many orders per second?

```shell
$> go test -benchmem -bench . -benchtime=1s
goos: linux
goarch: arm64
pkg: github.com/0x5487/matching-engine
BenchmarkDepthAdd-8               601053              5960 ns/op             773 B/op         25 allocs/op
BenchmarkDepthRemove-8           1000000              1513 ns/op             215 B/op         15 allocs/op
BenchmarkSizeAdd-8               1649569               716.4 ns/op           350 B/op         14 allocs/op
BenchmarkSizeRemove-8            2739526               470.0 ns/op           128 B/op         10 allocs/op
```

2. How long does it take to finish 1 million orders?

```shell
$> go test -benchmem -bench . -benchtime=1000000x
goos: linux
goarch: arm64
pkg: github.com/0x5487/matching-engine
BenchmarkDepthAdd-8              1000000              7239 ns/op             814 B/op         25 allocs/op
BenchmarkDepthRemove-8           1000000              1652 ns/op             215 B/op         15 allocs/op
BenchmarkSizeAdd-8               1000000               746.4 ns/op           395 B/op         14 allocs/op
BenchmarkSizeRemove-8            1000000               466.4 ns/op           128 B/op         10 allocs/op
```
