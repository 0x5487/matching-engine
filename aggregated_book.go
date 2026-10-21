package match

import (
	"fmt"
	"sync"

	"github.com/igrmk/treemap/v2"
	"github.com/quagmt/udecimal"

	"github.com/0x5487/matching-engine/protocol"
)

// RebuildFunc is the callback type for fetching a snapshot during rebuild.
// Implementations should fetch the current order book snapshot from external sources
// (e.g., Redis, Database, API) and return it for the AggregatedBook to apply.
//
// A snapshot is a protocol.DepthDelta that lists every level of the book: Seq is
// the version the snapshot was taken at, and the levels are the same type ApplyDelta
// takes.
type RebuildFunc func() (*protocol.DepthDelta, error)

// AggregatedBook maintains a simplified view of the order book,
// tracking only price levels and their aggregated sizes (depth).
// It is designed for downstream services that follow an OrderBook through the
// DepthDelta feed produced by DepthDeltaBuilder and applied with ApplyDelta.
//
// One lock guards the version and the levels together, so a reader never sees a
// version that the levels do not have. The levels are treemaps compared by value,
// so 100 and 100.00 are one level.
type AggregatedBook struct {
	mu  sync.RWMutex
	seq uint64 // Depth version the book has reached; guarded by mu
	ask *treemap.TreeMap[udecimal.Decimal, udecimal.Decimal]
	bid *treemap.TreeMap[udecimal.Decimal, udecimal.Decimal]

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
	ab.mu.RLock()
	defer ab.mu.RUnlock()
	return ab.seq
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

// ApplySnapshot resets the aggregated book state from a snapshot: the levels it
// lists and the version it carries. A snapshot is checked as a whole before anything
// changes. A negative size, or a price that appears twice on one side, returns
// ErrInvalidDepthDelta and leaves the book as it was. A zero size lists no level.
func (ab *AggregatedBook) ApplySnapshot(snapshot *protocol.DepthDelta) error {
	if snapshot == nil {
		return ErrNilSnapshot
	}
	if err := checkDeltaSides(snapshot); err != nil {
		return err
	}

	ab.mu.Lock()
	defer ab.mu.Unlock()

	ab.ask.Clear()
	ab.bid.Clear()
	setDeltaLevels(ab.ask, snapshot.Asks)
	setDeltaLevels(ab.bid, snapshot.Bids)
	ab.seq = snapshot.Seq

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
			UpdateID: ab.seq,
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
		UpdateID: ab.seq,
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
	cur := ab.seq
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
	if d.Seq > snap.Seq+1 {
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
	ab.seq = d.Seq
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
	return checkDeltaSides(d)
}

// checkDeltaSides reports whether the levels of d are well formed: sizes are not
// negative, and each side is strictly ordered, which also rules out a price written
// two ways. It does not look at Seq, which a snapshot may carry as zero.
func checkDeltaSides(d *protocol.DepthDelta) error {
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
