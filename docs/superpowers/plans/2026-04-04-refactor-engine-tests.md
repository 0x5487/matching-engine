# Refactor Engine Tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish refactoring `engine_test.go` and `order_book_bench_test.go` to use `submitXxx` helpers and unified `protocol.Params` structs.

**Architecture:** Standardize all engine interactions through helper functions that wrap `Submit` and `SubmitAsync` calls, ensuring consistent command construction and payload handling.

**Tech Stack:** Go, Testify, udecimal

---

### Task 1: Fix `engine_test.go` helpers parameter order and unified API

**Files:**
- Modify: `engine_test.go`

- [ ] **Step 1: Fix `submitCreateMarket` and `submitPlaceOrder` parameter order**
Change `submitXxx(engine, ctx, params)` to `submitXxx(ctx, engine, params)` and ensure all calls use `protocol.Params` structs.

- [ ] **Step 2: Append `t.Name()` to market IDs in all subtests**
Ensure market IDs are unique to avoid `market_already_exists` errors.

- [ ] **Step 3: Replace any remaining direct engine calls with helpers**
Ensure no direct `engine.PlaceOrder` (if any) or `engine.CreateMarket` calls remain.

### Task 2: Refactor `order_book_bench_test.go`

**Files:**
- Modify: `order_book_bench_test.go`

- [ ] **Step 1: Standardize `submitCreateMarket` calls**
Ensure they use `(ctx, engine, params)`.

- [ ] **Step 2: Update `PlaceOrderParams` initialization**
Ensure all `PlaceOrderParams` are complete and use consistent styles.

### Task 3: Verification

- [ ] **Step 1: Compile and run tests**
Run: `go clean -testcache && go test -race ./...`
Expected: ALL tests pass.

- [ ] **Step 2: Fix any lingering issues**
If any tests fail due to market collision or incorrect parameters, fix them.

---
Plan complete and saved to `docs/superpowers/plans/2026-04-04-refactor-engine-tests.md`.
