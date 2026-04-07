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

	engine.ring = NewRingBuffer(defaultRingBufferSize, &engineEventHandler{engine})

	return engine
}

// engineEventHandler is a private wrapper to hide the onEvent method from the public API.
type engineEventHandler struct {
	engine *MatchingEngine
}

func (h *engineEventHandler) OnEvent(ev *InputEvent) {
	h.engine.onEvent(ev)
}

// CreateMarket enqueues a typed create-market request and returns a future for completion.
func (engine *MatchingEngine) CreateMarket(
	ctx context.Context,
	req *protocol.CreateMarketRequest,
) (*Future[bool], error) {
	if err := engine.validateTypedRequest(ctx, req); err != nil {
		return nil, err
	}
	respChan := engine.acquireResponseChannel()
	if err := engine.enqueue(ctx, req, nil, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}
	return &Future[bool]{
		engine:   engine,
		respChan: respChan,
	}, nil
}

// SuspendMarket enqueues a typed suspend-market request and returns a future for completion.
func (engine *MatchingEngine) SuspendMarket(
	ctx context.Context,
	req *protocol.SuspendMarketRequest,
) (*Future[bool], error) {
	if err := engine.validateTypedRequest(ctx, req); err != nil {
		return nil, err
	}
	respChan := engine.acquireResponseChannel()
	if err := engine.enqueue(ctx, req, nil, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}
	return &Future[bool]{
		engine:   engine,
		respChan: respChan,
	}, nil
}

// ResumeMarket enqueues a typed resume-market request and returns a future for completion.
func (engine *MatchingEngine) ResumeMarket(
	ctx context.Context,
	req *protocol.ResumeMarketRequest,
) (*Future[bool], error) {
	if err := engine.validateTypedRequest(ctx, req); err != nil {
		return nil, err
	}
	respChan := engine.acquireResponseChannel()
	if err := engine.enqueue(ctx, req, nil, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}
	return &Future[bool]{
		engine:   engine,
		respChan: respChan,
	}, nil
}

// UpdateConfig enqueues a typed update-config request and returns a future for completion.
func (engine *MatchingEngine) UpdateConfig(
	ctx context.Context,
	req *protocol.UpdateConfigRequest,
) (*Future[bool], error) {
	if err := engine.validateTypedRequest(ctx, req); err != nil {
		return nil, err
	}
	respChan := engine.acquireResponseChannel()
	if err := engine.enqueue(ctx, req, nil, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}
	return &Future[bool]{
		engine:   engine,
		respChan: respChan,
	}, nil
}

// PlaceOrderAsync enqueues a typed place-order request without waiting for execution.
func (engine *MatchingEngine) PlaceOrderAsync(ctx context.Context, req *protocol.PlaceOrderRequest) error {
	if err := engine.validateTypedRequest(ctx, req); err != nil {
		return err
	}
	return engine.enqueue(ctx, req, nil, nil)
}

// PlaceOrderBatchAsync enqueues a batch of typed place-order requests without waiting for execution.
func (engine *MatchingEngine) PlaceOrderBatchAsync(
	ctx context.Context,
	reqs []*protocol.PlaceOrderRequest,
) error {
	if len(reqs) == 0 {
		return nil
	}
	if ctx == nil {
		return ErrInvalidParam
	}
	requests := make([]any, 0, len(reqs))
	for _, req := range reqs {
		if err := engine.validateTypedRequest(ctx, req); err != nil {
			return err
		}
		requests = append(requests, req)
	}
	return engine.enqueueBatch(ctx, requests)
}

// CancelOrderAsync enqueues a typed cancel-order request without waiting for execution.
func (engine *MatchingEngine) CancelOrderAsync(ctx context.Context, req *protocol.CancelOrderRequest) error {
	if err := engine.validateTypedRequest(ctx, req); err != nil {
		return err
	}
	return engine.enqueue(ctx, req, nil, nil)
}

// AmendOrderAsync enqueues a typed amend-order request without waiting for execution.
func (engine *MatchingEngine) AmendOrderAsync(ctx context.Context, req *protocol.AmendOrderRequest) error {
	if err := engine.validateTypedRequest(ctx, req); err != nil {
		return err
	}
	return engine.enqueue(ctx, req, nil, nil)
}

