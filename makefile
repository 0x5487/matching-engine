.PHONY: test

benchmark_all_1s:
	go test -benchmem -bench . -benchtime=1s


benchmark_all_100w:
	go test -benchmem -bench . -benchtime=1000000x