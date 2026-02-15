package match

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/0x5487/matching-engine/protocol"
	"github.com/quagmt/udecimal"
)

// MatchingEngine manages multiple order books for different markets.
// It uses a single shared RingBuffer (Disruptor) for all commands,
// allowing the entire event loop to run on a single goroutine.
// This enables runtime.LockOSThread() for CPU affinity scenarios.
type MatchingEngine struct {
	isShutdown    atomic.Bool
	orderbooks    map[string]*OrderBook
	ring          *RingBuffer[InputEvent]
	publishTrader PublishLog
	serializer    protocol.Serializer
}

// NewMatchingEngine creates a new matching engine instance.
func NewMatchingEngine(publishTrader PublishLog) *MatchingEngine {
	engine := &MatchingEngine{
		orderbooks:    make(map[string]*OrderBook),
		publishTrader: publishTrader,
		serializer:    &protocol.DefaultJSONSerializer{},
	}

	engine.ring = NewRingBuffer(32768, engine)

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
		engine.processCommand(ev.Cmd)
		return
	}

	if ev.Query != nil {
		engine.processQuery(ev)
		return
	}
}

func (engine *MatchingEngine) processCommand(cmd *protocol.Command) {
	if cmd.Type == protocol.CmdCreateMarket {
		engine.handleCreateMarket(cmd)
		return
	}

	book := engine.orderBook(cmd.MarketID)
	if book == nil {
		return
	}
	book.processCommand(cmd)

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
		}
	case *protocol.GetStatsRequest:
		book := engine.orderBook(q.MarketID)
		if book != nil {
			book.processQuery(ev)
		}
	case *OrderBookSnapshot:
		book := engine.orderBook(q.MarketID)
		if book != nil {
			book.processQuery(ev)
		}
	case *engineSnapshotQuery:
		engine.handleSnapshotQuery(ev)
	}
}

