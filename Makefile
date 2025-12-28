.PHONY: test

bench:
	go test -v -run=none -benchmem -bench . -count 1

lint:
	golangci-lint run ./... -v

test:
	go test -race -coverprofile=cover.out -covermode=atomic ./...

release: lint test