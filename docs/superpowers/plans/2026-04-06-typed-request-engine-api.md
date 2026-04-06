# Typed Request Engine API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the old `protocol.Command + Params` model with typed protocol requests and expose matching engine business methods that consume those request types directly.

**Architecture:** The `protocol` package becomes the single shared command schema across OMS, MQ transport, and engine facade input. The engine keeps the same single-threaded asynchronous event loop internally, but public write APIs become action-specific methods like `CreateMarket`, `PlaceOrderAsync`, and `CancelOrderAsync`.

**Tech Stack:** Go, manual binary serialization in `protocol`, typed request structs, existing `Future[T]`, current ring-buffer-based matching engine.

---

### Task 1: Replace protocol envelope types with typed request structs

**Files:**
- Modify: `protocol/command.go`
- Modify: `protocol/command_test.go`
- Modify: `protocol/protocol_bench_test.go`

- [ ] **Step 1: Replace `Command` and payload structs with `BaseCommand` and request structs**

Update `protocol/command.go` so the public model becomes:

```go
type BaseCommand struct {
	Type      CommandType
	SeqID     uint64 // Upstream-assigned monotonic sequence used to preserve logical command ordering.
	CommandID string
	UserID    uint64
	MarketID  string
	Timestamp int64
}

type PlaceOrderRequest struct {
	BaseCommand
	OrderID     string
	Side        Side
	OrderType   OrderType
	Price       udecimal.Decimal
	Size        udecimal.Decimal
	VisibleSize udecimal.Decimal
	QuoteSize   udecimal.Decimal
}
```

Expected: `Command`, `Params`, and `SetPayload` no longer exist as public protocol types.

- [ ] **Step 2: Add the remaining request structs**

Add:

```go
type CancelOrderRequest struct { BaseCommand; OrderID string }
type AmendOrderRequest struct { BaseCommand; OrderID string; NewPrice udecimal.Decimal; NewSize udecimal.Decimal }
type CreateMarketRequest struct { BaseCommand; MinLotSize udecimal.Decimal }
type SuspendMarketRequest struct { BaseCommand; Reason string }
type ResumeMarketRequest struct { BaseCommand }
type UpdateConfigRequest struct { BaseCommand; MinLotSize udecimal.Decimal }
```

Expected: all previous command variants have one typed request struct each.

- [ ] **Step 3: Keep manual binary encode/decode on the new types**

Implement request-local helpers or shared helpers so each request can still support:

```go
func (r *PlaceOrderRequest) MarshalBinary() ([]byte, error)
func (r *PlaceOrderRequest) UnmarshalBinary(data []byte) error
func (r *PlaceOrderRequest) BinarySize() int
```

Expected: binary encoding remains manual and does not use JSON or reflection.

- [ ] **Step 4: Add unified request codec entry points**

Replace the old command codec with:

```go
func MarshalRequest(req any) ([]byte, error)
func UnmarshalRequest(data []byte) (any, error)
```

Expected: the wire format still starts with shared header fields and `CommandType`, then decodes into a concrete `*protocol.XxxRequest`.

- [ ] **Step 5: Rewrite protocol tests around typed requests**

Update `protocol/command_test.go` and `protocol/protocol_bench_test.go` so tests cover:

```go
req := &PlaceOrderRequest{
	BaseCommand: BaseCommand{
		Type:      CmdPlaceOrder,
		SeqID:     123,
		CommandID: "cmd-1",
		UserID:    42,
		MarketID:  "BTC-USDT",
		Timestamp: 1000,
	},
	OrderID:   "order-1",
	Side:      SideBuy,
	OrderType: OrderTypeLimit,
	Price:     udecimal.MustFromInt64(100, 0),
	Size:      udecimal.MustFromInt64(1, 0),
}
```

Expected: encode/decode tests now assert round-tripping concrete request structs through `MarshalRequest` / `UnmarshalRequest`.

### Task 2: Replace engine input events with typed requests

**Files:**
- Modify: `models.go`
- Modify: `engine.go`
- Modify: `order_book.go`

- [ ] **Step 1: Change internal input event shape**

Update `models.go`:

```go
type InputEvent struct {
	Request any
	Query   any
	Resp    chan any
}
```

Expected: engine events no longer carry `*protocol.Command`.

- [ ] **Step 2: Change engine processing to switch on concrete request types**

Update `engine.go` and `order_book.go` so the write path works like:

```go
switch req := ev.Request.(type) {
case *protocol.PlaceOrderRequest:
	book.handlePlaceOrder(ev, req)
case *protocol.CancelOrderRequest:
	book.handleCancelOrder(ev, req)
case *protocol.AmendOrderRequest:
	book.handleAmendOrder(ev, req)
case *protocol.SuspendMarketRequest:
	book.handleSuspendMarket(ev, req)
case *protocol.ResumeMarketRequest:
	book.handleResumeMarket(ev, req)
case *protocol.UpdateConfigRequest:
	book.handleUpdateConfig(ev, req)
case *protocol.CreateMarketRequest:
	engine.handleCreateMarket(ev, req)
default:
	// send error response if present
}
```

Expected: there is no command envelope parsing inside the engine.

- [ ] **Step 3: Update handlers to consume typed requests directly**

For example:

