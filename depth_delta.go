package match

import (
	"slices"

	"github.com/quagmt/udecimal"

	"github.com/0x5487/matching-engine/protocol"
)

// DepthDeltaBuilder produces the DepthDelta of the book logs it is given. It is
// the only producer of depth changes: which levels a log moves is decided by
// CalculateDepthChange, and the size each level reports is read from the book.
//
// Usage: Add the logs of one or more SDK calls, then, before the next call changes
// the book, call Flush to read the levels, and Reset to forget them. Flush reads
// the book at the moment it is called, so the sizes it reports are the sizes that
// hold after the logs, not the sizes they were produced at. A caller that runs
// several calls before one Flush gets one delta holding each touched level once,
// at its final size.
//
// Only logs that move visible depth are kept: a log whose CalculateDepthChange
// has a zero SizeDiff (a refusal, an admin or user event, a no-op amend, an amend
// that shrinks inside the hidden size) is ignored.
//
// A builder is not safe for concurrent use. Steady-state Add, Flush and Reset with
// a reused DepthDelta do not allocate.
type DepthDeltaBuilder struct {
	seq uint64 // version of the last depth-moving log added
	// Each level is keyed by its canonical price, so 100 and 100.00 are one level.
	// The value is the price as first written, which is what Flush reports.
	asks map[priceKey]udecimal.Decimal
	bids map[priceKey]udecimal.Decimal
}

// NewDepthDeltaBuilder returns a builder with no logs.
func NewDepthDeltaBuilder() *DepthDeltaBuilder {
	return &DepthDeltaBuilder{
		asks: make(map[priceKey]udecimal.Decimal),
		bids: make(map[priceKey]udecimal.Decimal),
	}
}

// Add records one book log. The log is copied out, not kept, so it may be released
// right after Add returns.
func (b *DepthDeltaBuilder) Add(log *OrderBookLog) {
	change := CalculateDepthChange(log)
	if change.SizeDiff.IsZero() {
		return
	}
	b.seq = log.SeqID
	key := priceKeyOf(change.Price)
	if change.Side == Buy {
		b.bids[key] = change.Price
	} else {
		b.asks[key] = change.Price
	}
}

// AddBatch records every log of a batch, in order.
func (b *DepthDeltaBuilder) AddBatch(logs []*OrderBookLog) {
	for _, log := range logs {
		b.Add(log)
	}
}

// Flush fills out with the levels recorded so far, each at its size in book now,
// asks ascending and bids descending, and with the version of the last depth log.
// It reports false, and leaves out empty, when no visible depth moved. Flush does
// not change the builder; call Reset once the delta is taken.
//
// out's slices are reused. A caller that keeps one DepthDelta across calls does
// not allocate once its slices have grown to the size of the largest delta.
func (b *DepthDeltaBuilder) Flush(book *OrderBook, out *protocol.DepthDelta) bool {
	out.Seq = 0
	out.Asks = out.Asks[:0]
	out.Bids = out.Bids[:0]
	if len(b.asks)+len(b.bids) == 0 {
		return false
	}
	out.Seq = b.seq
	out.Asks = levelsOf(out.Asks, b.asks, book, Sell)
	out.Bids = levelsOf(out.Bids, b.bids, book, Buy)
	return true
}

// Reset forgets every log recorded so far.
func (b *DepthDeltaBuilder) Reset() {
	clear(b.asks)
	clear(b.bids)
	b.seq = 0
}

// levelsOf appends one level per price in set, sized from book, and orders them:
// ascending for asks, descending for bids.
func levelsOf(
	dst []protocol.DepthLevel,
	set map[priceKey]udecimal.Decimal,
	book *OrderBook,
	side Side,
) []protocol.DepthLevel {
	for _, price := range set {
		dst = append(dst, protocol.DepthLevel{Price: price, Size: book.LevelSize(side, price)})
	}
	if side == Sell {
		slices.SortFunc(dst, func(a, b protocol.DepthLevel) int { return a.Price.Cmp(b.Price) })
	} else {
		slices.SortFunc(dst, func(a, b protocol.DepthLevel) int { return b.Price.Cmp(a.Price) })
	}
	return dst
}
