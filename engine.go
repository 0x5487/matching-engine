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
	publishTrader PublishLog
	serializer    protocol.Serializer
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
func NewMatchingEngine(engineID string, publishTrader PublishLog) *MatchingEngine {
	engine := &MatchingEngine{
		engineID:      engineID,
		orderbooks:    make(map[string]*OrderBook),
		publishTrader: publishTrader,
		serializer:    &protocol.FastBinarySerializer{},
		responsePool: &sync.Pool{
			New: func() any {
				return make(chan any, 1)
			},
		},
	}

	engine.ring = NewRingBuffer(defaultRingBufferSize, engine)

	return engine
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
func (engine *MatchingEngine) EnqueueCommand(cmd *protocol.Command) error {
	if engine.isShutdown.Load() {
		return ErrShutdown
	}

	// All commands go through the shared RingBuffer
	seq, ev := engine.ring.Claim()
	if seq == -1 {
		return ErrShutdown
	}

	ev.Cmd = cmd
	ev.Query = nil
	ev.Resp = nil

	engine.ring.Commit(seq)
	return nil
}

// EnqueueCommandBatch routes a batch of commands to the Engine's shared RingBuffer.
// It claims n contiguous slots in the RingBuffer to amortize synchronization overhead.
func (engine *MatchingEngine) EnqueueCommandBatch(cmds []*protocol.Command) error {
	if len(cmds) == 0 {
		return nil
	}

	if engine.isShutdown.Load() {
		return ErrShutdown
	}

	n := int64(len(cmds))

	// Handle case where batch size exceeds ring buffer capacity limit
	// Simplest approach: if it's too large, we fall back to smaller chunks or single enqueues.
	// In practice, RingBuffer capacity is large (32768), so typical batches (100-1000) will fit.
	if n > engine.ring.capacity {
		// Fallback to individual enqueues if batch is larger than capacity
		for _, cmd := range cmds {
			if err := engine.EnqueueCommand(cmd); err != nil {
				return err
			}
		}
		return nil
	}

	startSeq, endSeq := engine.ring.ClaimN(n)
	if startSeq == -1 {
		return ErrShutdown
	}

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

func (engine *MatchingEngine) acquireResponseChannel() chan any {
	val := engine.responsePool.Get()
	return val.(chan any)
}

func (engine *MatchingEngine) releaseResponseChannel(ch chan any) {
	// Drain the channel if not empty
	select {
	case <-ch:
	default:
	}
	engine.responsePool.Put(ch)
}

func (engine *MatchingEngine) enqueueCommandWithResponse(cmd *protocol.Command, resp chan any) error {
	if engine.isShutdown.Load() {
		return ErrShutdown
	}

	seq, ev := engine.ring.Claim()
	if seq == -1 {
		return ErrShutdown
	}

	ev.Cmd = cmd
	ev.Query = nil
	ev.Resp = resp // 關鍵：將響應通道傳入事件

	engine.ring.Commit(seq)
	return nil
}

func requireCommandID(commandID string) error {
	if commandID == "" {
		return ErrInvalidParam
	}
	return nil
}

// PlaceOrder adds an order to the appropriate order book based on the market ID.
// Returns ErrShutdown if the engine is shutting down or ErrNotFound if market doesn't exist.
func (engine *MatchingEngine) PlaceOrder(_ context.Context, marketID string, cmd *protocol.PlaceOrderCommand) error {
	if err := requireCommandID(cmd.CommandID); err != nil {
		return err
	}
	bytes, err := engine.serializer.Marshal(cmd)
	if err != nil {
		return err
	}
	protoCmd := &protocol.Command{
		MarketID:  marketID,
		Type:      protocol.CmdPlaceOrder,
		CommandID: cmd.CommandID,
		Payload:   bytes,
	}
	return engine.EnqueueCommand(protoCmd)
}

// PlaceOrderBatch adds multiple orders to the appropriate order book(s).
// This method performs serialization before acquiring RingBuffer slots,
// ensuring that serialization errors do not block or waste RingBuffer sequences.
func (engine *MatchingEngine) PlaceOrderBatch(
	_ context.Context,
	marketID string,
	cmds []*protocol.PlaceOrderCommand,
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
		bytes, err := engine.serializer.Marshal(cmd)
		if err != nil {
			// Early return on serialization error - nothing has been inserted into the queue yet.
			return err
		}
		protoCmds = append(protoCmds, &protocol.Command{
			MarketID:  marketID,
			Type:      protocol.CmdPlaceOrder,
			CommandID: cmd.CommandID,
			Payload:   bytes,
		})
	}

	return engine.EnqueueCommandBatch(protoCmds)
}

// AmendOrder modifies an existing order in the appropriate order book.
// Returns ErrShutdown if the engine is shutting down or ErrNotFound if market doesn't exist.
func (engine *MatchingEngine) AmendOrder(_ context.Context, marketID string, cmd *protocol.AmendOrderCommand) error {
	if err := requireCommandID(cmd.CommandID); err != nil {
		return err
	}
	bytes, err := engine.serializer.Marshal(cmd)
	if err != nil {
		return err
	}
	protoCmd := &protocol.Command{
		MarketID:  marketID,
		Type:      protocol.CmdAmendOrder,
		CommandID: cmd.CommandID,
		Payload:   bytes,
	}
	return engine.EnqueueCommand(protoCmd)
}

// CancelOrder cancels an order in the appropriate order book.
// Returns ErrShutdown if the engine is shutting down or ErrNotFound if market doesn't exist.
func (engine *MatchingEngine) CancelOrder(
	_ context.Context,
	marketID string,
	cmd *protocol.CancelOrderCommand,
) error {
	if err := requireCommandID(cmd.CommandID); err != nil {
		return err
	}
	bytes, err := engine.serializer.Marshal(cmd)
	if err != nil {
		return err
	}
	protoCmd := &protocol.Command{
		MarketID:  marketID,
		Type:      protocol.CmdCancelOrder,
		CommandID: cmd.CommandID,
		Payload:   bytes,
	}
	return engine.EnqueueCommand(protoCmd)
}

// CreateMarket sends a command to create a new market.
func (engine *MatchingEngine) CreateMarket(
	_ context.Context, // Use ctx for consistency with future API
	commandID string,
	userID uint64,
	marketID string,
	minLotSize string,
	timestamp int64,
) (*Future[bool], error) {
	if err := requireCommandID(commandID); err != nil {
		return nil, err
	}
	cmd := &protocol.CreateMarketCommand{
		UserID:     userID,
		MarketID:   marketID,
		MinLotSize: minLotSize,
		Timestamp:  timestamp,
	}
	bytes, err := engine.serializer.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	respChan := engine.acquireResponseChannel()
	protoCmd := &protocol.Command{
		Type:      protocol.CmdCreateMarket,
		MarketID:  marketID,
		CommandID: commandID,
		Payload:   bytes,
	}

	if err := engine.enqueueCommandWithResponse(protoCmd, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}

	return &Future[bool]{
		engine:   engine,
		respChan: respChan,
	}, nil
}

// SuspendMarket sends a command to suspend a market.
func (engine *MatchingEngine) SuspendMarket(
	_ context.Context, // Use ctx for consistency with future API
	commandID string,
	userID uint64,
	marketID string,
	timestamp int64,
) (*Future[bool], error) {
	if err := requireCommandID(commandID); err != nil {
		return nil, err
	}
	cmd := &protocol.SuspendMarketCommand{
		UserID:    userID,
		MarketID:  marketID,
		Reason:    string(protocol.RejectReasonMarketSuspended),
		Timestamp: timestamp,
	}
	bytes, err := engine.serializer.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	respChan := engine.acquireResponseChannel()
	protoCmd := &protocol.Command{
		Type:      protocol.CmdSuspendMarket,
		MarketID:  marketID,
		CommandID: commandID,
		Payload:   bytes,
	}

	if err := engine.enqueueCommandWithResponse(protoCmd, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}

	return &Future[bool]{
		engine:   engine,
		respChan: respChan,
	}, nil
}

// ResumeMarket sends a command to resume a market.
func (engine *MatchingEngine) ResumeMarket(
	_ context.Context, // Use ctx for consistency with future API
	commandID string,
	userID uint64,
	marketID string,
	timestamp int64,
) (*Future[bool], error) {
	if err := requireCommandID(commandID); err != nil {
		return nil, err
	}
	cmd := &protocol.ResumeMarketCommand{
		UserID:    userID,
		MarketID:  marketID,
		Timestamp: timestamp,
	}
	bytes, err := engine.serializer.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	respChan := engine.acquireResponseChannel()
	protoCmd := &protocol.Command{
		Type:      protocol.CmdResumeMarket,
		MarketID:  marketID,
		CommandID: commandID,
		Payload:   bytes,
	}

	if err := engine.enqueueCommandWithResponse(protoCmd, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}

	return &Future[bool]{
		engine:   engine,
		respChan: respChan,
	}, nil
}

// UpdateConfig sends a command to update market configuration.
func (engine *MatchingEngine) UpdateConfig(
	_ context.Context,
	commandID string,
	userID uint64,
	marketID string,
	minLotSize string,
	timestamp int64,
) (*Future[bool], error) {
	if err := requireCommandID(commandID); err != nil {
		return nil, err
	}
	cmd := &protocol.UpdateConfigCommand{
		UserID:     userID,
		MarketID:   marketID,
		MinLotSize: minLotSize,
		Timestamp:  timestamp,
	}
	bytes, err := engine.serializer.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	respChan := engine.acquireResponseChannel()
	protoCmd := &protocol.Command{
		Type:      protocol.CmdUpdateConfig,
		MarketID:  marketID,
		CommandID: commandID,
		Payload:   bytes,
	}

	if err := engine.enqueueCommandWithResponse(protoCmd, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}

	return &Future[bool]{
		engine:   engine,
		respChan: respChan,
	}, nil
}

// SendUserEvent sends a generic user event to the matching engine.
// These events are processed sequentially with trades and emitted via PublishLog.
func (engine *MatchingEngine) SendUserEvent(
	commandID string,
	userID uint64,
	eventType string,
	key string,
	data []byte,
	timestamp int64,
) error {
	if err := requireCommandID(commandID); err != nil {
		return err
	}
	cmd := &protocol.UserEventCommand{
		CommandID: commandID,
		UserID:    userID,
		EventType: eventType,
		Key:       key,
		Data:      data,
		Timestamp: timestamp,
	}
	bytes, err := engine.serializer.Marshal(cmd)
	if err != nil {
		return err
	}
	// MarketID is empty for global events, or could be specific if needed.
	// For now we treat them as global or engine-level events.
	return engine.EnqueueCommand(&protocol.Command{
		Type:      protocol.CmdUserEvent,
		CommandID: commandID,
		Payload:   bytes,
	})
}

// GetStats returns usage statistics for the specified market.
func (engine *MatchingEngine) GetStats(marketID string) (*protocol.GetStatsResponse, error) {
	val := engine.responsePool.Get()
	respChan, ok := val.(chan any)
	if !ok {
		return nil, errors.New("failed to get response channel from pool")
	}
	defer func() {
		// Ensure channel is drained before putting back
		select {
		case <-respChan:
		default:
		}
		engine.responsePool.Put(respChan)
	}()

	engine.ring.Publish(InputEvent{
		Query: &protocol.GetStatsRequest{
			MarketID: marketID,
		},
		Resp: respChan,
	})

	select {
	case res := <-respChan:
		if err, ok := res.(error); ok {
			return nil, err
		}
		if result, ok := res.(*protocol.GetStatsResponse); ok {
			return result, nil
		}
		return nil, errors.New("unexpected response type")
	case <-time.After(time.Second):
		return nil, ErrTimeout
	}
}

// Depth returns the current depth of the order book for the specified market.
func (engine *MatchingEngine) Depth(marketID string, limit uint32) (*protocol.GetDepthResponse, error) {
	if limit == 0 {
		return nil, ErrInvalidParam
	}

	val := engine.responsePool.Get()
	respChan, ok := val.(chan any)
	if !ok {
		return nil, errors.New("failed to get response channel from pool")
	}
	defer func() {
		select {
		case <-respChan:
		default:
		}
		engine.responsePool.Put(respChan)
	}()
	engine.ring.Publish(InputEvent{
		Query: &protocol.GetDepthRequest{
			MarketID: marketID,
			Limit:    limit,
		},
		Resp: respChan,
	})

	select {
	case res := <-respChan:
		if err, ok := res.(error); ok {
			return nil, err
		}
		if result, ok := res.(*protocol.GetDepthResponse); ok {
			return result, nil
		}
		return nil, errors.New("unexpected response type")
	case <-time.After(time.Second):
		return nil, ErrTimeout
	}
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
func (engine *MatchingEngine) TakeSnapshot(outputDir string) (*SnapshotMetadata, error) {
	// Request snapshots from all OrderBooks through the RingBuffer
	// This ensures snapshots are taken on the consumer goroutine (no race conditions)
	val := engine.responsePool.Get()
	respChan, ok := val.(chan any)
	if !ok {
		return nil, errors.New("failed to get response channel from pool")
	}
	defer func() {
		select {
		case <-respChan:
		default:
		}
		engine.responsePool.Put(respChan)
	}()
	engine.ring.Publish(InputEvent{
		Query: &engineSnapshotQuery{},
		Resp:  respChan,
	})

	var results []snapshotResult
	select {
	case res := <-respChan:
		results, ok = res.([]snapshotResult)
		if !ok {
			return nil, errors.New("unexpected response type for snapshot")
		}
	case <-time.After(snapshotTimeout):
		return nil, ErrTimeout
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
		return
	}

	if cmd.Type == protocol.CmdCreateMarket {
		engine.handleCreateMarket(ev)
		return
	}

	if cmd.Type == protocol.CmdUserEvent {
		engine.handleUserEvent(cmd)
		return
	}

	book := engine.orderBook(cmd.MarketID)
	if book == nil {
		engine.rejectCommand(cmd, protocol.RejectReasonMarketNotFound)
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
// This is handled asynchronously by the RingBuffer consumer.
func (engine *MatchingEngine) handleCreateMarket(ev *InputEvent) {
	cmd := ev.Cmd
	payload := &protocol.CreateMarketCommand{}
	if err := engine.serializer.Unmarshal(cmd.Payload, payload); err != nil {
		engine.rejectCommand(cmd, protocol.RejectReasonInvalidPayload)
		engine.respondQueryError(ev, errors.New(string(protocol.RejectReasonInvalidPayload)))
		return
	}
	if payload.Timestamp <= 0 {
		engine.rejectCommandWithMarket(
			cmd,
			payload.MarketID,
			protocol.RejectReasonInvalidPayload,
			payload.Timestamp,
		)
		engine.respondQueryError(ev, errors.New(string(protocol.RejectReasonInvalidPayload)))
		return
	}

	if _, exists := engine.orderbooks[payload.MarketID]; exists {
		engine.rejectCommandWithMarket(
			cmd,
			payload.MarketID,
			protocol.RejectReasonMarketAlreadyExists,
			payload.Timestamp,
		)
		engine.respondQueryError(ev, errors.New(string(protocol.RejectReasonMarketAlreadyExists)))
		return
	}

	// Create and Store (no goroutine, no individual RingBuffer)
	opts := []OrderBookOption{}
	if payload.MinLotSize != "" {
		size, err := udecimal.Parse(payload.MinLotSize)
		if err != nil {
			engine.rejectCommandWithMarket(
				cmd,
				payload.MarketID,
				protocol.RejectReasonInvalidPayload,
				payload.Timestamp,
			)
			engine.respondQueryError(ev, err)
			return
		}
		opts = append(opts, WithLotSize(size))
	}

	newbook := newOrderBook(engine.engineID, payload.MarketID, engine.publishTrader, opts...)
	engine.orderbooks[payload.MarketID] = newbook

	log := NewAdminLog(
		newbook.seqID.Add(1),
		cmd.CommandID,
		engine.engineID,
		payload.MarketID,
		payload.UserID,
		"market_created",
		payload.Timestamp,
	)
	logs := acquireLogSlice()
	*logs = append(*logs, log)
	engine.publishTrader.Publish(*logs)
	releaseBookLog(log)
	releaseLogSlice(logs)

	if ev.Resp != nil {
		select {
		case ev.Resp <- true:
		default:
		}
	}
}

// handleUserEvent processes a generic user event.
func (engine *MatchingEngine) handleUserEvent(cmd *protocol.Command) {
	payload := &protocol.UserEventCommand{}
	if err := engine.serializer.Unmarshal(cmd.Payload, payload); err != nil {
		logger.Error("failed to unmarshal UserEvent command", "error", err)
		engine.rejectCommand(cmd, protocol.RejectReasonInvalidPayload)
		return
	}
	if payload.Timestamp <= 0 {
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
		payload.Timestamp,
	)

	// Publish via the shared publishTrader
	// We wrap it in a slice as required by the interface
	logs := acquireLogSlice()
	*logs = append(*logs, log)
	engine.publishTrader.Publish(*logs)
	releaseBookLog(log)
	releaseLogSlice(logs)
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
	engine.rejectCommandWithMarket(cmd, cmd.MarketID, reason, engine.commandTimestamp(cmd))
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
	logs := acquireLogSlice()
	*logs = append(*logs, log)
	engine.publishTrader.Publish(*logs)
	releaseBookLog(log)
	releaseLogSlice(logs)
}

// commandTimestamp extracts the command timestamp when available.
func (engine *MatchingEngine) commandTimestamp(cmd *protocol.Command) int64 {
	switch cmd.Type {
	case protocol.CmdPlaceOrder:
		payload := &protocol.PlaceOrderCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.Timestamp
		}
	case protocol.CmdCancelOrder:
		payload := &protocol.CancelOrderCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.Timestamp
		}
	case protocol.CmdAmendOrder:
		payload := &protocol.AmendOrderCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.Timestamp
		}
	case protocol.CmdUserEvent:
		payload := &protocol.UserEventCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.Timestamp
		}
	case protocol.CmdCreateMarket:
		payload := &protocol.CreateMarketCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.Timestamp
		}
	case protocol.CmdSuspendMarket:
		payload := &protocol.SuspendMarketCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.Timestamp
		}
	case protocol.CmdResumeMarket:
		payload := &protocol.ResumeMarketCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.Timestamp
		}
	case protocol.CmdUpdateConfig:
		payload := &protocol.UpdateConfigCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.Timestamp
		}
	default:
		return 0
	}
	return 0
}

