# AGENTS.md

This file provides guidance to agents when working with code in this repository.


## Project rules

- Use English to write code and comments.
- Add comments for each function and struct to help developers understand their purpose.
- Use the `slog` package for all logging purposes.
- Always use the `-race` flag when running Go tests (e.g., `go test -race ./...`) to detect and prevent potential race conditions.
- Run `make check` after development to verify code quality and fix any errors found.

