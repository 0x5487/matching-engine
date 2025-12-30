package match

import (
	"errors"
	"sync/atomic"

	"github.com/0x5487/matching-engine/protocol"
	"github.com/igrmk/treemap/v2"
	"github.com/quagmt/udecimal"
)

// Snapshot represents a point-in-time state of the order book.
// Used to initialize or reset the AggregatedBook during rebuild.
type Snapshot struct {
	SequenceID uint64                // The sequence ID at which this snapshot was taken
	Asks       []*protocol.DepthItem // Ask side depth levels, sorted by price ascending
	Bids       []*protocol.DepthItem // Bid side depth levels, sorted by price descending
}

// RebuildFunc is the callback type for fetching a snapshot during rebuild.
// Implementations should fetch the current order book snapshot from external sources
// (e.g., Redis, Database, API) and return it for the AggregatedBook to apply.
type RebuildFunc func() (*Snapshot, error)

// AggregatedBook maintains a simplified view of the order book,
// tracking only price levels and their aggregated sizes (depth).
// It is designed for downstream services that need to rebuild
// order book state from BookLog events received via message queue.
type AggregatedBook struct {
	seqID atomic.Uint64 // Last processed SequenceID for gap detection and deduplication
	ask   *treemap.TreeMap[udecimal.Decimal, udecimal.Decimal]
	bid   *treemap.TreeMap[udecimal.Decimal, udecimal.Decimal]

	// OnRebuild is called when a rebuild is needed (e.g., sequence gap detected).
	// The callback should return a snapshot from which the book will be rebuilt.
	// This must be set before calling Rebuild() or Replay() with gap detection.
	OnRebuild RebuildFunc
}

// NewAggregatedBook creates a new AggregatedBook instance with empty ask and bid sides.
func NewAggregatedBook() *AggregatedBook {
	return &AggregatedBook{
		ask: treemap.NewWithKeyCompare[udecimal.Decimal, udecimal.Decimal](func(a, b udecimal.Decimal) bool {
			return a.LessThan(b) // Ascending: lowest price first (best ask)
		}),
		bid: treemap.NewWithKeyCompare[udecimal.Decimal, udecimal.Decimal](func(a, b udecimal.Decimal) bool {
			return a.GreaterThan(b) // Descending: highest price first (best bid)
		}),
	}
}

// SequenceID returns the last processed sequence ID.
// Used for synchronization and gap detection during rebuild.
func (ab *AggregatedBook) SequenceID() uint64 {
	return ab.seqID.Load()
}

// Rebuild triggers a manual rebuild by calling the OnRebuild callback.
// Returns an error if OnRebuild is not set or if the callback fails.
func (ab *AggregatedBook) Rebuild() error {
	if ab.OnRebuild == nil {
		return errors.New("OnRebuild callback not set")
	}

	snapshot, err := ab.OnRebuild()
	if err != nil {
		return err
	}

	return ab.ApplySnapshot(snapshot)
}

// ApplySnapshot resets the aggregated book state from a snapshot.
// This clears all existing data and applies the snapshot's depth levels.
func (ab *AggregatedBook) ApplySnapshot(snapshot *Snapshot) error {
	if snapshot == nil {
		return errors.New("snapshot is nil")
	}

	// Clear existing data
	ab.ask.Clear()
	ab.bid.Clear()

	// Apply ask levels
	for _, level := range snapshot.Asks {
		p, _ := udecimal.Parse(level.Price)
		s, _ := udecimal.Parse(level.Size)
		ab.ask.Set(p, s)
	}

	// Apply bid levels
	for _, level := range snapshot.Bids {
		p, _ := udecimal.Parse(level.Price)
		s, _ := udecimal.Parse(level.Size)
		ab.bid.Set(p, s)
	}

	// Update sequence ID
	ab.seqID.Store(snapshot.SequenceID)

	return nil
}

// Replay applies a BookLog event to update the aggregated book state.
// Events with LogType == LogTypeReject do not affect book state but still update the sequence ID.
// Returns an error if a sequence gap is detected and rebuild fails.
func (ab *AggregatedBook) Replay(log *OrderBookLog) error {
	// TODO: Implement replay logic with:
	// 1. Gap detection: if log.SequenceID > expected, trigger Rebuild()
	// 2. Deduplication: skip if log.SequenceID <= current
	// 3. Apply state change based on LogType
	return nil
}

// Depth returns the aggregated size at a specific price level for the given side.
// Returns zero if the price level does not exist.
func (ab *AggregatedBook) Depth(side Side, price udecimal.Decimal) (udecimal.Decimal, error) {
	var tree *treemap.TreeMap[udecimal.Decimal, udecimal.Decimal]
	if side == Buy {
		tree = ab.bid
	} else {
		tree = ab.ask
	}

	size, found := tree.Get(price)
	if !found {
		return udecimal.Zero, nil
	}
	return size, nil
}