```go
func (book *OrderBook) handlePlaceOrder(ev *InputEvent, req *protocol.PlaceOrderRequest) {
	order := acquireOrder()
	order.ID = req.OrderID
	order.UserID = req.UserID
	order.Side = req.Side
	order.Price = req.Price
	order.Size = req.Size
	order.Type = req.OrderType
	order.Timestamp = req.Timestamp
	// existing matching logic continues
}
```

Expected: handler logic uses request fields directly and no longer type-asserts `cmd.Params`.

### Task 3: Add typed engine facade methods

**Files:**
- Modify: `engine.go`
- Modify: `engine_test.go`

- [ ] **Step 1: Add the management facade methods**

Implement:

```go
func (engine *MatchingEngine) CreateMarket(ctx context.Context, req *protocol.CreateMarketRequest) (*Future[bool], error)
func (engine *MatchingEngine) SuspendMarket(ctx context.Context, req *protocol.SuspendMarketRequest) (*Future[bool], error)
func (engine *MatchingEngine) ResumeMarket(ctx context.Context, req *protocol.ResumeMarketRequest) (*Future[bool], error)
func (engine *MatchingEngine) UpdateConfig(ctx context.Context, req *protocol.UpdateConfigRequest) (*Future[bool], error)
```

Expected: each method validates the request, enqueues it, and returns a `Future[bool]`.

- [ ] **Step 2: Add the trading facade methods**

Implement:

```go
func (engine *MatchingEngine) PlaceOrderAsync(ctx context.Context, req *protocol.PlaceOrderRequest) error
func (engine *MatchingEngine) PlaceOrderBatchAsync(ctx context.Context, reqs []*protocol.PlaceOrderRequest) error
func (engine *MatchingEngine) CancelOrderAsync(ctx context.Context, req *protocol.CancelOrderRequest) error
func (engine *MatchingEngine) AmendOrderAsync(ctx context.Context, req *protocol.AmendOrderRequest) error
```

Expected: these methods return enqueue-time errors only.

- [ ] **Step 3: Decide the fate of low-level `Submit*` methods**

Keep `Submit`, `SubmitAsync`, and `SubmitAsyncBatch` only if they are still useful as internal primitives for the facade implementation. Remove them from the public API if they no longer serve an external purpose.

Expected: the public API is centered around business actions, not transport envelopes.

### Task 4: Update tests, benchmarks, and docs to the new request model

**Files:**
- Modify: `engine_test.go`
- Modify: `order_book_test.go`
- Modify: `order_book_bench_test.go`
- Modify: `README.md`

- [ ] **Step 1: Replace command-envelope construction in tests**

Convert tests from patterns like:

```go
cmd := &protocol.Command{Type: protocol.CmdPlaceOrder, ...}
_ = cmd.SetPayload(&protocol.PlaceOrderParams{...})
err := engine.SubmitAsync(ctx, cmd)
```

to:

```go
req := &protocol.PlaceOrderRequest{
	BaseCommand: protocol.BaseCommand{
		Type:      protocol.CmdPlaceOrder,
		SeqID:     1,
		CommandID: "place-1",
		UserID:    1001,
		MarketID:  marketID,
		Timestamp: time.Now().UnixNano(),
	},
	OrderID:   "order-1",
	Side:      protocol.SideBuy,
	OrderType: protocol.OrderTypeLimit,
	Price:     udecimal.MustFromInt64(100, 0),
	Size:      udecimal.MustFromInt64(1, 0),
}
err := engine.PlaceOrderAsync(ctx, req)
```

- [ ] **Step 2: Replace benchmark setup and submit calls**

Update `order_book_bench_test.go` so benchmark pools use typed requests and the measured path calls `PlaceOrderAsync`, `PlaceOrderBatchAsync`, and the management/query facade that still exists.

Expected: benchmark logic remains the same, only the request construction model changes.

- [ ] **Step 3: Rewrite README examples**

Update examples to show:

- `protocol.PlaceOrderRequest`
- `protocol.CreateMarketRequest`
- `protocol.MarshalRequest` / `protocol.UnmarshalRequest`
- engine facade calls instead of `Submit*`

### Task 5: Verify the breaking change

**Files:**
- Modify: `protocol/command.go`
- Modify: `engine.go`
- Modify: `models.go`
- Modify: `order_book.go`
- Modify: tests and docs touched above

- [ ] **Step 1: Run focused protocol tests**

Run:

```bash
go test -race ./protocol -run 'TestCommand_MarshalUnmarshalBinary|TestMarshalUnmarshalCommand|TestCommand_SetAndUnmarshalPayload'
```

Expected: update the test names if they were renamed; the protocol package should pass its focused request-codec tests.

- [ ] **Step 2: Run focused engine tests**

Run:

```bash
go test -race . -run 'TestMatchingEngine|TestCreateMarket|TestSuspendMarket|TestResumeMarket'
```

Expected: engine write-path tests pass under the new typed API.

- [ ] **Step 3: Run the benchmark suite**

Run:

```bash
make bench
```

Expected: the benchmark matrix still runs and reports the four standard benchmark lines.

- [ ] **Step 4: Run repository checks**

Run:

```bash
make check
```

Expected: capture the actual result and explicitly note any unrelated pre-existing failures instead of assuming success.
