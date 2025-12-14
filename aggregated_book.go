package match

import (
	"sync/atomic"

	"github.com/igrmk/treemap/v2"
	"github.com/shopspring/decimal"
)

// AggregatedBook maintains a simplified view of the order book,
// tracking only price levels and their aggregated sizes (depth).
// It is designed for downstream services that need to rebuild
// order book state from BookLog events received via message queue.
type AggregatedBook struct {
	seqID atomic.Uint64 // Last processed SequenceID for gap detection and deduplication
	ask   *treemap.TreeMap[decimal.Decimal, decimal.Decimal]
	bid   *treemap.TreeMap[decimal.Decimal, decimal.Decimal]
}

// NewAggregatedBook creates a new AggregatedBook instance with empty ask and bid sides.
func NewAggregatedBook() *AggregatedBook {
	return &AggregatedBook{
		ask: treemap.NewWithKeyCompare[decimal.Decimal, decimal.Decimal](func(a, b decimal.Decimal) bool {
			return a.LessThan(b)
		}),
		bid: treemap.NewWithKeyCompare[decimal.Decimal, decimal.Decimal](func(a, b decimal.Decimal) bool {
			return a.LessThan(b)
		}),
	}
}

// SequenceID returns the last processed sequence ID.
// Used for synchronization and gap detection during rebuild.
func (ab *AggregatedBook) SequenceID() uint64 {
	return ab.seqID.Load()
}

// Replay applies a BookLog event to update the aggregated book state.
// Events with LogType == LogTypeReject do not affect book state but still update the sequence ID.
// Returns an error if the event cannot be applied (e.g., sequence gap detected).
func (ab *AggregatedBook) Replay(log *BookLog) error {
	return nil
}

// OnRebuild initializes or resets the aggregated book from a snapshot.
// This should be called before replaying events from the message queue.
func (ab *AggregatedBook) OnRebuild() error {
	return nil
}

// Depth returns the aggregated size at a specific price level for the given side.
// Returns zero if the price level does not exist.
func (ab *AggregatedBook) Depth(side Side, price decimal.Decimal) (decimal.Decimal, error) {
	return decimal.Zero, nil
}
