package match

import (
	"sync"
	"time"

	"github.com/quagmt/udecimal"
)

// OrderBookLog represents an event in the order book.
// SequenceID is a globally increasing ID for every event, used for ordering,
// deduplication, and rebuild synchronization in downstream systems.
// Use LogType to determine if the event affects order book state:
// - Open, Match, Cancel, Amend: affect order book state
// - Reject: does not affect order book state
type OrderBookLog struct {
	SequenceID   uint64           `json:"seq_id"`
	TradeID      uint64           `json:"trade_id,omitempty"` // Sequential trade ID, only set for Match events
	Type         LogType          `json:"type"`               // Event type: open, match, cancel, amend, reject
	MarketID     string           `json:"market_id"`
	Side         Side             `json:"side"`
	Price        udecimal.Decimal `json:"price"`
	Size         udecimal.Decimal `json:"size"`
	Amount       udecimal.Decimal `json:"amount,omitempty"` // Price * Size, only set for Match events
	OldPrice     udecimal.Decimal `json:"old_price,omitempty"`
	OldSize      udecimal.Decimal `json:"old_size,omitempty"`
	OrderID      string           `json:"order_id"`
	UserID       int64            `json:"user_id"`
	OrderType    OrderType        `json:"order_type,omitempty"` // Order type: limit, market, ioc, fok
	MakerOrderID string           `json:"maker_order_id,omitempty"`
	MakerUserID  int64            `json:"maker_user_id,omitempty"`
	RejectReason RejectReason     `json:"reject_reason,omitempty"` // Reason for rejection, only set for Reject events
	CreatedAt    time.Time        `json:"created_at"`
}

var bookLogPool = sync.Pool{
	New: func() any {
		return new(OrderBookLog)
	},
}

func acquireBookLog() *OrderBookLog {
	return bookLogPool.Get().(*OrderBookLog)
}

func releaseBookLog(log *OrderBookLog) {
	// Reset structure to zero values.
	// For decimal.Decimal, the zero value (nil internal pointer) represents 0, which is valid.
	*log = OrderBookLog{}
	bookLogPool.Put(log)
}

// logSlicePool pools the *[]*OrderBookLog pointers to reduce allocations.
var logSlicePool = sync.Pool{
	New: func() any {
		s := make([]*OrderBookLog, 0, 8)
		return &s
	},
}

func acquireLogSlice() *[]*OrderBookLog {
	return logSlicePool.Get().(*[]*OrderBookLog)
}

func releaseLogSlice(ps *[]*OrderBookLog) {
	s := *ps
	// Clear the slice but keep capacity for reuse.
	*ps = s[:0]
	logSlicePool.Put(ps)
}

func NewOpenLog(seqID uint64, marketID string, orderID string, userID int64, side Side, price, size udecimal.Decimal, orderType OrderType) *OrderBookLog {
	log := acquireBookLog()
	log.SequenceID = seqID
	log.Type = LogTypeOpen
	log.MarketID = marketID
	log.Side = side
	log.Price = price
	log.Size = size
	log.OrderID = orderID
	log.UserID = userID
	log.OrderType = orderType
	log.CreatedAt = time.Now().UTC()
	return log
}

func NewMatchLog(seqID uint64, tradeID uint64, marketID string, takerID string, takerUserID int64, takerSide Side, takerType OrderType, makerID string, makerUserID int64, price udecimal.Decimal, size udecimal.Decimal) *OrderBookLog {
	log := acquireBookLog()
	log.SequenceID = seqID
	log.TradeID = tradeID
	log.Type = LogTypeMatch
	log.MarketID = marketID
	log.Side = takerSide
	log.Price = price
	log.Size = size
	log.Amount = price.Mul(size)
	log.OrderID = takerID
	log.UserID = takerUserID
	log.OrderType = takerType
	log.MakerOrderID = makerID
	log.MakerUserID = makerUserID
	log.CreatedAt = time.Now().UTC()
	return log
}

func NewCancelLog(seqID uint64, marketID string, orderID string, userID int64, side Side, price, size udecimal.Decimal, orderType OrderType) *OrderBookLog {
	log := acquireBookLog()
	log.SequenceID = seqID
	log.Type = LogTypeCancel
	log.MarketID = marketID
	log.Side = side
	log.Price = price
	log.Size = size
	log.OrderID = orderID
	log.UserID = userID
	log.OrderType = orderType
	log.CreatedAt = time.Now().UTC()
	return log
}

func NewAmendLog(seqID uint64, marketID string, orderID string, userID int64, side Side, price, size udecimal.Decimal, oldPrice udecimal.Decimal, oldSize udecimal.Decimal, orderType OrderType) *OrderBookLog {
	log := acquireBookLog()
	log.SequenceID = seqID
	log.Type = LogTypeAmend
	log.MarketID = marketID
	log.Side = side
	log.Price = price
	log.Size = size
	log.OldPrice = oldPrice
	log.OldSize = oldSize
	log.OrderID = orderID
	log.UserID = userID
	log.OrderType = orderType
	log.CreatedAt = time.Now().UTC()
	return log
}

func NewRejectLog(seqID uint64, marketID string, orderID string, userID int64, reason RejectReason) *OrderBookLog {
	log := acquireBookLog()
	log.SequenceID = seqID
	log.Type = LogTypeReject
	log.MarketID = marketID
	log.OrderID = orderID
	log.UserID = userID
	log.RejectReason = reason
	log.CreatedAt = time.Now().UTC()
	return log
}
