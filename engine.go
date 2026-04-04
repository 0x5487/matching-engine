package match

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quagmt/udecimal"

	"github.com/0x5487/matching-engine/protocol"
)

// MatchingEngine manages multiple order books for different markets.
// It uses a single shared RingBuffer (Disruptor) for all commands,
// allowing the entire event loop to run on a single goroutine.
// This enables runtime.LockOSThread() for CPU affinity scenarios.
type MatchingEngine struct {
	isShutdown    atomic.Bool
	engineID      string
	orderbooks    map[string]*OrderBook
	ring          *RingBuffer[InputEvent]
	publishTrader Publisher
	responsePool  *sync.Pool
}

const (
	defaultRingBufferSize = 32768
	snapshotTimeout       = 10 * time.Second
	tmpDirPerm            = 0o750
	metaFilePerm          = 0o600
	footerSizeLimit       = 4294967295
	footerLenSize         = 4
)

// NewMatchingEngine creates a new matching engine instance.
func NewMatchingEngine(engineID string, publishTrader Publisher) *MatchingEngine {
	engine := &MatchingEngine{
		engineID:      engineID,
		orderbooks:    make(map[string]*OrderBook),
		publishTrader: publishTrader,
		responsePool: &sync.Pool{
			New: func() any {
				return make(chan any, 1)
			},
		},
	}

	engine.ring = NewRingBuffer(defaultRingBufferSize, engine)

	return engine
}

// Submit sends a command to the engine and returns a Future for the result.
func (engine *MatchingEngine) Submit(ctx context.Context, cmd *protocol.Command) (*Future[any], error) {
	if ctx == nil || cmd == nil {
		return nil, ErrInvalidParam
	}
	if err := requireCommandID(cmd.CommandID); err != nil {
		return nil, err
	}
	if engine.isShutdown.Load() {
		return nil, ErrShutdown
	}

	respChan := engine.acquireResponseChannel()
	if err := engine.enqueueCommandWithResponse(ctx, cmd, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}

	return &Future[any]{
		engine:   engine,
		respChan: respChan,
	}, nil
}

// SubmitAsync sends a command to the engine without waiting for a result.
func (engine *MatchingEngine) SubmitAsync(ctx context.Context, cmd *protocol.Command) error {
	if ctx == nil || cmd == nil {
		return ErrInvalidParam
	}
	if err := requireCommandID(cmd.CommandID); err != nil {
		return err
	}
	return engine.EnqueueCommand(ctx, cmd)
}

// Run starts the engine's event loop. This is a blocking call.
// The consumer loop runs on the calling goroutine, enabling the caller
// to control thread affinity via runtime.LockOSThread().
//
// Usage:
//
//	go func() {
//	    runtime.LockOSThread()
//	    defer runtime.UnlockOSThread()
//	    engine.Run()
//	}()
func (engine *MatchingEngine) Run() error {
	engine.ring.Run()
	return nil
}

// OnEvent implements EventHandler[InputEvent] for the Engine's shared RingBuffer.
// It routes events to the appropriate OrderBook based on MarketID.
func (engine *MatchingEngine) OnEvent(ev *InputEvent) {
	if ev.Cmd != nil {
		engine.processCommand(ev)
		return
	}

	if ev.Query != nil {
		engine.processQuery(ev)
		return
	}
}

