# Design Specification: Management Interface Future Pattern Refactoring

## 1. Background & Objective
Currently, management-related interfaces in `MatchingEngine` (`SuspendMarket`, `ResumeMarket`, `UpdateConfig`) follow a "fire-and-forget" asynchronous model. This forces callers to be unaware of when a command truly takes effect and prevents them from capturing logical errors during execution (e.g., invalid parameters, incompatible market states).

This design aims to refactor these interfaces to use the **Future Pattern**, consistent with `CreateMarket`, to enhance the developer experience (DX) and ensure traceability of operations.

## 2. Core Changes

### 2.1 Engine Layer API Adjustments
Modify the method signatures of `MatchingEngine` to return `*Future[bool]`:

- `SuspendMarket(ctx context.Context, commandID string, userID uint64, marketID string, timestamp int64) (*Future[bool], error)`
- `ResumeMarket(ctx context.Context, commandID string, userID uint64, marketID string, timestamp int64) (*Future[bool], error)`
- `UpdateConfig(ctx context.Context, commandID string, userID uint64, marketID string, minLotSize string, timestamp int64) (*Future[bool], error)`

The internal implementation will switch from calling `EnqueueCommand` to using `enqueueCommandWithResponse`, acquiring response channels from the `responsePool`.

### 2.2 Event Flow Path Refactoring
To return execution results to the caller, the command object (`InputEvent`) must carry the `Resp` channel into the `OrderBook`:

1.  **`MatchingEngine.processCommand`**: Change the delegation to `OrderBook` to pass the entire `ev` (`*InputEvent`) instead of just `ev.Cmd`.
2.  **`OrderBook.processCommand`**: Change the signature to `func (book *OrderBook) processCommand(ev *InputEvent)`.

### 2.3 OrderBook Business Logic Reporting
Incorporate reporting mechanisms into specific handler functions:

- **`handleSuspendMarket`**: Send `true` upon successful execution; send an error if the market is already in the `Halted` state.
- **`handleResumeMarket`**: Send `true` upon successful execution.
- **`handleUpdateConfig`**: Send `true` after successful configuration updates (e.g., successful `MinLotSize` parsing); send `error` if parsing fails.

## 3. Helper Tools
Introduce private helper methods in `OrderBook` to reduce boilerplate:
- `respondSuccess(ev *InputEvent, val any)`: Safely send a result to `ev.Resp`.
- `respondError(ev *InputEvent, err error)`: Safely send an error to `ev.Resp`.

## 4. Compatibility & Impact
- **Breaking Change**: This is a breaking API change. All calls to these interfaces must be updated to wait for the Future or explicitly ignore the return value.
- **Performance Impact**: Introducing `Future` adds a negligible overhead for channel allocation and recycling, which is acceptable for low-frequency management operations.

## 5. Testing Strategy
- **Unit Tests**: Update relevant tests in `engine_test.go` to verify that `future.Wait()` correctly captures success and failure scenarios.
- **Integration Tests**: Simulate management operations under high concurrency to ensure response channels are correctly returned to the `sync.Pool`, preventing memory leaks.
