# Design Specification: Async Request Synchronization (Future Pattern)

## 1. Background & Objective
Management requests in the `MatchingEngine` (e.g., `CreateMarket`) are executed asynchronously on the engine event loop. Without a completion handle, developers would need to manually poll the state (e.g., via `QueryGetStats`) to confirm when a resource is ready, leading to a suboptimal developer experience.

This design introduces the **Future Pattern** to provide a synchronous-like experience for waiting on request execution results while preserving the high-performance asynchronous nature of the underlying Actor model.

## 2. Core Architecture

### 2.1 Generic Future[T]
A generic structure `Future[T]` is introduced to represent a placeholder for an operation that hasn't completed yet.

```go
type Future[T any] struct {
    engine   *MatchingEngine
    respChan chan any
    err      error // Error during the submission phase
}
```

- **Wait(ctx context.Context) (T, error)**: 
    - Blocks until the engine returns a result or the context times out.
    - Automatically handles the return of resources to the `responsePool`.
    - Distinguishes between "timeout/context errors" and "business execution errors."

### 2.2 Error Classification
- **Submission Error (Enqueue Error)**: If the RingBuffer is full or the engine is shut down, `CreateMarket` returns an error immediately, and the `Future` object is `nil`.
- **Execution Error (Business Error)**: The request entered the queue successfully but failed during execution (e.g., `MarketAlreadyExists`). This type of error is returned via `future.Wait()`.

## 3. Implementation Details

### 3.1 Resource Management (Channel Pooling)
To maintain high performance and minimize GC pressure, the `Future` will reuse the existing `responsePool` in `MatchingEngine`:
1. `CreateMarket` acquires a channel from the pool.
2. The channel travels through the RingBuffer within the `InputEvent`.
3. `Future.Wait()` is responsible for draining and returning the channel to the pool after completion.

### 3.2 Internal Engine Response Path
Modify `MatchingEngine.processCommand` and specific handlers (e.g., `handleCreateMarket`):
- Carry the `Resp` channel in the `InputEvent`.
- Upon successful execution, send the result value (e.g., `true` or a metadata object).
- Upon execution failure, send the corresponding `error` object.

## 4. Usage Example (Expected README Improvement)

```go
// 1. Submit the create-market request
future, err := engine.CreateMarket(ctx, &protocol.CreateMarketRequest{
    BaseCommand: protocol.BaseCommand{
        CommandID: "cmd-1",
        UserID:    1001,
        MarketID:  "BTC-USDT",
        Timestamp: time.Now().UnixNano(),
    },
    MinLotSize: udecimal.MustFromInt64(1, 2), // 0.01
})
if err != nil {
    panic(err) // Submission failed (e.g., queue full)
}

// 2. Wait for the execution result (synchronous-like experience)
if _, err := future.Wait(ctx); err != nil {
    panic(err) // Execution failed (e.g., market already exists)
}

// At this point, the market is guaranteed to be visible on the read path.
```

## 5. Extensibility
While the initial focus is on `CreateMarket`, the generic `Future[T]` design can easily be extended to other management commands:
- `SuspendMarket` -> `Future[bool]`
- `ResumeMarket` -> `Future[bool]`
- `UpdateConfig` -> `Future[bool]`

## 6. Testing Strategy
- **Unit Tests**: Validate `Wait()` behavior under normal execution, timeout, and context cancellation.
- **Stress Tests**: Ensure the `responsePool` resource recovery is correct under high-concurrency submission without leaks or contention.
- **Integration Tests**: Simulate duplicate market creation and verify that `future.Wait()` correctly captures and returns the business error.