// EnqueueCommand routes the command to the Engine's shared RingBuffer.
// CreateMarket is handled synchronously so the OrderBook is immediately available.
func (engine *MatchingEngine) EnqueueCommand(ctx context.Context, cmd *protocol.Command) error {
	if engine.isShutdown.Load() {
		return ErrShutdown
	}

	strategy := YieldingIdleStrategy{}
	for {
		// All commands go through the shared RingBuffer
		seq, ev := engine.ring.TryClaim()
		if seq != -1 {
			ev.Cmd = cmd
			ev.Query = nil
			ev.Resp = nil

			engine.ring.Commit(seq)
			return nil
		}

		if engine.isShutdown.Load() {
			return ErrShutdown
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		strategy.Idle()
	}
}

// EnqueueCommandBatch routes a batch of commands to the Engine's shared RingBuffer.
// It claims n contiguous slots in the RingBuffer to amortize synchronization overhead.
func (engine *MatchingEngine) EnqueueCommandBatch(ctx context.Context, cmds []*protocol.Command) error {
	if len(cmds) == 0 {
		return nil
	}

	if engine.isShutdown.Load() {
		return ErrShutdown
	}

	n := int64(len(cmds))

	// Handle case where batch size exceeds ring buffer capacity limit
	if n > engine.ring.capacity {
		// Fallback to individual enqueues if batch is larger than capacity
		for _, cmd := range cmds {
			if err := engine.EnqueueCommand(ctx, cmd); err != nil {
				return err
			}
		}
		return nil
	}

	strategy := YieldingIdleStrategy{}
	for {
		startSeq, endSeq := engine.ring.TryClaimN(n)
		if startSeq != -1 {
			for i, cmd := range cmds {
				seq := startSeq + int64(i)
				slot := &engine.ring.buffer[seq&engine.ring.bufferMask]
				slot.Cmd = cmd
				slot.Query = nil
				slot.Resp = nil
			}

			engine.ring.CommitN(startSeq, endSeq)
			return nil
		}

		if engine.isShutdown.Load() {
			return ErrShutdown
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		strategy.Idle()
	}
}

func requireCommandID(commandID string) error {
	if commandID == "" {
		return ErrInvalidParam
	}
	return nil
}

// PlaceOrderBatch adds multiple orders to the appropriate order book(s).
// This method performs serialization before acquiring RingBuffer slots,
// ensuring that serialization errors do not block or waste RingBuffer sequences.
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
		// Use OrderID as CommandID for legacy batch support until Task 6
		if err := requireCommandID(cmd.OrderID); err != nil {
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
			CommandID: cmd.OrderID,
			Timestamp: time.Now().UnixNano(),
			Payload:   bytes,
		})
	}

	return engine.EnqueueCommandBatch(ctx, protoCmds)
}

// GetStats returns usage statistics for the specified market.
func (engine *MatchingEngine) GetStats(
	ctx context.Context,
	marketID string,
) (*Future[*protocol.GetStatsResponse], error) {
	if engine.isShutdown.Load() {
		return nil, ErrShutdown
	}

	respChan := engine.acquireResponseChannel()

	if err := engine.enqueueQueryWithResponse(ctx, &protocol.GetStatsRequest{
		MarketID: marketID,
	}, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}

	return &Future[*protocol.GetStatsResponse]{
		engine:   engine,
		respChan: respChan,
	}, nil
}

// Depth returns the current depth of the order book for the specified market.
func (engine *MatchingEngine) Depth(
	ctx context.Context,
	marketID string,
	limit uint32,
) (*Future[*protocol.GetDepthResponse], error) {
	if engine.isShutdown.Load() {
		return nil, ErrShutdown
	}

	if limit == 0 {
		return nil, ErrInvalidParam
	}

	respChan := engine.acquireResponseChannel()

	if err := engine.enqueueQueryWithResponse(ctx, &protocol.GetDepthRequest{
		MarketID: marketID,
		Limit:    limit,
	}, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}

	return &Future[*protocol.GetDepthResponse]{
		engine:   engine,
		respChan: respChan,
	}, nil
}

// Shutdown gracefully shuts down the engine.
// It blocks until all pending commands in the RingBuffer are processed
// or the context is canceled.
func (engine *MatchingEngine) Shutdown(ctx context.Context) error {
	engine.isShutdown.Store(true)
	return engine.ring.Shutdown(ctx)
}

// snapshotResult wraps a snapshot result with potential error.
type snapshotResult struct {
	snap *OrderBookSnapshot
	err  error
}

