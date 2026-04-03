# Design Specification: Query Future Pattern & UpdateConfig Fix

## 1. Background & Objective
This design addresses two critical improvements:
1.  **Query Future Pattern**: Refactor `Depth` and `GetStats` to use the `Future` pattern. This prevents potential response channel pollution (ABA problem) by leveraging the safe channel release logic in `Future.Wait` and provides a more consistent, context-aware API.
2.  **UpdateConfig Response Fix**: Ensure that `UpdateConfig` (and other management commands) always resolve their `Future` even when payload deserialization fails, preventing the caller from hanging.

## 2. Core Changes

### 2.1 Query Methods Refactoring
Modify the signatures of `GetStats` and `Depth` in `MatchingEngine` to return `*Future[T]`:

- `GetStats(ctx context.Context, marketID string) (*Future[*protocol.GetStatsResponse], error)`
- `Depth(ctx context.Context, marketID string, limit uint32) (*Future[*protocol.GetDepthResponse], error)`

**Internal Flow:**
1. Acquire a channel from `responsePool`.
2. Publish an `InputEvent` with the `Query` and `Resp` channel.
3. Return a `Future` wrapping the response channel.
4. The caller uses `future.Wait(ctx)` to retrieve the result.

### 2.2 UpdateConfig Deserialization Fix
Modify `OrderBook.processCommand` to ensure `respondError` is called when `Unmarshal` fails for `CmdUpdateConfig`, `CmdSuspendMarket`, and `CmdResumeMarket`.

```go
// In order_book.go: processCommand
case protocol.CmdUpdateConfig:
    payload := &protocol.UpdateConfigCommand{}
    if err := book.serializer.Unmarshal(cmd.Payload, payload); err != nil {
        book.rejectInvalidPayload(cmd.CommandID, "unknown", 0, protocol.RejectReasonInvalidPayload, nil, 0)
        book.respondError(ev, err) // Ensure response is sent
        return
    }
    book.handleUpdateConfig(ev, payload)
```

## 3. Impact & Benefits
- **Safety**: Eliminates the risk of a new query reading a late response from a timed-out previous query.
- **Consistency**: All interactions that pass through the RingBuffer and wait for a response now follow the same `Future` pattern.
- **Reliability**: Guarantees that management APIs will always return (either with success or error) regardless of the failure stage.

## 4. Testing Strategy
- **Pollution Test**: Reuse and adapt `TestManagement_LateResponsePollution` to ensure queries are also protected.
- **Hanging Test**: Add a test case that sends a malformed `UpdateConfig` payload and verifies that `future.Wait()` returns an error immediately instead of timing out.
- **Integration Tests**: Update existing tests in `engine_test.go` to use the new Future-based query API.