// SendUserEvent enqueues a typed user event request without waiting for execution.
func (engine *MatchingEngine) SendUserEvent(ctx context.Context, req *protocol.UserEventRequest) error {
	if err := engine.validateTypedRequest(ctx, req); err != nil {
		return err
	}
	return engine.enqueue(ctx, req, nil, nil)
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

// GetDepth executes a read-only depth query against the matching engine.
func (engine *MatchingEngine) GetDepth(
	ctx context.Context,
	query *protocol.GetDepthQuery,
) (*Future[*protocol.GetDepthResponse], error) {
	if query == nil {
		return nil, ErrInvalidParam
	}

	if engine.isShutdown.Load() {
		return nil, ErrShutdown
	}

	respChan := engine.acquireResponseChannel()
	if err := engine.enqueue(ctx, nil, query, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}

	return &Future[*protocol.GetDepthResponse]{
		engine:   engine,
		respChan: respChan,
	}, nil
}

// GetStats executes a read-only statistics query against the matching engine.
func (engine *MatchingEngine) GetStats(
	ctx context.Context,
	query *protocol.GetStatsQuery,
) (*Future[*protocol.GetStatsResponse], error) {
	if query == nil {
		return nil, ErrInvalidParam
	}

	if engine.isShutdown.Load() {
		return nil, ErrShutdown
	}

	respChan := engine.acquireResponseChannel()
	if err := engine.enqueue(ctx, nil, query, respChan); err != nil {
		engine.releaseResponseChannel(respChan)
		return nil, err
	}

	return &Future[*protocol.GetStatsResponse]{
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

type snapshotQuery struct{}

// TakeSnapshot captures a consistent snapshot of all order books and writes them to the specified directory.
// It generates two files: `snapshot.bin` (binary data) and `metadata.json` (metadata).
// Returns the metadata object or an error.
func (engine *MatchingEngine) TakeSnapshot(
	ctx context.Context,
	outputDir string,
) (*SnapshotMetadata, error) {
	if engine.isShutdown.Load() {
		return nil, ErrShutdown
	}

	respChan := engine.acquireResponseChannel()
	defer engine.releaseResponseChannel(respChan)

	query := &snapshotQuery{}

	if err := engine.enqueue(ctx, nil, query, respChan); err != nil {
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
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return engine.writeSnapshot(outputDir, results)
}

// RestoreFromSnapshot restores the entire matching engine state from a snapshot.
func (engine *MatchingEngine) RestoreFromSnapshot(inputDir string) (*SnapshotMetadata, error) {
	meta, err := engine.readMetadata(inputDir)
	if err != nil {
		return nil, err
	}

	binPath := filepath.Join(inputDir, "snapshot.bin")
	fileChecksum, err := calculateFileCRC32(binPath)
	if err != nil {
		return nil, err
	}
	if fileChecksum != meta.SnapshotChecksum {
		return nil, errors.New("snapshot.bin checksum mismatch")
	}

	binFile, err := os.Open(filepath.Clean(binPath))
	if err != nil {
		return nil, err
	}
	defer binFile.Close()

	footer, footerOffset, err := engine.readFooter(binFile)
	if err != nil {
		return nil, err
	}

	for _, segment := range footer.Markets {
		if err := engine.restoreMarket(binFile, segment, footerOffset); err != nil {
			return nil, err
		}
	}

	return meta, nil
}

func (engine *MatchingEngine) validateTypedRequest(ctx context.Context, req any) error {
	if ctx == nil || req == nil {
		return ErrInvalidParam
	}
	base, ok := protocol.GetRequestBase(req)
	if !ok {
		return ErrInvalidParam
	}
	if err := requireCommandID(base.CommandID); err != nil {
		return err
	}
	if engine.isShutdown.Load() {
		return ErrShutdown
	}
	return nil
}

func (engine *MatchingEngine) enqueue(
	ctx context.Context,
	request any,
	query any,
	resp chan any,
) error {
	strategy := YieldingIdleStrategy{}
	for {
		seq, ev := engine.ring.TryClaim()
		if seq != -1 {
			ev.Request = request
			ev.Query = query
			ev.Resp = resp

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

func (engine *MatchingEngine) enqueueBatch(ctx context.Context, requests []any) error {
	if len(requests) == 0 {
		return nil
	}
	if engine.isShutdown.Load() {
		return ErrShutdown
	}

	n := int64(len(requests))
	if n > engine.ring.capacity {
		for _, request := range requests {
			if err := engine.enqueue(ctx, request, nil, nil); err != nil {
				return err
			}
		}
		return nil
	}

	strategy := YieldingIdleStrategy{}
	for {
		startSeq, endSeq := engine.ring.TryClaimN(n)
		if startSeq != -1 {
			for i, request := range requests {
				seq := startSeq + int64(i)
				slot := &engine.ring.buffer[seq&engine.ring.bufferMask]
				slot.Request = request
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

func (engine *MatchingEngine) writeSnapshot(
	outputDir string,
	results []snapshotResult,
) (*SnapshotMetadata, error) {
	tmpDir := outputDir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tmpDir, tmpDirPerm); err != nil {
		return nil, err
	}

	binPath := filepath.Join(tmpDir, "snapshot.bin")
	binFile, err := os.Create(filepath.Clean(binPath))
	if err != nil {
		return nil, err
	}

	var globalSeqID uint64
	markets := make([]MarketSegment, 0, len(results))
	currentOffset := int64(0)
	var snapshotErrors []error

	for _, result := range results {
		if result.err != nil {
			snapshotErrors = append(snapshotErrors, result.err)
			continue
		}

		snap := result.snap
		snapData, errMarshal := json.Marshal(snap)
		if errMarshal != nil {
			_ = binFile.Close()
			return nil, errMarshal
		}

		n, errWrite := binFile.Write(snapData)
		if errWrite != nil {
			_ = binFile.Close()
			return nil, errWrite
		}

		length := int64(n)
		markets = append(markets, MarketSegment{
			MarketID: snap.MarketID,
			Offset:   currentOffset,
			Length:   length,
			Checksum: crc32.ChecksumIEEE(snapData),
		})
		currentOffset += length

		if snap.LastCmdSeqID > globalSeqID {
			globalSeqID = snap.LastCmdSeqID
		}
	}

	if len(snapshotErrors) > 0 {
		_ = binFile.Close()
		return nil, errors.Join(snapshotErrors...)
	}

	if errFooter := engine.writeFooter(binFile, markets); errFooter != nil {
		_ = binFile.Close()
		return nil, errFooter
	}

	if errSync := binFile.Sync(); errSync != nil {
		_ = binFile.Close()
		return nil, errSync
	}
	if errClose := binFile.Close(); errClose != nil {
		return nil, errClose
	}

	snapshotChecksum, errCRC := calculateFileCRC32(binPath)
	if errCRC != nil {
		return nil, errCRC
	}

	meta := &SnapshotMetadata{
		SchemaVersion:      SnapshotSchemaVersion,
		Timestamp:          time.Now().UnixNano(),
		GlobalLastCmdSeqID: globalSeqID,
		EngineVersion:      EngineVersion,
		SnapshotChecksum:   snapshotChecksum,
	}

	if errMeta := engine.writeMetadata(tmpDir, meta); errMeta != nil {
		return nil, errMeta
	}

	if errRemove := os.RemoveAll(outputDir); errRemove != nil {
		return nil, errRemove
	}
	return meta, os.Rename(tmpDir, outputDir)
}

func (engine *MatchingEngine) writeFooter(f *os.File, markets []MarketSegment) error {
	footer := SnapshotFileFooter{Markets: markets}
	footerData, err := json.Marshal(footer)
	if err != nil {
		return err
	}

	if _, err = f.Write(footerData); err != nil {
		return err
	}

	if len(footerData) > footerSizeLimit {
		return errors.New("footer too large")
	}

	/* #nosec G115 */
	return binary.Write(f, binary.BigEndian, uint32(len(footerData)))
}

func (engine *MatchingEngine) writeMetadata(dir string, meta *SnapshotMetadata) error {
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	metaPath := filepath.Join(dir, "metadata.json")
	return os.WriteFile(metaPath, metaBytes, metaFilePerm)
}

func (engine *MatchingEngine) readMetadata(dir string) (*SnapshotMetadata, error) {
	metaPath := filepath.Join(dir, "metadata.json")
	metaBytes, err := os.ReadFile(filepath.Clean(metaPath))
	if err != nil {
		return nil, err
	}

	var meta SnapshotMetadata
	if err = json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (engine *MatchingEngine) readFooter(f *os.File) (*SnapshotFileFooter, int64, error) {
	stat, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	fileSize := stat.Size()

	footerLenBytes := make([]byte, footerLenSize)
	if _, err = f.ReadAt(footerLenBytes, fileSize-int64(footerLenSize)); err != nil {
		return nil, 0, err
	}
	footerLen := binary.BigEndian.Uint32(footerLenBytes)

	footerOffset := fileSize - int64(footerLenSize) - int64(footerLen)
	if footerOffset < 0 {
		return nil, 0, errors.New("invalid snapshot footer offset")
	}

	footerBytes := make([]byte, footerLen)
	if _, err := f.ReadAt(footerBytes, footerOffset); err != nil {
		return nil, 0, err
	}

	var footer SnapshotFileFooter
	if err := json.Unmarshal(footerBytes, &footer); err != nil {
		return nil, 0, err
	}
	return &footer, footerOffset, nil
}

func (engine *MatchingEngine) restoreMarket(f *os.File, segment MarketSegment, footerOffset int64) error {
	if segment.Offset < 0 || segment.Length < 0 ||
		segment.Offset > footerOffset || segment.Length > footerOffset-segment.Offset {
		return errors.New("invalid snapshot segment bounds")
	}

	segmentData := make([]byte, segment.Length)
	if _, err := f.ReadAt(segmentData, segment.Offset); err != nil {
		return err
	}

	if crc32.ChecksumIEEE(segmentData) != segment.Checksum {
		return errors.New("checksum mismatch for market " + segment.MarketID)
	}

	var snap OrderBookSnapshot
	if err := json.Unmarshal(segmentData, &snap); err != nil {
		return err
	}

	book := newOrderBook(engine.engineID, segment.MarketID, engine.publishTrader)
	book.Restore(&snap)
	engine.orderbooks[segment.MarketID] = book
	return nil
}

func (engine *MatchingEngine) processCommand(ev *InputEvent) {
	req := ev.Request
	base, ok := protocol.GetRequestBase(req)
	if !ok || base.CommandID == "" {
		engine.rejectRequest(req, protocol.RejectReasonInvalidPayload)
		engine.respondQueryError(ev, errors.New(string(protocol.RejectReasonInvalidPayload)))
		return
	}

	switch request := req.(type) {
	case *protocol.CreateMarketRequest:
		engine.handleCreateMarketRequest(request, ev.Resp)
	case *protocol.UserEventRequest:
		engine.handleUserEvent(request)
	case *protocol.PlaceOrderRequest,
		*protocol.CancelOrderRequest,
		*protocol.AmendOrderRequest,
		*protocol.SuspendMarketRequest,
		*protocol.ResumeMarketRequest,
		*protocol.UpdateConfigRequest:
		book := engine.orderbooks[base.MarketID]
		if book == nil {
			engine.rejectRequest(req, protocol.RejectReasonMarketNotFound)
			engine.respondQueryError(ev, ErrNotFound)
			return
		}
		book.processCommand(ev)
		if base.SeqID > 0 {
			book.lastCmdSeqID.Store(base.SeqID)
		}
	default:
		engine.rejectRequest(req, protocol.RejectReasonUnknownCommand)
		engine.respondQueryError(ev, ErrUnknownCommand)
	}
}

func (engine *MatchingEngine) handleCreateMarketRequest(
	req *protocol.CreateMarketRequest,
	resp chan<- any,
) {
	engine.handleCreateMarket(
		req.CommandID,
		req.Timestamp,
		req.MarketID,
		req.UserID,
		req.MinLotSize,
		resp,
	)
}

func (engine *MatchingEngine) processQuery(ev *InputEvent) {
	switch q := ev.Query.(type) {
	case *protocol.GetDepthQuery:
		book := engine.orderbooks[q.MarketID]
		if book != nil {
			book.processQuery(ev)
		} else {
			engine.respondQueryError(ev, ErrNotFound)
		}
	case *protocol.GetStatsQuery:
		book := engine.orderbooks[q.MarketID]
		if book != nil {
			book.processQuery(ev)
		} else {
			engine.respondQueryError(ev, ErrNotFound)
		}
	case *snapshotQuery:
		engine.handleSnapshotQuery(ev)
	default:
		// Unsupported query type
		engine.respondQueryError(ev, ErrUnknownQuery)
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
	userID uint64,
	minLotSize udecimal.Decimal,
	resp chan<- any,
) {
	if ts <= 0 {
		engine.rejectLog(cmdID, marketID, userID, protocol.RejectReasonInvalidPayload, ts)
		if resp != nil {
			resp <- errors.New(string(protocol.RejectReasonInvalidPayload))
		}
		return
	}

	if _, exists := engine.orderbooks[marketID]; exists {
		engine.rejectLog(cmdID, marketID, userID, protocol.RejectReasonMarketAlreadyExists, ts)
		if resp != nil {
			resp <- errors.New(string(protocol.RejectReasonMarketAlreadyExists))
		}
		return
	}

	// Create and Store (no goroutine, no individual RingBuffer)
	opts := []OrderBookOption{}
	if !minLotSize.IsZero() {
		opts = append(opts, WithLotSize(minLotSize))
	}

	newbook := newOrderBook(engine.engineID, marketID, engine.publishTrader, opts...)
	engine.orderbooks[marketID] = newbook

	log := NewAdminLog(
		newbook.seqID.Add(1),
		cmdID,
		engine.engineID,
		marketID,
		userID,
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
func (engine *MatchingEngine) handleUserEvent(req *protocol.UserEventRequest) {
	if req.Timestamp <= 0 {
		engine.rejectRequest(req, protocol.RejectReasonInvalidPayload)
		return
	}

	// Create and Publish Log
	log := NewUserEventLog(
		req.SeqID,
		req.CommandID,
		engine.engineID,
		req.UserID,
		req.EventType,
		req.Key,
		req.Data,
		req.Timestamp,
	)

	// Publish via the shared publishTrader
	batch := acquireLogBatch()
	batch.Logs = append(batch.Logs, log)
	engine.publishTrader.Publish(batch.Logs)
	releaseBookLog(log)
	batch.Release()
}

func (engine *MatchingEngine) rejectLog(
	cmdID, marketID string,
	userID uint64,
	reason protocol.RejectReason,
	ts int64,
) {
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

// rejectRequest emits a standardized reject log for engine-level command failures.
func (engine *MatchingEngine) rejectRequest(req any, reason protocol.RejectReason) {
	base, ok := protocol.GetRequestBase(req)
	if !ok {
		return
	}
	log := NewRejectLog(
		0,
		base.CommandID,
		engine.engineID,
		base.MarketID,
		engine.requestOrderID(req),
		base.UserID,
		reason,
		base.Timestamp,
	)
	batch := acquireLogBatch()
	batch.Logs = append(batch.Logs, log)
	engine.publishTrader.Publish(batch.Logs)
	releaseBookLog(log)
	batch.Release()
}

// requestOrderID extracts the business identifier used for reject logs.
func (engine *MatchingEngine) requestOrderID(req any) string {
	switch r := req.(type) {
	case *protocol.PlaceOrderRequest:
		return r.OrderID
	case *protocol.CancelOrderRequest:
		return r.OrderID
	case *protocol.AmendOrderRequest:
		return r.OrderID
	case *protocol.UserEventRequest:
		return r.Key
	}
	return "unknown"
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

func (engine *MatchingEngine) onEvent(ev *InputEvent) {
	if ev.Request != nil {
		engine.processCommand(ev)
		return
	}

	if ev.Query != nil {
		engine.processQuery(ev)
		return
	}
}

func requireCommandID(commandID string) error {
	if commandID == "" {
		return ErrInvalidParam
	}
	return nil
}
