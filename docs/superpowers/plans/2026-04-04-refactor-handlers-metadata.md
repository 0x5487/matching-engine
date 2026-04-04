# Refactor Handlers to use Envelope Metadata

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ensure all Handlers use envelope metadata (`protocol.Command`) instead of payload fields for `MarketID`, `CommandID`, and `Timestamp`.

**Architecture:** Update `engine.go` and `order_book.go` to extract routing and metadata fields from the command envelope rather than the deserialized payload. This aligns with the binary protocol change where these fields were removed from payloads to reduce redundancy and improve performance.

**Tech Stack:** Go (Matching Engine)

---

### Task 1: Refactor `engine.go`

**Files:**
- Modify: `engine.go`

- [ ] **Step 1: Update `handleCreateMarket` to use envelope metadata**
In `handleCreateMarket`, replace usages of `payload.MarketID` and `payload.Timestamp` with `cmd.MarketID` and `cmd.Timestamp`.

```go
// In engine.go
func (engine *MatchingEngine) handleCreateMarket(ev *InputEvent) {
	cmd := ev.Cmd
	payload := &protocol.CreateMarketParams{}
	if err := payload.UnmarshalBinary(cmd.Payload); err != nil {
		engine.rejectCommand(cmd, protocol.RejectReasonInvalidPayload)
		engine.respondQueryError(ev, errors.New(string(protocol.RejectReasonInvalidPayload)))
		return
	}
	// Use cmd.Timestamp instead of payload.Timestamp
	if cmd.Timestamp <= 0 {
		engine.rejectCommandWithMarket(
			cmd,
			cmd.MarketID, // Use cmd.MarketID
			protocol.RejectReasonInvalidPayload,
			cmd.Timestamp, // Use cmd.Timestamp
		)
		engine.respondQueryError(ev, errors.New(string(protocol.RejectReasonInvalidPayload)))
		return
	}

	if _, exists := engine.orderbooks[cmd.MarketID]; exists { // Use cmd.MarketID
		engine.rejectCommandWithMarket(
			cmd,
			cmd.MarketID, // Use cmd.MarketID
			protocol.RejectReasonMarketAlreadyExists,
			cmd.Timestamp,
		)
		engine.respondQueryError(ev, errors.New(string(protocol.RejectReasonMarketAlreadyExists)))
		return
	}

	// Create and Store
	opts := []OrderBookOption{}
	if payload.MinLotSize != "" {
		size, err := udecimal.Parse(payload.MinLotSize)
		if err != nil {
			engine.rejectCommandWithMarket(
				cmd,
				cmd.MarketID, // Use cmd.MarketID
				protocol.RejectReasonInvalidPayload,
				cmd.Timestamp,
			)
			engine.respondQueryError(ev, err)
			return
		}
		opts = append(opts, WithLotSize(size))
	}

	newbook := newOrderBook(engine.engineID, cmd.MarketID, engine.publishTrader, opts...) // Use cmd.MarketID
	engine.orderbooks[cmd.MarketID] = newbook // Use cmd.MarketID
    // ... rest of method
}
```

- [ ] **Step 2: Verify `engine.go` changes**
Run `go build ./...` to ensure it compiles.

### Task 2: Refactor `order_book.go` signature and calls

**Files:**
- Modify: `order_book.go`

- [ ] **Step 1: Update `rejectInvalidPayload` signature**
Add `marketID string` as the second parameter to `rejectInvalidPayload`.

```go
// In order_book.go
func (book *OrderBook) rejectInvalidPayload(
	commandID string,
	marketID string, // Added
	orderID string,
	userID uint64,
	reason protocol.RejectReason,
	timestamp int64,
) {
	batch := acquireLogBatch()
	log := NewRejectLog(
		book.seqID.Add(1),
		commandID,
		book.engineID,
		marketID, // Use passed marketID
		orderID,
		userID,
		reason,
		timestamp,
	)
	batch.Logs = append(batch.Logs, log)
	book.publisher.Publish(batch.Logs)
	releaseBookLog(log)
	batch.Release()
}
```

- [ ] **Step 2: Update all 28+ calls to `rejectInvalidPayload` in `order_book.go`**
Search for all calls to `rejectInvalidPayload` and update them to pass `cmd.MarketID` (or `book.marketID` if `cmd` is not available, but usually it is). Ensure `cmd.CommandID` and `cmd.Timestamp` are also used correctly.

Example for `CmdSuspendMarket` in `processCommand`:
```go
	case protocol.CmdSuspendMarket:
		payload := &protocol.SuspendMarketParams{}
		if err := payload.UnmarshalBinary(cmd.Payload); err != nil {
			book.rejectInvalidPayload(cmd.CommandID, cmd.MarketID, "unknown", 0, protocol.RejectReasonInvalidPayload, cmd.Timestamp)
			book.respondError(ev, err)
			return
		}
		book.handleSuspendMarket(ev, payload)
```

And for `CmdUpdateConfig` where it checks `payload.Timestamp`:
```go
		if cmd.Timestamp <= 0 { // Change to cmd.Timestamp
			book.rejectInvalidPayload(
				cmd.CommandID,
				cmd.MarketID, // Use cmd.MarketID
				"unknown", // Was payload.MarketID
				payload.UserID,
				protocol.RejectReasonInvalidPayload,
				cmd.Timestamp, // Use cmd.Timestamp
			)
			book.respondError(ev, errors.New(string(protocol.RejectReasonInvalidPayload)))
			return
		}
```

- [ ] **Step 3: Update `handleSuspendMarket`, `handleResumeMarket`, `handleUpdateConfig`**
Ensure these methods use `cmd.MarketID` and `cmd.Timestamp`.

```go
func (book *OrderBook) handleSuspendMarket(ev *InputEvent, payload *protocol.SuspendMarketParams) {
	cmd := ev.Cmd
	if cmd.Timestamp <= 0 { // Use cmd.Timestamp
		book.rejectInvalidPayload(
			cmd.CommandID,
			cmd.MarketID, // Use cmd.MarketID
			"unknown",
			payload.UserID,
			protocol.RejectReasonInvalidPayload,
			cmd.Timestamp, // Use cmd.Timestamp
		)
		book.respondError(ev, errors.New(string(protocol.RejectReasonInvalidPayload)))
		return
	}
    // ...
	log := NewAdminLog(
		book.seqID.Add(1),
		cmd.CommandID,
		book.engineID,
		book.marketID, // book.marketID is fine here, or use cmd.MarketID
		payload.UserID,
		"market_suspended",
		cmd.Timestamp, // Use cmd.Timestamp
	)
    // ...
}
```

- [ ] **Step 4: Verify `order_book.go` changes**
Run `go build ./...` to ensure it compiles.

### Task 3: Regression Testing

**Files:**
- Test: `engine_test.go`, `order_book_test.go`

- [ ] **Step 1: Run all tests**
Run `go test ./...` to ensure no regressions. Pay special attention to tests that might rely on `MarketID` in payload (they might need updates if they were manually constructing binary payloads with `MarketID` inside).

- [ ] **Step 2: Specifically verify "market_already_exists" fix**
Ensure that creating a market with a valid ID in the envelope but empty in the payload works correctly and doesn't trigger "market_already_exists" incorrectly due to empty ID.