// handleSnapshotQuery creates snapshots for all OrderBooks synchronously
// (on the same goroutine as the event loop, ensuring consistency).
func (engine *MatchingEngine) handleSnapshotQuery(ev *InputEvent) {
	var results []snapshotResult

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

// PlaceOrder adds an order to the appropriate order book based on the market ID.
// Returns ErrShutdown if the engine is shutting down or ErrNotFound if market doesn't exist.
func (engine *MatchingEngine) PlaceOrder(ctx context.Context, marketID string, cmd *protocol.PlaceOrderCommand) error {
	bytes, err := engine.serializer.Marshal(cmd)
	if err != nil {
		return err
	}
	protoCmd := &protocol.Command{
		MarketID: marketID,
		Type:     protocol.CmdPlaceOrder,
		Payload:  bytes,
	}
	return engine.EnqueueCommand(protoCmd)
}

// AmendOrder modifies an existing order in the appropriate order book.
// Returns ErrShutdown if the engine is shutting down or ErrNotFound if market doesn't exist.
func (engine *MatchingEngine) AmendOrder(ctx context.Context, marketID string, cmd *protocol.AmendOrderCommand) error {
	bytes, err := engine.serializer.Marshal(cmd)
	if err != nil {
		return err
	}
	protoCmd := &protocol.Command{
		MarketID: marketID,
		Type:     protocol.CmdAmendOrder,
		Payload:  bytes,
	}
	return engine.EnqueueCommand(protoCmd)
}

// CancelOrder cancels an order in the appropriate order book.
// Returns ErrShutdown if the engine is shutting down or ErrNotFound if market doesn't exist.
func (engine *MatchingEngine) CancelOrder(ctx context.Context, marketID string, cmd *protocol.CancelOrderCommand) error {
	bytes, err := engine.serializer.Marshal(cmd)
	if err != nil {
		return err
	}
	protoCmd := &protocol.Command{
		MarketID: marketID,
		Type:     protocol.CmdCancelOrder,
		Payload:  bytes,
	}
	return engine.EnqueueCommand(protoCmd)
}

// CreateMarket sends a command to create a new market.
func (engine *MatchingEngine) CreateMarket(userID string, marketID string, minLotSize string) error {
	cmd := &protocol.CreateMarketCommand{
		UserID:     userID,
		MarketID:   marketID,
		MinLotSize: minLotSize,
	}
	bytes, err := engine.serializer.Marshal(cmd)
	if err != nil {
		return err
	}
	return engine.EnqueueCommand(&protocol.Command{
		Type:     protocol.CmdCreateMarket,
		MarketID: marketID,
		Payload:  bytes,
	})
}

// SuspendMarket sends a command to suspend a market.
func (engine *MatchingEngine) SuspendMarket(userID string, marketID string) error {
	cmd := &protocol.SuspendMarketCommand{
		UserID:   userID,
		MarketID: marketID,
		Reason:   string(protocol.RejectReasonMarketSuspended),
	}
	bytes, err := engine.serializer.Marshal(cmd)
	if err != nil {
		return err
	}
	return engine.EnqueueCommand(&protocol.Command{
		Type:     protocol.CmdSuspendMarket,
		MarketID: marketID,
		Payload:  bytes,
	})
}

// ResumeMarket sends a command to resume a market.
func (engine *MatchingEngine) ResumeMarket(userID string, marketID string) error {
	cmd := &protocol.ResumeMarketCommand{
		UserID:   userID,
		MarketID: marketID,
	}
	bytes, err := engine.serializer.Marshal(cmd)
	if err != nil {
		return err
	}
	return engine.EnqueueCommand(&protocol.Command{
		Type:     protocol.CmdResumeMarket,
		MarketID: marketID,
		Payload:  bytes,
	})
}

// UpdateConfig sends a command to update market configuration.
func (engine *MatchingEngine) UpdateConfig(userID string, marketID string, minLotSize string) error {
	cmd := &protocol.UpdateConfigCommand{
		UserID:     userID,
		MarketID:   marketID,
		MinLotSize: minLotSize,
	}
	bytes, err := engine.serializer.Marshal(cmd)
	if err != nil {
		return err
	}
	return engine.EnqueueCommand(&protocol.Command{
		Type:     protocol.CmdUpdateConfig,
		MarketID: marketID,
		Payload:  bytes,
	})
}

// GetStats returns usage statistics for the specified market.
func (engine *MatchingEngine) GetStats(marketID string) (*protocol.GetStatsResponse, error) {
	respChan := make(chan any, 1)
	engine.ring.Publish(InputEvent{
		Query: &protocol.GetStatsRequest{
			MarketID: marketID,
		},
		Resp: respChan,
	})

	select {
	case res := <-respChan:
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

	respChan := make(chan any, 1)
	engine.ring.Publish(InputEvent{
		Query: &protocol.GetDepthRequest{
			MarketID: marketID,
			Limit:    limit,
		},
		Resp: respChan,
	})

	select {
	case res := <-respChan:
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
// or the context is cancelled.
func (engine *MatchingEngine) Shutdown(ctx context.Context) error {
	engine.isShutdown.Store(true)
	return engine.ring.Shutdown(ctx)
}

// snapshotResult wraps a snapshot result with potential error
type snapshotResult struct {
	snap *OrderBookSnapshot
	err  error
}

// TakeSnapshot captures a consistent snapshot of all order books and writes them to the specified directory.
// It generates two files: `snapshot.bin` (binary data) and `metadata.json` (metadata).
// Returns the metadata object or an error.
func (e *MatchingEngine) TakeSnapshot(outputDir string) (*SnapshotMetadata, error) {
	// Request snapshots from all OrderBooks through the RingBuffer
	// This ensures snapshots are taken on the consumer goroutine (no race conditions)
	respChan := make(chan any, 1)
	e.ring.Publish(InputEvent{
		Query: &engineSnapshotQuery{},
		Resp:  respChan,
	})

	var results []snapshotResult
	select {
	case res := <-respChan:
		results = res.([]snapshotResult)
	case <-time.After(10 * time.Second):
		return nil, ErrTimeout
	}

	// Use a temporary directory for atomic writes
	tmpDir := outputDir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, err
	}

	// Track GlobalLastCmdSeqID as max of all snapshots
	var globalSeqID uint64

	// Open snapshot.bin
	binPath := filepath.Join(tmpDir, "snapshot.bin")
	binFile, err := os.Create(binPath)
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
		data, err := json.Marshal(snap)
		if err != nil {
			binFile.Close()
			return nil, err
		}

		n, err := binFile.Write(data)
		if err != nil {
			binFile.Close()
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
		binFile.Close()
		return nil, errors.Join(snapshotErrors...)
	}

	// Write Footer
	footer := SnapshotFileFooter{Markets: markets}
	footerData, err := json.Marshal(footer)
	if err != nil {
		binFile.Close()
		return nil, err
	}

	// Write Footer JSON
	if _, err := binFile.Write(footerData); err != nil {
		binFile.Close()
		return nil, err
	}

	// Write Footer Length (4 bytes, Big Endian)
	if len(footerData) > 4294967295 {
		binFile.Close()
		return nil, errors.New("footer too large")
	}
	//nolint:gosec // Verified length above
	footerLen := uint32(len(footerData))
	if err := binary.Write(binFile, binary.BigEndian, footerLen); err != nil {
		binFile.Close()
		return nil, err
	}

	// Sync to ensure data is flushed to disk before checksum calculation
	if err := binFile.Sync(); err != nil {
		binFile.Close()
		return nil, err
	}

	// Close file before calculating checksum
	if err := binFile.Close(); err != nil {
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
	if err := os.WriteFile(metaPath, metaBytes, 0600); err != nil {
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
func (e *MatchingEngine) RestoreFromSnapshot(inputDir string) (*SnapshotMetadata, error) {
	// 1. Read metadata.json
	metaPath := filepath.Join(inputDir, "metadata.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}

	var meta SnapshotMetadata
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, err
	}

	// 2. Open snapshot.bin
	binPath := filepath.Join(inputDir, "snapshot.bin")
	binFile, err := os.Open(binPath)
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
	footerLenBytes := make([]byte, 4)
	stat, err := binFile.Stat()
	if err != nil {
		return nil, err
	}
	fileSize := stat.Size()

	if _, err := binFile.ReadAt(footerLenBytes, fileSize-4); err != nil {
		return nil, err
	}
	footerLen := binary.BigEndian.Uint32(footerLenBytes)

	// 4. Read Footer JSON
	footerOffset := fileSize - 4 - int64(footerLen)
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
		book := newOrderBook(segment.MarketID, e.publishTrader)
		book.Restore(&snap)

		// Add to engine map (no goroutine needed)
		e.orderbooks[segment.MarketID] = book
	}

	return &meta, nil
}

// handleCreateMarket handles the creation of a new market.
// This is handled asynchronously by the RingBuffer consumer.
func (engine *MatchingEngine) handleCreateMarket(cmd *protocol.Command) {
	payload := &protocol.CreateMarketCommand{}
	if err := engine.serializer.Unmarshal(cmd.Payload, payload); err != nil {
		logger.Error("failed to unmarshal CreateMarket command", "error", err)
		return
	}

	if _, exists := engine.orderbooks[payload.MarketID]; exists {
		logger.Warn("market already exists", "market_id", payload.MarketID)
		return
	}

	// Create and Store (no goroutine, no individual RingBuffer)
	opts := []OrderBookOption{}
	if payload.MinLotSize != "" {
		size, err := udecimal.Parse(payload.MinLotSize)
		if err == nil {
			opts = append(opts, WithLotSize(size))
		}
	}

	newbook := newOrderBook(payload.MarketID, engine.publishTrader, opts...)
	engine.orderbooks[payload.MarketID] = newbook
}

// orderBook is an internal helper to look up an OrderBook by marketID.
func (engine *MatchingEngine) orderBook(marketID string) *OrderBook {
	return engine.orderbooks[marketID]
}
