# Refactor MatchingEngine API Signatures Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor the public API of MatchingEngine to use `xxxParams` structs for consolidated arguments and switch to binary serialization for all command payloads.

**Architecture:** Update `engine.go` to match the new API signatures and ensure that internal command routing uses `protocol.Command` envelopes constructed from the parameters.

**Tech Stack:** Go (Matching Engine)

---

### Task 1: Refactor CreateMarket

**Files:**
- Modify: `/home/jason/_repository/public/matching-engine/engine.go`

- [ ] **Step 1: Update CreateMarket signature and implementation**
```go
func (engine *MatchingEngine) CreateMarket(ctx context.Context, params *protocol.CreateMarketParams) (*Future[bool], error) {
	if err := requireCommandID(params.CommandID); err != nil {
		return nil, err
	}
	bytes, err := params.MarshalBinary()
	if err != nil {
		return nil, err
	}

	respChan := engine.acquireResponseChannel()
	protoCmd := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		MarketID:  params.MarketID,
		CommandID: params.CommandID,
		Timestamp: params.Timestamp,
		Payload:   bytes,
	}

	if err := engine.enqueueCommandWithResponse(ctx, protoCmd, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}

	return &Future[bool]{
		engine:   engine,
		respChan: respChan,
	}, nil
}
```

### Task 2: Refactor SuspendMarket

**Files:**
- Modify: `/home/jason/_repository/public/matching-engine/engine.go`

- [ ] **Step 1: Update SuspendMarket signature and implementation**
```go
func (engine *MatchingEngine) SuspendMarket(ctx context.Context, params *protocol.SuspendMarketParams) (*Future[bool], error) {
	if err := requireCommandID(params.CommandID); err != nil {
		return nil, err
	}
	bytes, err := params.MarshalBinary()
	if err != nil {
		return nil, err
	}

	respChan := engine.acquireResponseChannel()
	protoCmd := &protocol.Command{
		Type:      protocol.CmdSuspendMarket,
		MarketID:  params.MarketID,
		CommandID: params.CommandID,
		Timestamp: params.Timestamp,
		Payload:   bytes,
	}

	if err := engine.enqueueCommandWithResponse(ctx, protoCmd, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}

	return &Future[bool]{
		engine:   engine,
		respChan: respChan,
	}, nil
}
```

### Task 3: Refactor ResumeMarket

**Files:**
- Modify: `/home/jason/_repository/public/matching-engine/engine.go`

- [ ] **Step 1: Update ResumeMarket signature and implementation**
```go
func (engine *MatchingEngine) ResumeMarket(ctx context.Context, params *protocol.ResumeMarketParams) (*Future[bool], error) {
	if err := requireCommandID(params.CommandID); err != nil {
		return nil, err
	}
	bytes, err := params.MarshalBinary()
	if err != nil {
		return nil, err
	}

	respChan := engine.acquireResponseChannel()
	protoCmd := &protocol.Command{
		Type:      protocol.CmdResumeMarket,
		MarketID:  params.MarketID,
		CommandID: params.CommandID,
		Timestamp: params.Timestamp,
		Payload:   bytes,
	}

	if err := engine.enqueueCommandWithResponse(ctx, protoCmd, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}

	return &Future[bool]{
		engine:   engine,
		respChan: respChan,
	}, nil
}
```

### Task 4: Refactor UpdateConfig

**Files:**
- Modify: `/home/jason/_repository/public/matching-engine/engine.go`

- [ ] **Step 1: Update UpdateConfig signature and implementation**
```go
func (engine *MatchingEngine) UpdateConfig(ctx context.Context, params *protocol.UpdateConfigParams) (*Future[bool], error) {
	if err := requireCommandID(params.CommandID); err != nil {
		return nil, err
	}
	bytes, err := params.MarshalBinary()
	if err != nil {
		return nil, err
	}

	respChan := engine.acquireResponseChannel()
	protoCmd := &protocol.Command{
		Type:      protocol.CmdUpdateConfig,
		MarketID:  params.MarketID,
		CommandID: params.CommandID,
		Timestamp: params.Timestamp,
		Payload:   bytes,
	}

	if err := engine.enqueueCommandWithResponse(ctx, protoCmd, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}

	return &Future[bool]{
		engine:   engine,
		respChan: respChan,
	}, nil
}
```

### Task 5: Refactor PlaceOrder

**Files:**
- Modify: `/home/jason/_repository/public/matching-engine/engine.go`

- [ ] **Step 1: Update PlaceOrder signature and implementation**
```go
func (engine *MatchingEngine) PlaceOrder(ctx context.Context, params *protocol.PlaceOrderParams) error {
	if err := requireCommandID(params.CommandID); err != nil {
		return err
	}
	bytes, err := params.MarshalBinary()
	if err != nil {
		return err
	}
	protoCmd := &protocol.Command{
		MarketID:  params.MarketID,
		Type:      protocol.CmdPlaceOrder,
		CommandID: params.CommandID,
		Timestamp: params.Timestamp,
		Payload:   bytes,
	}
	return engine.EnqueueCommand(ctx, protoCmd)
}
```

### Task 6: Refactor AmendOrder

**Files:**
- Modify: `/home/jason/_repository/public/matching-engine/engine.go`

- [ ] **Step 1: Update AmendOrder signature and implementation**
```go
func (engine *MatchingEngine) AmendOrder(ctx context.Context, params *protocol.AmendOrderParams) error {
	if err := requireCommandID(params.CommandID); err != nil {
		return err
	}
	bytes, err := params.MarshalBinary()
	if err != nil {
		return err
	}
	protoCmd := &protocol.Command{
		MarketID:  params.MarketID,
		Type:      protocol.CmdAmendOrder,
		CommandID: params.CommandID,
		Timestamp: params.Timestamp,
		Payload:   bytes,
	}
	return engine.EnqueueCommand(ctx, protoCmd)
}
```

