.PHONY: test

bench:
	go test -v -run=none -benchmem -bench . -count 1

lint:
	golangci-lint run ./... -v

test:
	go test -race -coverprofile=cover.out -covermode=atomic ./...

release: lint test

prof-mem:
	go test -v -run=none -bench . -benchmem -memprofile mem.out ./...
	go tool pprof -alloc_objects -top mem.out

prof-cpu:
	go test -v -run=none -bench . -benchmem -cpuprofile cpu.out ./...
	go tool pprof -top cpu.out