// TakeSnapshot captures a consistent snapshot of all order books and writes them to the specified directory.
// It generates two files: `snapshot.bin` (binary data) and `metadata.json` (metadata).
// Returns the metadata object or an error.
func (engine *MatchingEngine) TakeSnapshot(ctx context.Context, outputDir string) (*SnapshotMetadata, error) {
	if engine.isShutdown.Load() {
		return nil, ErrShutdown
	}

	// Request snapshots from all OrderBooks through the RingBuffer
	// This ensures snapshots are taken on the consumer goroutine (no race conditions)
	respChan := engine.acquireResponseChannel()
	var success bool
	defer func() {
		if success {
			engine.releaseResponseChannel(respChan)
		}
	}()

	if err := engine.enqueueQueryWithResponse(ctx, &engineSnapshotQuery{}, respChan); err != nil {
		return nil, err
	}

	var results []snapshotResult
	select {
	case res := <-respChan:
		r, ok := res.([]snapshotResult)
		if !ok {
			return nil, errors.New("unexpected response type for snapshot")
		}
		results = r
		success = true
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Use a temporary directory for atomic writes
	tmpDir := outputDir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return nil, err
	}
	// G301: permissions 0750
	if err := os.MkdirAll(tmpDir, tmpDirPerm); err != nil {
		return nil, err
	}

	// Track GlobalLastCmdSeqID as max of all snapshots
	var globalSeqID uint64

	// Open snapshot.bin

	binPath := filepath.Join(tmpDir, "snapshot.bin")
	binFile, err := os.Create(filepath.Clean(binPath))
	if err != nil {
		return nil, err
	}

	// Prepare Footer info
	markets := make([]MarketSegment, 0)
	currentOffset := int64(0)
	var snapshotErrors []error

	// Write snapshots
	for _, result := range results {
		if result.err != nil {
			snapshotErrors = append(snapshotErrors, result.err)
			continue
		}

		snap := result.snap

		// Serialize Market Data
		var data []byte
		data, err = json.Marshal(snap)
		if err != nil {
			_ = binFile.Close()
			return nil, err
		}

		var n int
		n, err = binFile.Write(data)
		if err != nil {
			_ = binFile.Close()
			return nil, err
		}

		length := int64(n)

		// Record Segment
		checksum := crc32.ChecksumIEEE(data)

		markets = append(markets, MarketSegment{
			MarketID: snap.MarketID,
			Offset:   currentOffset,
			Length:   length,
			Checksum: checksum,
		})

		currentOffset += length

		// Update GlobalLastCmdSeqID to max observed
		if snap.LastCmdSeqID > globalSeqID {
			globalSeqID = snap.LastCmdSeqID
		}
	}

	// If any snapshots failed, return error
	if len(snapshotErrors) > 0 {
		_ = binFile.Close()
		return nil, errors.Join(snapshotErrors...)
	}

	// Write Footer
	footer := SnapshotFileFooter{Markets: markets}
	var footerData []byte
	footerData, err = json.Marshal(footer)
	if err != nil {
		_ = binFile.Close()
		return nil, err
	}

	// Write Footer JSON
	if _, err = binFile.Write(footerData); err != nil {
		_ = binFile.Close()
		return nil, err
	}

	// Write Footer Length (4 bytes, Big Endian)
	if len(footerData) > footerSizeLimit {
		_ = binFile.Close()
		return nil, errors.New("footer too large")
	}
	//nolint:gosec // Verified length above
	footerLen := uint32(len(footerData))
	if err = binary.Write(binFile, binary.BigEndian, footerLen); err != nil {
		_ = binFile.Close()
		return nil, err
	}

	// Sync to ensure data is flushed to disk before checksum calculation
	if err = binFile.Sync(); err != nil {
		_ = binFile.Close()
		return nil, err
	}

	// Close file before calculating checksum
	if err = binFile.Close(); err != nil {
		return nil, err
	}

	// Calculate full file checksum
	snapshotChecksum, err := calculateFileCRC32(binPath)
	if err != nil {
		return nil, err
	}

	// Write metadata.json
	meta := &SnapshotMetadata{
		SchemaVersion:      SnapshotSchemaVersion,
		Timestamp:          time.Now().UnixNano(),
		GlobalLastCmdSeqID: globalSeqID,
		EngineVersion:      EngineVersion,
		SnapshotChecksum:   snapshotChecksum,
	}

	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, err
	}

	metaPath := filepath.Join(tmpDir, "metadata.json")
	if err = os.WriteFile(metaPath, metaBytes, metaFilePerm); err != nil {
		return nil, err
	}

	// Atomic rename: remove old dir and rename temp to final
	if err := os.RemoveAll(outputDir); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpDir, outputDir); err != nil {
		return nil, err
	}

	return meta, nil
}

