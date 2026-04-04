# Design Specification: Distributed-Ready Full-Struct API & Binary Protocol (v2.0)

This document outlines the architectural refactoring of the `MatchingEngine` to support a high-performance, distributed trading system (OMS -> MQ -> Matching Engine) using a unified Binary Protocol and "Envelope/Params" separation.

## 1. Architectural Principles
- **Envelope/Params Separation**: `protocol.Command` acts as the "Envelope" (metadata for routing), while `protocol.xxxParams` acts as the "Payload" (business data).
- **Manual Binary Serialization**: Abandon JSON/Sonic. All business data is serialized via manual, zero-allocation `MarshalBinary` for maximum performance.
- **Distributed-Ready**: The SDK provides a core `EnqueueCommand` entry point to accept pre-wrapped envelopes from upstream services (OMS).
- **Type-Safe API**: Maintain specialized methods (e.g., `PlaceOrder`) as convenience wrappers for local usage and testing.

## 2. Structural Definitions

### 2.1 The Envelope: `protocol.Command`
The envelope contains only what is necessary for the engine to route and sequence the command.

```go
type Command struct {
    Version   uint8       // Protocol version
    Type      CommandType // e.g., CmdPlaceOrder, CmdCreateMarket
    CommandID string      // Unique ID for deduplication
    MarketID  string      // Target market for routing
    Timestamp int64       // Logical timestamp (UnixNano)
    SeqID     uint64      // Assigned by engine on entry
    Payload   []byte      // Binary-serialized xxxParams
}
```

### 2.2 The Payloads: `protocol.xxxParams`
All business-specific structures are renamed from `xxxCommand` to `xxxParams`. They contain metadata for developer convenience but their binary serialization only includes business data.

| Struct Name | SDK Developer Convenience Fields | Business Fields (Serialized) |
| :--- | :--- | :--- |
| `PlaceOrderParams` | `CommandID`, `MarketID`, `Timestamp` | `OrderID`, `Side`, `Price`, `Size`, `UserID`, etc. |
| `CancelOrderParams` | `CommandID`, `MarketID`, `Timestamp` | `OrderID`, `UserID` |
| `AmendOrderParams` | `CommandID`, `MarketID`, `Timestamp` | `OrderID`, `UserID`, `NewPrice`, `NewSize` |
| `CreateMarketParams` | `CommandID`, `MarketID`, `Timestamp` | `UserID`, `MinLotSize` |
| `SuspendMarketParams` | `CommandID`, `Timestamp` | `UserID`, `MarketID`, `Reason` |
| `ResumeMarketParams` | `CommandID`, `Timestamp` | `UserID`, `MarketID` |
| `UpdateConfigParams` | `CommandID`, `Timestamp` | `UserID`, `MarketID`, `MinLotSize` |

## 3. High-Performance Serialization
Every `xxxParams` struct must implement:
- `MarshalBinary() ([]byte, error)`: Uses `binary.BigEndian` to write fields into a pre-allocated or pooled buffer.
- `UnmarshalBinary(data []byte) error`: Directly reads from the buffer.

**JSON is completely removed from the hot path.**

## 4. MatchingEngine API

### 4.1 Core Submission Entry (for ME Service)
```go
// Primary entry point for envelopes received from MQ
func (e *MatchingEngine) EnqueueCommand(ctx context.Context, cmd *protocol.Command) error
```

### 4.2 Convenience Wrappers (for SDK Users)
```go
func (e *MatchingEngine) PlaceOrder(ctx context.Context, params *protocol.PlaceOrderParams) error {
    // 1. Validate CommandID/MarketID in params
    // 2. data, _ := params.MarshalBinary()
    // 3. envelope := &protocol.Command{
    //        Type: protocol.CmdPlaceOrder,
    //        CommandID: params.CommandID,
    //        MarketID: params.MarketID,
    //        Timestamp: params.Timestamp,
    //        Payload: data,
    //    }
    // 4. return e.EnqueueCommand(ctx, envelope)
}
```

## 5. Migration Tasks
1.  **Refactor `protocol/command.go`**:
    - Rename all `xxxCommand` to `xxxParams`.
    - Implement/Update `MarshalBinary` and `UnmarshalBinary` for all `Params` types.
    - Add `Timestamp` to `protocol.Command`.
2.  **Refactor `engine.go`**:
    - Update public method signatures to use `xxxParams`.
    - Update internal logic to use manual binary serialization.
    - Ensure `EnqueueCommand` is the central hub.
3.  **Update `order_book.go`**:
    - Update command handlers to use `UnmarshalBinary`.
4.  **Cleanup**:
    - Remove `sonic` or `encoding/json` dependencies from `MatchingEngine` and `OrderBook`.
5.  **Test Parity**:
    - Update all unit and integration tests to the new API.
    - Verify that binary payloads are correctly restored from snapshots.
