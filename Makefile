.PHONY: test

bench:
	go test -v -run=none -benchmem -bench . -count 1

bench_1s:
	go test -v -run=none -benchmem -bench . -benchtime=1s

bench_1w:
	go test -v -run=none -benchmem -bench . -benchtime=10000x


lint:
	golangci-lint run ./... -v

test:
	go test -race -coverprofile=cover.out -covermode=atomic ./...

release: lint test