// RestoreFromSnapshot restores the entire matching engine state from a snapshot in the specified directory.
// Returns the metadata from the snapshot for MQ replay positioning.
func (engine *MatchingEngine) RestoreFromSnapshot(inputDir string) (*SnapshotMetadata, error) {
	// 1. Read metadata.json
	metaPath := filepath.Join(inputDir, "metadata.json")
	metaBytes, err := os.ReadFile(filepath.Clean(metaPath))
	if err != nil {
		return nil, err
	}

	var meta SnapshotMetadata
	if err = json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, err
	}

	// 2. Open snapshot.bin
	binPath := filepath.Join(inputDir, "snapshot.bin")
	binFile, err := os.Open(filepath.Clean(binPath))
	if err != nil {
		return nil, err
	}
	defer binFile.Close()

	// 2.5 Verify full file checksum
	fileChecksum, err := calculateFileCRC32(binPath)
	if err != nil {
		return nil, err
	}
	if fileChecksum != meta.SnapshotChecksum {
		return nil, errors.New("snapshot.bin checksum mismatch")
	}

	// 3. Read Footer Length (last 4 bytes)
	footerLenBytes := make([]byte, footerLenSize)
	stat, err := binFile.Stat()
	if err != nil {
		return nil, err
	}
	fileSize := stat.Size()

	if _, err = binFile.ReadAt(footerLenBytes, fileSize-int64(footerLenSize)); err != nil {
		return nil, err
	}
	footerLen := binary.BigEndian.Uint32(footerLenBytes)
	if fileSize < int64(footerLenSize) || int64(footerLen) > fileSize-int64(footerLenSize) {
		return nil, errors.New("invalid snapshot footer length")
	}

	// 4. Read Footer JSON
	footerOffset := fileSize - int64(footerLenSize) - int64(footerLen)
	if footerOffset < 0 {
		return nil, errors.New("invalid snapshot footer offset")
	}
	footerBytes := make([]byte, footerLen)

	if _, err := binFile.ReadAt(footerBytes, footerOffset); err != nil {
		return nil, err
	}

	var footer SnapshotFileFooter
	if err := json.Unmarshal(footerBytes, &footer); err != nil {
		return nil, err
	}

	// 5. Restore OrderBooks (as managed books, no individual RingBuffers)
	for _, segment := range footer.Markets {
		if segment.Offset < 0 || segment.Length < 0 {
			return nil, errors.New("invalid snapshot segment bounds")
		}
		if segment.Offset > footerOffset || segment.Length > footerOffset-segment.Offset {
			return nil, errors.New("invalid snapshot segment bounds")
		}

		// Read segment data
		segmentData := make([]byte, segment.Length)
		if _, err := binFile.ReadAt(segmentData, segment.Offset); err != nil {
			return nil, err
		}

		// Checksum verification
		if crc32.ChecksumIEEE(segmentData) != segment.Checksum {
			return nil, errors.New("checksum mismatch for market " + segment.MarketID)
		}

		// Deserialize
		var snap OrderBookSnapshot
		if err := json.Unmarshal(segmentData, &snap); err != nil {
			return nil, err
		}

		// Create managed OrderBook (no individual RingBuffer) and restore
		book := newOrderBook(engine.engineID, segment.MarketID, engine.publishTrader)
		book.Restore(&snap)

		// Add to engine map (no goroutine needed)
		engine.orderbooks[segment.MarketID] = book
	}

	return &meta, nil
}

