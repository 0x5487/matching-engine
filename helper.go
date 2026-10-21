package match

import (
	"github.com/0x5487/matching-engine/protocol"
)

// CalculateDepthChange is the only rule for how a book log moves depth. It returns
// the side, the price level and the signed visible size change that the log causes.
// DepthDeltaBuilder applies this and nothing else, so a consumer and the matching
// book cannot disagree about the same log.
//
// Size and OldSize are order totals (the visible part plus the hidden part) and
// are not read here. Depth counts only visible size, so the removal side uses
// VisibleRemoved. It is set on cancel and amend logs; see OrderBookLog.
//
// Note: For LogTypeMatch, the side returned is the Maker's side (opposite of the log's side).
func CalculateDepthChange(log *OrderBookLog) DepthChange {
	switch log.Type {
	case protocol.LogTypeOpen:
		return DepthChange{
			Side:     log.Side,
			Price:    log.Price,
			SizeDiff: log.Size,
		}
	case protocol.LogTypeCancel:
		return DepthChange{
			Side:     log.Side,
			Price:    log.Price,
			SizeDiff: log.VisibleRemoved.Neg(),
		}
	case protocol.LogTypeMatch:
		// Match reduces liquidity from the Maker side.
		// The log.Side is the Taker's side, so we update the opposite side.
		makerSide := Buy
		if log.Side == Buy {
			makerSide = Sell
		}
		return DepthChange{
			Side:     makerSide,
			Price:    log.Price,
			SizeDiff: log.Size.Neg(),
		}
	case protocol.LogTypeAmend:
		// One rule for every amend. The visible part the amend takes off the book is
		// removed from OldPrice: an amend that keeps its price and shrinks in place
		// has OldPrice equal to Price, and a priority-losing amend (a new price or a
		// growth) leaves the old level, while its re-match is logged separately as an
		// open or a match at the new price.
		return DepthChange{
			Side:     log.Side,
			Price:    log.OldPrice,
			SizeDiff: log.VisibleRemoved.Neg(),
		}
	case protocol.LogTypeReject, protocol.LogTypeUser, protocol.LogTypeAdmin:
		// Rejected orders never entered the book, so no depth change.
		// User and admin events interact with the log stream but do not affect order book depth.
		return DepthChange{}
	}

	return DepthChange{}
}