// commandOrderID extracts the identifier used for reject logs.
func (engine *MatchingEngine) commandOrderID(cmd *protocol.Command) string {
	switch cmd.Type {
	case protocol.CmdPlaceOrder:
		payload := &protocol.PlaceOrderCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.OrderID
		}
	case protocol.CmdCancelOrder:
		payload := &protocol.CancelOrderCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.OrderID
		}
	case protocol.CmdAmendOrder:
		payload := &protocol.AmendOrderCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.OrderID
		}
	case protocol.CmdUserEvent:
		payload := &protocol.UserEventCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
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
		payload := &protocol.PlaceOrderCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.UserID
		}
	case protocol.CmdCancelOrder:
		payload := &protocol.CancelOrderCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.UserID
		}
	case protocol.CmdAmendOrder:
		payload := &protocol.AmendOrderCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.UserID
		}
	case protocol.CmdUserEvent:
		payload := &protocol.UserEventCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.UserID
		}
	case protocol.CmdCreateMarket:
		payload := &protocol.CreateMarketCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.UserID
		}
	case protocol.CmdSuspendMarket:
		payload := &protocol.SuspendMarketCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.UserID
		}
	case protocol.CmdResumeMarket:
		payload := &protocol.ResumeMarketCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.UserID
		}
	case protocol.CmdUpdateConfig:
		payload := &protocol.UpdateConfigCommand{}
		if err := engine.serializer.Unmarshal(cmd.Payload, payload); err == nil {
			return payload.UserID
		}
	default:
		return 0
	}
	return 0
}
