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
		// Scenario 1: Priority Lost (Price changed OR Size increased)
		// Logic: The visible part is removed from OldPrice. The new order is then
		// handled by a subsequent protocol.LogTypeOpen or protocol.LogTypeMatch,
		// which adds whatever is visible at the new place.
		if !log.OldPrice.Equal(log.Price) || log.Size.GreaterThan(log.OldSize) {
			return DepthChange{
				Side:     log.Side,
				Price:    log.OldPrice,
				SizeDiff: log.VisibleRemoved.Neg(),
			}
		}

		// Scenario 2: Priority Kept (Price same AND Size not increased)
		// Logic: Update in-place. Only the visible size the decrease reached is removed;
		// a decrease that stays inside the hidden size removes nothing.
		return DepthChange{
			Side:     log.Side,
			Price:    log.Price,
			SizeDiff: log.VisibleRemoved.Neg(),
		}
	case protocol.LogTypeReject, protocol.LogTypeUser, protocol.LogTypeAdmin:
		// Rejected orders never entered the book, so no depth change.
		// User and admin events interact with the log stream but do not affect order book depth.
		return DepthChange{}
	}

	return DepthChange{}
}
