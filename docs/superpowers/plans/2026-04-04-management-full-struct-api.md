# Management & Trading Full-Struct API Refactoring Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor the MatchingEngine API to use a "Full-Struct" pattern with manual binary serialization, supporting high-performance distributed architectures (OMS -> MQ -> ME).

**Architecture:** 
1. **Envelope/Params Separation**: Standardize `protocol.Command` as the routing envelope.
2. **Binary Protocol**: Replace JSON with manual `MarshalBinary` for all command parameters (`xxxParams`).
3. **Unified Entry Point**: Expose `EnqueueCommand` for pre-wrapped envelopes while providing typed convenience wrappers.

**Tech Stack:** Go, binary.BigEndian, Disruptor (RingBuffer).

---

### Task 1: Refactor Protocol Envelope and Base Types

**Files:**
- Modify: `protocol/command.go`

- [ ] **Step 1: Add Timestamp to `protocol.Command` envelope**
```go
type Command struct {
    Version   uint8
    MarketID  string
    SeqID     uint64
    Type      CommandType
    CommandID string
    Timestamp int64 // Added
    Payload   []byte
}
```

- [ ] **Step 2: Define `MarshalBinary` and `UnmarshalBinary` for `protocol.Command`**
Implement manual serialization/deserialization for the envelope structure using `binary.BigEndian`.

- [ ] **Step 3: Run protocol tests with race detection**
Run: `go test -race -v ./protocol/...`
Expected: PASS

### Task 2: Rename Commands to Params and Implement Binary Serialization

**Files:**
- Modify: `protocol/command.go`

- [ ] **Step 1: Rename all `xxxCommand` to `xxxParams`**
- `CreateMarketCommand` -> `CreateMarketParams`
- `SuspendMarketCommand` -> `SuspendMarketParams`
- `ResumeMarketCommand` -> `ResumeMarketParams`
- `UpdateConfigCommand` -> `UpdateConfigParams`
- `UserEventCommand` -> `UserEventParams`
- `PlaceOrderCommand` -> `PlaceOrderParams`
- `CancelOrderCommand` -> `CancelOrderParams`
- `AmendOrderCommand` -> `AmendOrderParams`

- [ ] **Step 2: Add Envelope fields to Params with `json:"-"`**
Ensure all Params structs include `CommandID`, `MarketID`, and `Timestamp` (where applicable) with the correct JSON tags to avoid redundancy in payloads.

- [ ] **Step 3: Implement `MarshalBinary` for all Params**
Implement for: `CreateMarketParams`, `SuspendMarketParams`, `ResumeMarketParams`, `UpdateConfigParams`, `UserEventParams`, `PlaceOrderParams`, `CancelOrderParams`, `AmendOrderParams`. Use `binary.BigEndian` for zero-allocation performance.

- [ ] **Step 4: Run protocol tests with race detection**
Run: `go test -race -v ./protocol/...`
Expected: PASS

### Task 3: Refactor MatchingEngine API Signatures

**Files:**
- Modify: `engine.go`

- [ ] **Step 1: Update Management API signatures**
```go
func (engine *MatchingEngine) CreateMarket(ctx context.Context, params *protocol.CreateMarketParams) (*Future[bool], error)
// Repeat for Suspend, Resume, UpdateConfig
```

- [ ] **Step 2: Update Trading API signatures**
```go
func (engine *MatchingEngine) PlaceOrder(ctx context.Context, params *protocol.PlaceOrderParams) error
// Repeat for Cancel, Amend
```

- [ ] **Step 3: Update Internal Logic (Envelope Wrapping)**
Inside each method, extract metadata from `params`, call `params.MarshalBinary()`, and build the `protocol.Command` envelope before calling `EnqueueCommand`.

- [ ] **Step 4: Verify engine compilation**
Run: `go build ./...`

### Task 4: Update OrderBook Handlers to Use Binary Payloads

**Files:**
- Modify: `order_book.go`

- [ ] **Step 1: Update `processCommand` to use `UnmarshalBinary`**
Replace JSON/Sonic unmarshaling with `params.UnmarshalBinary(ev.Payload)`.

- [ ] **Step 2: Update individual handler signatures and logic**
Ensure handlers like `handleCreateMarket`, `handlePlaceOrder`, etc., correctly receive and process the new `xxxParams` structures.

- [ ] **Step 3: Remove JSON/Sonic dependencies**
Completely remove usage of JSON serializers in the hot path within `order_book.go`.

### Task 5: Refactor Tests, Examples and Final Quality Check

**Files:**
- Modify: `engine_test.go`, `order_book_test.go`, `README.md`, `examples/*.go`

- [ ] **Step 1: Update all test call sites and examples**
Update all usage of `CreateMarket`, `PlaceOrder`, etc., in tests and documentation to match the new struct-based API.

- [ ] **Step 2: Perform final project-wide quality check**
Run: `make check`
Expected: PASS (Linter passes and all tests pass with `-race`).
