# Matching Engine API Refactoring Design

## Objective
To simplify the `MatchingEngine` API by unifying the entry points, strictly adhering to the CQRS (Command Query Responsibility Segregation) principle, and hiding low-level implementation details. This ensures the public API is clean, intuitive, and prevents misuse.

## Design

### 1. Unified Query Interface
Currently, query methods like `GetStats` and `Depth` are implemented as separate methods on the engine. To make the API more extensible and uniform, we will unify them under a single `Query` method.

*   **Add method:** `func (engine *MatchingEngine) Query(ctx context.Context, req any) (*Future[any], error)`
*   **Remove methods:** `GetStats` and `Depth`.
*   **Implementation:** The `Query` method will wrap the request in an `InputEvent` by setting the `Query` field (leaving `Cmd` nil) and enqueueing it.
*   **Performance:** By using `any` (interface{}), we avoid serializing queries into byte arrays, maintaining zero-allocation for high-frequency reads.

### 2. Hiding Internal Methods
Several methods currently exposed on `MatchingEngine` are implementation details of the RingBuffer integration or low-level enqueueing mechanisms.

*   **`OnEvent` -> `onEvent`**: This is the callback for the Disruptor pattern. It will be made private.
*   **`EnqueueCommand` -> `enqueueCommand`**: External users should use `SubmitAsync` for asynchronous, fire-and-forget commands.
*   **`EnqueueCommandBatch` -> `enqueueCommandBatch`**: Made private to hide low-level batching.
*   **Note on `PlaceOrderBatch`**: This method will remain public for now but its implementation will be updated to call the newly privatized `enqueueCommandBatch`.

### 3. Impact Analysis

*   **Clients:** Callers requesting stats or depth will migrate from `engine.GetStats(ctx, id)` to `engine.Query(ctx, &protocol.GetStatsRequest{MarketID: id})`.
*   **Tests:**
    *   Update all usages of `GetStats` and `Depth` to use `Query`.
    *   Update tests using `EnqueueCommand` to use `SubmitAsync` where appropriate, or `enqueueCommand` for specific low-level boundary tests within the same package.
*   **Documentation:** Update the `README.md` to reflect that queries are handled via the unified `Query()` method instead of specialized methods.

## Execution Plan
1.  **Refactor `engine.go`**:
    *   Add `Query` method.
    *   Remove `GetStats` and `Depth` methods.
    *   Rename `OnEvent`, `EnqueueCommand`, and `EnqueueCommandBatch` to start with a lowercase letter.
    *   Update `PlaceOrderBatch` and `SubmitAsync` to use the new private enqueue methods.
2.  **Update Tests**: Refactor all occurrences of the changed/removed methods across test files.
3.  **Update `README.md`**: Adjust the "Command Semantics" section.