func (engine *MatchingEngine) processCommand(ev *InputEvent) {
	cmd := ev.Cmd
	if cmd.CommandID == "" {
		engine.rejectCommand(cmd, protocol.RejectReasonInvalidPayload)
		engine.respondQueryError(ev, errors.New(string(protocol.RejectReasonInvalidPayload)))
		return
	}

	if cmd.Type == protocol.CmdCreateMarket {
		params := &protocol.CreateMarketParams{}
		if err := params.UnmarshalBinary(cmd.Payload); err != nil {
			engine.rejectCommand(cmd, protocol.RejectReasonInvalidPayload)
			engine.respondQueryError(ev, errors.New(string(protocol.RejectReasonInvalidPayload)))
			return
		}
		engine.handleCreateMarket(cmd.CommandID, cmd.Timestamp, cmd.MarketID, params, ev.Resp)
		return
	}

	if cmd.Type == protocol.CmdUserEvent {
		engine.handleUserEvent(cmd)
		return
	}

	book := engine.orderBook(cmd.MarketID)
	if book == nil {
		engine.rejectCommand(cmd, protocol.RejectReasonMarketNotFound)
		engine.respondQueryError(ev, ErrNotFound)
		return
	}
	book.processCommand(ev)

	if cmd.SeqID > 0 {
		book.lastCmdSeqID.Store(cmd.SeqID)
	}
}

func (engine *MatchingEngine) processQuery(ev *InputEvent) {
	switch q := ev.Query.(type) {
	case *protocol.GetDepthRequest:
		book := engine.orderBook(q.MarketID)
		if book != nil {
			book.processQuery(ev)
		} else {
			engine.respondQueryError(ev, ErrNotFound)
		}
	case *protocol.GetStatsRequest:
		book := engine.orderBook(q.MarketID)
		if book != nil {
			book.processQuery(ev)
		} else {
			engine.respondQueryError(ev, ErrNotFound)
		}
	case *OrderBookSnapshot:
		book := engine.orderBook(q.MarketID)
		if book != nil {
			book.processQuery(ev)
		} else {
			engine.respondQueryError(ev, ErrNotFound)
		}
	case *engineSnapshotQuery:
		engine.handleSnapshotQuery(ev)
	}
}

// handleSnapshotQuery creates snapshots for all OrderBooks synchronously
// (on the same goroutine as the event loop, ensuring consistency).
func (engine *MatchingEngine) handleSnapshotQuery(ev *InputEvent) {
	results := make([]snapshotResult, 0, len(engine.orderbooks))

	for _, book := range engine.orderbooks {
		snap := book.createSnapshot()
		results = append(results, snapshotResult{snap: snap})
	}

	if ev.Resp != nil {
		select {
		case ev.Resp <- results:
		default:
		}
	}
}

// handleCreateMarket handles the creation of a new market.
func (engine *MatchingEngine) handleCreateMarket(
	cmdID string,
	ts int64,
	marketID string,
	params *protocol.CreateMarketParams,
	resp chan<- any,
) {
	if ts <= 0 {
		engine.rejectLog(cmdID, marketID, params.UserID, protocol.RejectReasonInvalidPayload, ts)
		if resp != nil {
			resp <- errors.New(string(protocol.RejectReasonInvalidPayload))
		}
		return
	}

	if _, exists := engine.orderbooks[marketID]; exists {
		engine.rejectLog(cmdID, marketID, params.UserID, protocol.RejectReasonMarketAlreadyExists, ts)
		if resp != nil {
			resp <- errors.New(string(protocol.RejectReasonMarketAlreadyExists))
		}
		return
	}

	// Create and Store (no goroutine, no individual RingBuffer)
	opts := []OrderBookOption{}
	if params.MinLotSize != "" {
		size, err := udecimal.Parse(params.MinLotSize)
		if err != nil {
			engine.rejectLog(cmdID, marketID, params.UserID, protocol.RejectReasonInvalidPayload, ts)
			if resp != nil {
				resp <- err
			}
			return
		}
		opts = append(opts, WithLotSize(size))
	}

	newbook := newOrderBook(engine.engineID, marketID, engine.publishTrader, opts...)
	engine.orderbooks[marketID] = newbook

	log := NewAdminLog(
		newbook.seqID.Add(1),
		cmdID,
		engine.engineID,
		marketID,
		params.UserID,
		"market_created",
		ts,
	)
	batch := acquireLogBatch()
	batch.Logs = append(batch.Logs, log)
	engine.publishTrader.Publish(batch.Logs)
	releaseBookLog(log)
	batch.Release()

	if resp != nil {
		select {
		case resp <- true:
		default:
		}
	}
}

