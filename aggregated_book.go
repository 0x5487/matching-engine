package match

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/igrmk/treemap/v2"
	"github.com/quagmt/udecimal"

	"github.com/0x5487/matching-engine/protocol"
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
	mu    sync.RWMutex
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
		ask: treemap.NewWithKeyCompare[udecimal.Decimal, udecimal.Decimal](
			func(a, b udecimal.Decimal) bool {
				return a.LessThan(b) // Ascending: lowest price first (best ask)
			},
		),
		bid: treemap.NewWithKeyCompare[udecimal.Decimal, udecimal.Decimal](
			func(a, b udecimal.Decimal) bool {
				return a.GreaterThan(b) // Descending: highest price first (best bid)
			},
		),
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
		return ErrOnRebuildNotSet
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
		return ErrNilSnapshot
	}

	ab.mu.Lock()
	defer ab.mu.Unlock()

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
	if log == nil {
		return errors.New("order book log is nil")
	}

	currentSeq := ab.seqID.Load()

	// 1. Deduplication: skip if log has already been applied or included in snapshot
	if currentSeq > 0 && log.SeqID <= currentSeq {
		return nil
	}

	// 2. Gap detection: if log.SeqID is ahead of expected sequence
	expectedSeq := currentSeq + 1
	if currentSeq == 0 && log.SeqID == 0 {
		expectedSeq = 0
	}
	if log.SeqID > expectedSeq {
		if ab.OnRebuild == nil {
			return ErrSequenceGap
		}
		if err := ab.Rebuild(); err != nil {
			return err
		}
		currentSeq = ab.seqID.Load()
		if log.SeqID <= currentSeq {
			return nil
		}
		if log.SeqID > currentSeq+1 {
			return ErrSequenceGap
		}
	}

	ab.mu.Lock()
	defer ab.mu.Unlock()

	// Double-check deduplication under lock
	if current := ab.seqID.Load(); current > 0 && log.SeqID <= current {
		return nil
	}

	ab.seqID.Store(log.SeqID)

	switch log.Type {
	case protocol.LogTypeOpen:
		tree := ab.ask
		if log.Side == Buy {
			tree = ab.bid
		}
		addDepth(tree, log.Price, log.Size)

	case protocol.LogTypeMatch:
		// log.Side is the taker's side; resting maker order is on the opposite side
		tree := ab.ask
		if log.Side == Sell {
			tree = ab.bid
		}
		deductDepth(tree, log.Price, log.Size)

	case protocol.LogTypeCancel:
		tree := ab.ask
		if log.Side == Buy {
			tree = ab.bid
		}
		deductDepth(tree, log.Price, log.Size)

	case protocol.LogTypeAmend:
		tree := ab.ask
		if log.Side == Buy {
			tree = ab.bid
		}
		if log.OldPrice.Equal(log.Price) {
			if log.Size.GreaterThan(log.OldSize) {
				addDepth(tree, log.Price, log.Size.Sub(log.OldSize))
			} else if log.OldSize.GreaterThan(log.Size) {
				deductDepth(tree, log.Price, log.OldSize.Sub(log.Size))
			}
		} else {
			deductDepth(tree, log.OldPrice, log.OldSize)
			addDepth(tree, log.Price, log.Size)
		}

	default:
		// LogTypeReject, LogTypeUser, LogTypeAdmin, and unknown events
		// do not affect order book depth state.
	}

	return nil
}

func addDepth(tree *treemap.TreeMap[udecimal.Decimal, udecimal.Decimal], price, size udecimal.Decimal) {
	if size.IsZero() {
		return
	}
	currentSize, found := tree.Get(price)
	if found {
		tree.Set(price, currentSize.Add(size))
	} else {
		tree.Set(price, size)
	}
}

func deductDepth(tree *treemap.TreeMap[udecimal.Decimal, udecimal.Decimal], price, size udecimal.Decimal) {
	if size.IsZero() {
		return
	}
	currentSize, found := tree.Get(price)
	if !found {
		return
	}
	if currentSize.LessThanOrEqual(size) {
		tree.Del(price)
	} else {
		tree.Set(price, currentSize.Sub(size))
	}
}

// Depth returns the aggregated size at a specific price level for the given side.
// Returns zero if the price level does not exist.
func (ab *AggregatedBook) Depth(side Side, price udecimal.Decimal) (udecimal.Decimal, error) {
	ab.mu.RLock()
	defer ab.mu.RUnlock()

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

// GetDepth returns the aggregated order book depth up to the specified limit.
func (ab *AggregatedBook) GetDepth(limit int) *protocol.GetDepthResponse {
	ab.mu.RLock()
	defer ab.mu.RUnlock()

	if limit <= 0 {
		return &protocol.GetDepthResponse{
			UpdateID: ab.seqID.Load(),
			Asks:     []*protocol.DepthItem{},
			Bids:     []*protocol.DepthItem{},
		}
	}

	asks := make([]*protocol.DepthItem, 0, limit)
	for it := ab.ask.Iterator(); it.Valid() && len(asks) < limit; it.Next() {
		asks = append(asks, &protocol.DepthItem{
			Price: it.Key().String(),
			Size:  it.Value().String(),
		})
	}

	bids := make([]*protocol.DepthItem, 0, limit)
	for it := ab.bid.Iterator(); it.Valid() && len(bids) < limit; it.Next() {
		bids = append(bids, &protocol.DepthItem{
			Price: it.Key().String(),
			Size:  it.Value().String(),
		})
	}

	return &protocol.GetDepthResponse{
		UpdateID: ab.seqID.Load(),
		Asks:     asks,
		Bids:     bids,
	}
}
