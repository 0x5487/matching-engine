package match

import (
	"github.com/0x5487/matching-engine/protocol"
)

// CalculateDepthChange calculates the depth change based on the book log.
// It returns a DepthChange struct indicating which side and price level should be updated.
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
			SizeDiff: log.Size.Neg(),
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
		// Logic: The order is removed from the book. The new order will be handled by a subsequent protocol.LogTypeOpen or protocol.LogTypeMatch.
		// So we only need to remove the OldSize from OldPrice.
		if !log.OldPrice.Equal(log.Price) || log.Size.GreaterThan(log.OldSize) {
			return DepthChange{
				Side:     log.Side,
				Price:    log.OldPrice,
				SizeDiff: log.OldSize.Neg(),
			}
		}

		// Scenario 2: Priority Kept (Price same AND Size decreased)
		// Logic: Update in-place. The difference is (NewSize - OldSize).
		return DepthChange{
			Side:     log.Side,
			Price:    log.Price,
			SizeDiff: log.Size.Sub(log.OldSize),
		}
	case protocol.LogTypeReject, protocol.LogTypeUser:
		// Rejected orders never entered the book, so no depth change.
		// User events interact with the log stream but do not affect order book depth.
		return DepthChange{}
	}

	return DepthChange{}
}
