# AGENTS.md

This file provides guidance to agents when working with code in this repository.


## Project rules

- Communicate in the same language as the user. If the user speaks English, respond in English; if the user speaks Chinese, respond in Chinese.
- Use English to write code and comments.
- Add comments for each function and struct to help developers understand their purpose.
- Use the `slog` package for all logging purposes.
- Prefer using the `any` keyword instead of `interface{}` for empty interfaces.
- Avoid using `fmt.Sprint` or `fmt.Sprintf` for simple string concatenation in performance-critical code; use efficient alternatives like direct concatenation or the `strconv` package.
- Always use the `-race` flag when running Go tests (e.g., `go test -race ./...`) to detect and prevent potential race conditions.
- Prioritize using `assert.Eventually` over `time.Sleep` in unit tests to ensure tests are deterministic and efficient.
- Run `make lint` after development to verify code quality and fix any errors found.