// handleUserEvent processes a generic user event.
func (engine *MatchingEngine) handleUserEvent(cmd *protocol.Command) {
	payload := &protocol.UserEventParams{}
	if err := payload.UnmarshalBinary(cmd.Payload); err != nil {
		logger.Warn("failed to unmarshal UserEvent command", "error", err)
		engine.rejectCommand(cmd, protocol.RejectReasonInvalidPayload)
		return
	}
	if cmd.Timestamp <= 0 {
		engine.rejectCommand(cmd, protocol.RejectReasonInvalidPayload)
		return
	}

	// Create and Publish Log
	log := NewUserEventLog(
		cmd.SeqID,
		cmd.CommandID,
		engine.engineID,
		payload.UserID,
		payload.EventType,
		payload.Key,
		payload.Data,
		cmd.Timestamp,
	)

	// Publish via the shared publishTrader
	batch := acquireLogBatch()
	batch.Logs = append(batch.Logs, log)
	engine.publishTrader.Publish(batch.Logs)
	releaseBookLog(log)
	batch.Release()
}

func (engine *MatchingEngine) rejectLog(cmdID, marketID string, userID uint64, reason protocol.RejectReason, ts int64) {
	log := NewRejectLog(
		0,
		cmdID,
		engine.engineID,
		marketID,
		"", // OrderID and UserID are business data, not in envelope
		userID,
		reason,
		ts,
	)
	batch := acquireLogBatch()
	batch.Logs = append(batch.Logs, log)
	engine.publishTrader.Publish(batch.Logs)
	releaseBookLog(log)
	batch.Release()
}

// orderBook is an internal helper to look up an OrderBook by marketID.
func (engine *MatchingEngine) orderBook(marketID string) *OrderBook {
	return engine.orderbooks[marketID]
}

// respondQueryError returns a query-side error without waiting for timeout.
func (engine *MatchingEngine) respondQueryError(ev *InputEvent, err error) {
	if ev.Resp == nil {
		return
	}
	select {
	case ev.Resp <- err:
	default:
	}
}

// rejectCommand emits a standardized reject log for engine-level command failures.
func (engine *MatchingEngine) rejectCommand(cmd *protocol.Command, reason protocol.RejectReason) {
	engine.rejectCommandWithMarket(cmd, cmd.MarketID, reason, cmd.Timestamp)
}

// commandOrderID extracts the business identifier used for reject logs.
func (engine *MatchingEngine) commandOrderID(cmd *protocol.Command) string {
	switch cmd.Type {
	case protocol.CmdPlaceOrder:
		payload := &protocol.PlaceOrderParams{}
		if err := payload.UnmarshalBinary(cmd.Payload); err == nil {
			return payload.OrderID
		}
	case protocol.CmdCancelOrder:
		payload := &protocol.CancelOrderParams{}
		if err := payload.UnmarshalBinary(cmd.Payload); err == nil {
			return payload.OrderID
		}
	case protocol.CmdAmendOrder:
		payload := &protocol.AmendOrderParams{}
		if err := payload.UnmarshalBinary(cmd.Payload); err == nil {
			return payload.OrderID
		}
	case protocol.CmdUserEvent:
		payload := &protocol.UserEventParams{}
		if err := payload.UnmarshalBinary(cmd.Payload); err == nil {
			return payload.Key
		}
	default:
		return "unknown"
	}
	return "unknown"
}

