.PHONY: test

bench:
	go test -v -run=none -benchmem -bench . -count 1

lint:
	go tool golangci-lint run ./... -v

test:
	go test -race -coverprofile=cover.out -covermode=atomic ./...

coverage: test
	go tool cover -func=cover.out

coverage-html: test
	go tool cover -html=cover.out -o coverage.html

check: lint test

prof-mem:
	go test -v -run=none -bench "BenchmarkPlaceOrders|BenchmarkMatching" -benchmem -memprofile mem.out .
	go tool pprof -alloc_objects -top mem.out

prof-cpu:
	go test -v -run=none -bench "BenchmarkPlaceOrders|BenchmarkMatching" -benchmem -cpuprofile cpu.out .
	go tool pprof -top cpu.out

bench-structure:
	go test -v -run=none -bench=. -benchmem -count 1 ./structure/...

fmt:
	go tool golangci-lint fmt

fix:
	go tool golangci-lint run --fix ./...