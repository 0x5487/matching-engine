.PHONY: test

benchmark_all_1s:
	go test -benchmem -bench . -benchtime=1s


benchmark_all_10w:
	go test -benchmem -bench . -benchtime=100000x


lint:
	golangci-lint run ./... -v

test:
	go test -race -coverprofile=cover.out -covermode=atomic ./...