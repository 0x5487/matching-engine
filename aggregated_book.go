package match

import (
	"fmt"
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
// It is designed for downstream services that follow an OrderBook through the
// DepthDelta feed produced by DepthDeltaBuilder and applied with ApplyDelta.
type AggregatedBook struct {
	mu    sync.RWMutex
	seqID atomic.Uint64 // Last processed SequenceID for gap detection and deduplication
	ask   *treemap.TreeMap[udecimal.Decimal, udecimal.Decimal]
	bid   *treemap.TreeMap[udecimal.Decimal, udecimal.Decimal]

	// OnRebuild is called when a rebuild is needed (e.g., sequence gap detected).
	// The callback should return a snapshot from which the book will be rebuilt.
	// This must be set before calling Rebuild(), or before ApplyDelta meets a gap.
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

// SequenceID returns the depth version the book has reached.
// Used for synchronization and gap detection when a delta is applied.
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

// ApplyDelta applies a DepthDelta produced by an OrderBook. The delta's Seq is
// compared with the version the book has reached, cur:
//
//   - Seq == cur+1: each level is set to its size, zero removes the level, and the
//     version becomes Seq.
//   - Seq <= cur: the delta was applied already. Nothing changes and nil is returned.
//   - Seq > cur+1: a version was missed. Without OnRebuild, ErrSequenceGap is
//     returned. With it, the snapshot is fetched and applied only if it reaches
//     Seq-1 or further, and the delta is then applied or skipped as above. A
//     snapshot that still leaves a gap is not applied, and ErrSequenceGap is
//     returned with the book unchanged.
//
// A malformed delta (nil, Seq zero, a negative size, a side whose levels are not
// strictly ordered, which also rules out a repeated price) returns
// ErrInvalidDepthDelta before anything is read or changed. Every error leaves the
// book unchanged.
//
// ApplyDelta is for one writer at a time, as the feed it follows is ordered.
func (ab *AggregatedBook) ApplyDelta(d *protocol.DepthDelta) error {
	if err := checkDepthDelta(d); err != nil {
		return err
	}

	ab.mu.Lock()
	cur := ab.seqID.Load()
	switch {
	case d.Seq <= cur:
		ab.mu.Unlock()
		return nil
	case d.Seq == cur+1:
		applyDeltaLevels(ab, d)
		ab.mu.Unlock()
		return nil
	}
	ab.mu.Unlock()

	return ab.applyAcrossGap(d)
}

// applyAcrossGap handles a delta that starts past the version after the book's.
// The snapshot is taken first and applied only if it connects the delta, so a
// snapshot that does not is never half-applied.
func (ab *AggregatedBook) applyAcrossGap(d *protocol.DepthDelta) error {
	if ab.OnRebuild == nil {
		return ErrSequenceGap
	}
	snap, err := ab.OnRebuild()
	if err != nil {
		return err
	}
	if snap == nil {
		return ErrNilSnapshot
	}
	if d.Seq > snap.SequenceID+1 {
		return ErrSequenceGap
	}
	if err := ab.ApplySnapshot(snap); err != nil {
		return err
	}
	// The snapshot now reaches d.Seq-1 or further, so this is a resend or the
	// next version and cannot gap again.
	return ab.ApplyDelta(d)
}

// applyDeltaLevels sets the levels of d and the version. The caller holds the lock
// and has checked that d.Seq is the version after the book's.
func applyDeltaLevels(ab *AggregatedBook, d *protocol.DepthDelta) {
	setDeltaLevels(ab.ask, d.Asks)
	setDeltaLevels(ab.bid, d.Bids)
	ab.seqID.Store(d.Seq)
}

func setDeltaLevels(tree *treemap.TreeMap[udecimal.Decimal, udecimal.Decimal], levels []protocol.DepthLevel) {
	for _, l := range levels {
		if l.Size.IsZero() {
			tree.Del(l.Price)
		} else {
			tree.Set(l.Price, l.Size)
		}
	}
}

// checkDepthDelta reports whether d is well formed. It changes nothing.
func checkDepthDelta(d *protocol.DepthDelta) error {
	if d == nil {
		return fmt.Errorf("%w: nil delta", ErrInvalidDepthDelta)
	}
	if d.Seq == 0 {
		return fmt.Errorf("%w: seq is zero", ErrInvalidDepthDelta)
	}
	if err := checkDeltaSide(d.Asks, "asks", func(a, b udecimal.Decimal) bool { return a.LessThan(b) }); err != nil {
		return err
	}
	return checkDeltaSide(d.Bids, "bids", func(a, b udecimal.Decimal) bool { return a.GreaterThan(b) })
}

// checkDeltaSide requires every size to be non-negative and the prices to be in
// strict order: before reports whether a comes before b, so equal prices fail too.
func checkDeltaSide(levels []protocol.DepthLevel, name string, before func(a, b udecimal.Decimal) bool) error {
	for i, l := range levels {
		if l.Size.LessThan(udecimal.Zero) {
			return fmt.Errorf("%w: %s level %s has negative size %s", ErrInvalidDepthDelta, name, l.Price, l.Size)
		}
		if i > 0 && !before(levels[i-1].Price, l.Price) {
			return fmt.Errorf("%w: %s not strictly ordered at %s", ErrInvalidDepthDelta, name, l.Price)
		}
	}
	return nil
}
