# Typed Request Engine API Design

## Goal

Replace the current `protocol.Command + Params` envelope model with a typed request model that is shared across the OMS, MQ transport, and matching engine facade.

The public engine API should expose one method per business action while the internal engine execution model remains fully asynchronous through the existing single-threaded event loop.

## Motivation

The current API is transport-oriented:

- `Submit`
- `SubmitAsync`
- `SubmitAsyncBatch`
- `Query`

This is flexible, but it forces SDK users and service code to construct `protocol.Command` envelopes manually even when they are performing simple business operations such as placing an order or creating a market.

That model made more sense when the engine was also responsible for serialization. Now that serialization has already been moved into `protocol`, the extra envelope layer no longer improves usability.

The desired direction is:

1. OMS constructs typed request structs from the `protocol` package.
2. OMS serializes them with `protocol`.
3. Matching service deserializes them with `protocol`.
4. Matching service calls a matching engine facade method such as `PlaceOrderAsync`.

## Scope

This design includes:

- replacing `protocol.Command + Params` with typed request structs
- keeping a shared wire header through `BaseCommand`
- defining the new engine facade methods
- preserving high-performance manual binary serialization

This design does not include:

- decimal wire-format optimization
- query API redesign
- changing the internal event-loop architecture

## Core Decision

The `protocol` package becomes the shared command schema for both transport and engine facade input.

The `match` package should not define duplicate request structs such as `match.PlaceOrderRequest`.

The engine facade should accept `*protocol.XxxRequest` directly.

## Protocol Model

### Base Type

Every command request should embed a shared base struct:

```go
type BaseCommand struct {
	Type      CommandType
	SeqID     uint64 // Upstream-assigned monotonic sequence used to preserve logical command ordering.
	CommandID string
	UserID    uint64
	MarketID  string
	Timestamp int64
}
```

`Type` remains part of the shared header because the binary decoder still needs a stable message-kind field to decide which request type to instantiate.

`SeqID` is part of the shared header because the engine and surrounding services need a transport-level ordering field that is assigned by the upstream sequencer or OMS rather than generated locally inside the engine.

### Typed Requests

The `protocol` package should define typed request structs such as:

```go
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

type CancelOrderRequest struct {
	BaseCommand
	OrderID string
}

type AmendOrderRequest struct {
	BaseCommand
	OrderID  string
	NewPrice udecimal.Decimal
	NewSize  udecimal.Decimal
}

type CreateMarketRequest struct {
	BaseCommand
	MinLotSize udecimal.Decimal
}

type SuspendMarketRequest struct {
	BaseCommand
	Reason string
}

type ResumeMarketRequest struct {
	BaseCommand
}

type UpdateConfigRequest struct {
	BaseCommand
	MinLotSize udecimal.Decimal
}
```

## Protocol Serialization

The `protocol` package should keep the current high-performance approach:

- manual binary encoding
- no JSON in the hot path
- no reflection-based serialization
- no compatibility bridge for the old command envelope

The new public functions should be:

```go
func MarshalRequest(req any) ([]byte, error)
func UnmarshalRequest(data []byte) (any, error)
```

The wire format should still contain:

- shared command header
- command type tag
- request-specific body

The old `Command`, `Params`, `SetPayload`, `MarshalCommand`, and `UnmarshalCommand` APIs should be removed as part of the same breaking change.

## Engine API

The engine should expose typed facade methods that map directly to business actions.

### Management Commands

These should remain synchronous-from-the-caller-perspective by returning a `Future`:

```go
func (engine *MatchingEngine) CreateMarket(
	ctx context.Context,
	req *protocol.CreateMarketRequest,
) (*Future[bool], error)

func (engine *MatchingEngine) SuspendMarket(
	ctx context.Context,
	req *protocol.SuspendMarketRequest,
) (*Future[bool], error)

func (engine *MatchingEngine) ResumeMarket(
	ctx context.Context,
	req *protocol.ResumeMarketRequest,
) (*Future[bool], error)

func (engine *MatchingEngine) UpdateConfig(
	ctx context.Context,
	req *protocol.UpdateConfigRequest,
) (*Future[bool], error)
```

`Future[bool]` indicates enqueue success plus execution completion. Business rejection behavior should remain aligned with the existing event and error model.

### Trading Commands

These should remain asynchronous and optimized for high throughput:

```go
func (engine *MatchingEngine) PlaceOrderAsync(
	ctx context.Context,
	req *protocol.PlaceOrderRequest,
) error

func (engine *MatchingEngine) PlaceOrderBatchAsync(
	ctx context.Context,
	reqs []*protocol.PlaceOrderRequest,
) error

func (engine *MatchingEngine) CancelOrderAsync(
	ctx context.Context,
	req *protocol.CancelOrderRequest,
) error

func (engine *MatchingEngine) AmendOrderAsync(
	ctx context.Context,
	req *protocol.AmendOrderRequest,
) error
```

These methods return only enqueue-time errors.

## Internal Engine Architecture

The internal execution model should not change:

- a single engine event loop
- a single ring buffer
- asynchronous command handling
- read queries still routed through the query path

The facade methods are only responsible for:

- validating request shape
- mapping typed requests into internal engine events
- preserving the current enqueue semantics

They should not introduce synchronous business logic on the caller goroutine.

## MQ Flow

The intended end-to-end message flow becomes:

1. OMS constructs a typed request from `protocol`.
2. OMS calls `protocol.MarshalRequest`.
3. OMS sends the resulting bytes to MQ.
4. Matching service receives bytes from MQ.
5. Matching service calls `protocol.UnmarshalRequest`.
6. Matching service uses a type switch and dispatches to the corresponding engine facade method.

Example:

```go
msg, err := protocol.UnmarshalRequest(data)
if err != nil {
	return err
}

switch req := msg.(type) {
case *protocol.PlaceOrderRequest:
	return engine.PlaceOrderAsync(ctx, req)
case *protocol.CancelOrderRequest:
	return engine.CancelOrderAsync(ctx, req)
case *protocol.CreateMarketRequest:
	f, err := engine.CreateMarket(ctx, req)
	if err != nil {
		return err
	}
	_, err = f.Wait(ctx)
	return err
default:
	return ErrUnknownCommand
}
```

## Breaking Change Policy

Backward compatibility is explicitly out of scope.

The old `protocol.Command` model should be removed directly rather than kept behind a compatibility layer. This avoids a prolonged double-model period and keeps the architecture consistent.

## Risks

- Removing `Command` in one step will require broad refactoring across tests, benchmarks, docs, and examples.
- If request validation is split inconsistently between facade methods and engine internals, behavior may become harder to reason about.
- If the binary format is rewritten carelessly, transport regressions may be introduced even if the API design is cleaner.

## Recommendation

Implement the new typed request model as a full breaking change:

- replace `protocol.Command + Params`
- expose typed engine facade methods
- keep `Submit*` only if they are still useful as low-level internal primitives
- keep the internal engine fully asynchronous

This gives the cleanest public API while preserving the current performance-oriented engine architecture.
