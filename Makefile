.PHONY: test

benchmark_1s:
	go test -v -benchmem -bench . -benchtime=1s


benchmark_1w:
	go test -benchmem -bench . -benchtime=10000x


lint:
	golangci-lint run ./... -v

test:
	go test -race -coverprofile=cover.out -covermode=atomic ./...