### Task 7: Refactor CancelOrder

**Files:**
- Modify: `/home/jason/_repository/public/matching-engine/engine.go`

- [ ] **Step 1: Update CancelOrder signature and implementation**
```go
func (engine *MatchingEngine) CancelOrder(ctx context.Context, params *protocol.CancelOrderParams) error {
	if err := requireCommandID(params.CommandID); err != nil {
		return err
	}
	bytes, err := params.MarshalBinary()
	if err != nil {
		return err
	}
	protoCmd := &protocol.Command{
		MarketID:  params.MarketID,
		Type:      protocol.CmdCancelOrder,
		CommandID: params.CommandID,
		Timestamp: params.Timestamp,
		Payload:   bytes,
	}
	return engine.EnqueueCommand(ctx, protoCmd)
}
```

### Task 8: Refactor SendUserEvent

**Files:**
- Modify: `/home/jason/_repository/public/matching-engine/engine.go`

- [ ] **Step 1: Update SendUserEvent signature and implementation**
```go
func (engine *MatchingEngine) SendUserEvent(ctx context.Context, params *protocol.UserEventParams) error {
	if err := requireCommandID(params.CommandID); err != nil {
		return err
	}
	bytes, err := params.MarshalBinary()
	if err != nil {
		return err
	}
	return engine.EnqueueCommand(ctx, &protocol.Command{
		Type:      protocol.CmdUserEvent,
		MarketID:  params.MarketID,
		CommandID: params.CommandID,
		Timestamp: params.Timestamp,
		Payload:   bytes,
	})
}
```

### Task 9: Verification

- [ ] **Step 1: Build engine**
Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 2: Check for any other callers in engine.go**
Ensure that `PlaceOrderBatch` still compiles and works as intended (it was excluded from signature refactoring but might need internal adjustments).

Wait, `PlaceOrderBatch` still uses `engine.serializer.Marshal(cmd)`. I should update it to use `cmd.MarshalBinary()` for consistency.
Instruction says: `Call params.MarshalBinary() to get the payload.` - This might apply to `PlaceOrderBatch` internal implementation as well, even if signature is unchanged.

Let's review `PlaceOrderBatch` in `engine.go`:
```go
func (engine *MatchingEngine) PlaceOrderBatch(
	ctx context.Context,
	marketID string,
	cmds []*protocol.PlaceOrderParams,
) error {
    ...
	for _, cmd := range cmds {
		if err := requireCommandID(cmd.CommandID); err != nil {
			return err
		}
		bytes, err := engine.serializer.Marshal(cmd)
        ...
```
I'll update it to use `cmd.MarshalBinary()`.

- [ ] **Step 3: Update PlaceOrderBatch internal implementation**
```go
func (engine *MatchingEngine) PlaceOrderBatch(
	ctx context.Context,
	marketID string,
	cmds []*protocol.PlaceOrderParams,
) error {
	if len(cmds) == 0 {
		return nil
	}

	if engine.isShutdown.Load() {
		return ErrShutdown
	}

	protoCmds := make([]*protocol.Command, 0, len(cmds))
	for _, cmd := range cmds {
		if err := requireCommandID(cmd.CommandID); err != nil {
			return err
		}
		bytes, err := cmd.MarshalBinary()
		if err != nil {
			// Early return on serialization error - nothing has been inserted into the queue yet.
			return err
		}
		protoCmds = append(protoCmds, &protocol.Command{
			MarketID:  marketID,
			Type:      protocol.CmdPlaceOrder,
			CommandID: cmd.CommandID,
			Timestamp: cmd.Timestamp,
			Payload:   bytes,
		})
	}

	return engine.EnqueueCommandBatch(ctx, protoCmds)
}
```

Wait, I should also check `handleCreateMarket` etc. in `engine.go` because they use `engine.serializer.Unmarshal`.
The instruction didn't mention updating handlers, but it said "Update internal implementation".
However, `Unmarshal` might still be needed since `payload.UnmarshalBinary` is also available.
Actually, the instruction specifically mentioned `params.MarshalBinary()`.
For `Unmarshal`, `engine.serializer` is still okay, but `UnmarshalBinary` is more direct.
Let's see if I should update handlers too.
Instruction 3 says:
> Update internal implementation:
> - Extract `CommandID`, `MarketID`, `Timestamp` from `params`.
> - Call `params.MarshalBinary()` to get the payload.
> - Create `protocol.Command` (the envelope) using the extracted metadata and payload.
> - Ensure `requireCommandID(params.CommandID)` is checked.
> - Call `engine.enqueueCommandWithResponse` or `engine.EnqueueCommand`.

This clearly refers to the PUBLIC methods where `params` are passed in.
The `handlexxx` methods are private and they process `protocol.Command` from the ring buffer.

Wait, I should also check if `engine.serializer` field in `MatchingEngine` struct is still needed.
If all commands now use `MarshalBinary()`, `engine.serializer` might be used only for `Unmarshal`.
But `protocol.PlaceOrderParams` etc also have `UnmarshalBinary()`.
Wait, `protocol.Serializer` interface might be used elsewhere.
`MatchingEngine` has:
```go
	serializer    protocol.Serializer
```

Let's check `protocol/types.go` or where `Serializer` is defined.
I'll grep for `Serializer`.
