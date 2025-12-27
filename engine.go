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

	"github.com/shopspring/decimal"
)

// MatchingEngine manages multiple order books for different markets.
type MatchingEngine struct {
	isShutdown    atomic.Bool
	orderbooks    sync.Map
	publishTrader PublishLog
}

// NewMatchingEngine creates a new matching engine instance.
func NewMatchingEngine(publishTrader PublishLog) *MatchingEngine {
	return &MatchingEngine{
		publishTrader: publishTrader,
	}
}

// AddOrder adds an order to the appropriate order book based on the market ID.
// Returns ErrShutdown if the engine is shutting down.
func (engine *MatchingEngine) AddOrder(ctx context.Context, cmd *PlaceOrderCommand) error {
	if engine.isShutdown.Load() {
		return ErrShutdown
	}
	orderbook := engine.OrderBook(cmd.MarketID)
	if orderbook == nil {
		return ErrShutdown
	}
	return orderbook.AddOrder(ctx, cmd)
}

// AmendOrder modifies an existing order in the appropriate order book.
// Returns ErrShutdown if the engine is shutting down.
func (engine *MatchingEngine) AmendOrder(ctx context.Context, marketID string, orderID string, newPrice decimal.Decimal, newSize decimal.Decimal) error {
	if engine.isShutdown.Load() {
		return ErrShutdown
	}
	orderbook := engine.OrderBook(marketID)
	if orderbook == nil {
		return ErrShutdown
	}
	return orderbook.AmendOrder(ctx, orderID, newPrice, newSize)
}

// CancelOrder cancels an order in the appropriate order book.
// Returns ErrShutdown if the engine is shutting down.
func (engine *MatchingEngine) CancelOrder(ctx context.Context, marketID string, orderID string) error {
	if engine.isShutdown.Load() {
		return ErrShutdown
	}
	orderbook := engine.OrderBook(marketID)
	if orderbook == nil {
		return ErrShutdown
	}
	return orderbook.CancelOrder(ctx, orderID)
}

// OrderBook retrieves the order book for a specific market ID, creating it if it doesn't exist.
// Returns nil if the engine is shutting down.
func (engine *MatchingEngine) OrderBook(marketID string) *OrderBook {
	// Do not create new order books during shutdown
	if engine.isShutdown.Load() {
		book, found := engine.orderbooks.Load(marketID)
		if !found {
			return nil
		}
		orderbook, _ := book.(*OrderBook)
		return orderbook
	}

	book, found := engine.orderbooks.Load(marketID)
	if !found {
		newbook := NewOrderBook(marketID, engine.publishTrader)
		var loaded bool
		book, loaded = engine.orderbooks.LoadOrStore(marketID, newbook)
		if !loaded {
			go func() {
				_ = newbook.Start()
			}()
		}
	}

	orderbook, _ := book.(*OrderBook)
	return orderbook
}

// Shutdown gracefully shuts down all order books in the engine.
// It blocks until all order books have completed their shutdown or the context is cancelled.
// Returns nil if all order books shut down successfully, or an aggregated error otherwise.
func (engine *MatchingEngine) Shutdown(ctx context.Context) error {
	// Set shutdown flag to prevent new orders and new market creation
	engine.isShutdown.Store(true)

	var wg sync.WaitGroup
	var errs []error
	var errMu sync.Mutex

	// Shutdown all order books in parallel
	engine.orderbooks.Range(func(key, value any) bool {
		wg.Add(1)
		go func(marketID string, book *OrderBook) {
			defer wg.Done()
			if err := book.Shutdown(ctx); err != nil {
				errMu.Lock()
				errs = append(errs, err)
				errMu.Unlock()
			}
		}(key.(string), value.(*OrderBook))
		return true
	})

	// Wait for all order books to complete shutdown
	wg.Wait()

	// Return aggregated errors if any
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// snapshotResult wraps a snapshot result with potential error
type snapshotResult struct {
	snap *OrderBookSnapshot
	err  error
}

// takeSnapshot orchestrates the snapshot process across all order books.
// It returns a channel that streams snapshot results (including errors).
func (e *MatchingEngine) takeSnapshot() chan snapshotResult {
	ch := make(chan snapshotResult)

	go func() {
		defer close(ch)
		var wg sync.WaitGroup

		e.orderbooks.Range(func(key, value any) bool {
			book := value.(*OrderBook)
			wg.Add(1)
			go func(b *OrderBook, marketID string) {
				defer wg.Done()
				snap, err := b.TakeSnapshot()
				if err != nil {
					ch <- snapshotResult{snap: nil, err: errors.New("snapshot failed for market " + marketID + ": " + err.Error())}
					return
				}
				if snap != nil {
					ch <- snapshotResult{snap: snap, err: nil}
				}
			}(book, key.(string))
			return true
		})

		wg.Wait()
	}()

	return ch
}

// TakeSnapshot captures a consistent snapshot of all order books and writes them to the specified directory.
// It generates two files: `snapshot.bin` (binary data) and `metadata.json` (metadata).
// Returns the metadata object or an error.
func (e *MatchingEngine) TakeSnapshot(outputDir string) (*SnapshotMetadata, error) {
	// Use a temporary directory for atomic writes
	tmpDir := outputDir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, err
	}

	snapChan := e.takeSnapshot()

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

	// Stream write
	for result := range snapChan {
		// Check for snapshot errors
		if result.err != nil {
			snapshotErrors = append(snapshotErrors, result.err)
			continue
		}

		snap := result.snap

		// Serialize Market Data
		data, err := json.Marshal(snap)
		if err != nil {
			binFile.Close()
			return nil, err // Should probably handle partial failure better, but fail-fast for now
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

	// Calculate full file checksum (Issue 2)
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

	// Atomic rename: remove old dir and rename temp to final (Issue 3)
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

	// 5. Restore OrderBooks
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

		// Create and restore OrderBook
		book := NewOrderBook(segment.MarketID, e.publishTrader)
		book.Restore(&snap)

		// Add to engine map and start
		e.orderbooks.Store(segment.MarketID, book)
		go func(b *OrderBook) {
			_ = b.Start()
		}(book)
	}

	return &meta, nil
}