// commandUserID extracts the actor identifier used for reject logs.
func (engine *MatchingEngine) commandUserID(cmd *protocol.Command) uint64 {
	switch cmd.Type {
	case protocol.CmdPlaceOrder:
		payload := &protocol.PlaceOrderParams{}
		if err := payload.UnmarshalBinary(cmd.Payload); err == nil {
			return payload.UserID
		}
	case protocol.CmdCancelOrder:
		payload := &protocol.CancelOrderParams{}
		if err := payload.UnmarshalBinary(cmd.Payload); err == nil {
			return payload.UserID
		}
	case protocol.CmdAmendOrder:
		payload := &protocol.AmendOrderParams{}
		if err := payload.UnmarshalBinary(cmd.Payload); err == nil {
			return payload.UserID
		}
	case protocol.CmdUserEvent:
		payload := &protocol.UserEventParams{}
		if err := payload.UnmarshalBinary(cmd.Payload); err == nil {
			return payload.UserID
		}
	case protocol.CmdCreateMarket:
		payload := &protocol.CreateMarketParams{}
		if err := payload.UnmarshalBinary(cmd.Payload); err == nil {
			return payload.UserID
		}
	case protocol.CmdSuspendMarket:
		payload := &protocol.SuspendMarketParams{}
		if err := payload.UnmarshalBinary(cmd.Payload); err == nil {
			return payload.UserID
		}
	case protocol.CmdResumeMarket:
		payload := &protocol.ResumeMarketParams{}
		if err := payload.UnmarshalBinary(cmd.Payload); err == nil {
			return payload.UserID
		}
	case protocol.CmdUpdateConfig:
		payload := &protocol.UpdateConfigParams{}
		if err := payload.UnmarshalBinary(cmd.Payload); err == nil {
			return payload.UserID
		}
	default:
		return 0
	}
	return 0
}

// rejectCommandWithMarket emits a standardized reject log for engine-level command failures.
func (engine *MatchingEngine) rejectCommandWithMarket(
	cmd *protocol.Command,
	marketID string,
	reason protocol.RejectReason,
	timestamp int64,
) {
	log := NewRejectLog(
		0,
		cmd.CommandID,
		engine.engineID,
		marketID,
		engine.commandOrderID(cmd),
		engine.commandUserID(cmd),
		reason,
		timestamp,
	)
	batch := acquireLogBatch()
	batch.Logs = append(batch.Logs, log)
	engine.publishTrader.Publish(batch.Logs)
	releaseBookLog(log)
	batch.Release()
}

func (engine *MatchingEngine) acquireResponseChannel() chan any {
	val := engine.responsePool.Get()
	ch, ok := val.(chan any)
	if !ok {
		// Should not happen with our pool setup, but satisfies linter
		return make(chan any, 1)
	}
	return ch
}

func (engine *MatchingEngine) releaseResponseChannel(ch chan any) {
	// Drain the channel if not empty
	select {
	case <-ch:
	default:
	}
	engine.responsePool.Put(ch)
}

func (engine *MatchingEngine) enqueueCommandWithResponse(
	ctx context.Context,
	cmd *protocol.Command,
	resp chan any,
) error {
	if engine.isShutdown.Load() {
		return ErrShutdown
	}

	strategy := YieldingIdleStrategy{}
	for {
		seq, ev := engine.ring.TryClaim()
		if seq != -1 {
			ev.Cmd = cmd
			ev.Query = nil
			ev.Resp = resp // Essential: Pass the response channel into the event

			engine.ring.Commit(seq)
			return nil
		}

		if engine.isShutdown.Load() {
			return ErrShutdown
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		strategy.Idle()
	}
}

func (engine *MatchingEngine) enqueueQueryWithResponse(ctx context.Context, query any, resp chan any) error {
	if engine.isShutdown.Load() {
		return ErrShutdown
	}

	strategy := YieldingIdleStrategy{}
	for {
		seq, ev := engine.ring.TryClaim()
		if seq != -1 {
			ev.Cmd = nil
			ev.Query = query
			ev.Resp = resp // Essential: Pass the response channel into the query

			engine.ring.Commit(seq)
			return nil
		}

		if engine.isShutdown.Load() {
			return ErrShutdown
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		strategy.Idle()
	}
}
