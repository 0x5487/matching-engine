# AGENTS.md

A high-performance, in-memory matching engine SDK written in Go. Designed for matching engine service

## Project structure

The repository is organized into a core engine and several supporting packages:

- **Root Package**: Contains the core high-performance matching logic, order book management, and event-driven orchestration.
- **`protocol/`**: Defines the public API for the matching engine. This package contains typed requests, responses, and common models intended for consumption by **external services**.
- **`structure/`**: Houses specialized, performance-tuned data structures used for internal state management.
- **`docs/`**: Supplemental documentation and design specifications.


## Project rules

- Use the `slog` package for all logging purposes.
- Run `make check` after development to verify code quality (includes both lint and tests) and fix any errors found.

