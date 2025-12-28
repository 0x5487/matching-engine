package match

import (
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// OrderBookLog represents an event in the order book.
// SequenceID is a globally increasing ID for every event, used for ordering,
// deduplication, and rebuild synchronization in downstream systems.
// Use LogType to determine if the event affects order book state:
// - Open, Match, Cancel, Amend: affect order book state
// - Reject: does not affect order book state
type OrderBookLog struct {
	SequenceID   uint64          `json:"seq_id"`
	TradeID      uint64          `json:"trade_id,omitempty"` // Sequential trade ID, only set for Match events
	Type         LogType         `json:"type"`               // Event type: open, match, cancel, amend, reject
	MarketID     string          `json:"market_id"`
	Side         Side            `json:"side"`
	Price        decimal.Decimal `json:"price"`
	Size         decimal.Decimal `json:"size"`
	Amount       decimal.Decimal `json:"amount,omitempty"` // Price * Size, only set for Match events
	OldPrice     decimal.Decimal `json:"old_price,omitempty"`
	OldSize      decimal.Decimal `json:"old_size,omitempty"`
	OrderID      string          `json:"order_id"`
	UserID       int64           `json:"user_id"`
	OrderType    OrderType       `json:"order_type,omitempty"` // Order type: limit, market, ioc, fok
	MakerOrderID string          `json:"maker_order_id,omitempty"`
	MakerUserID  int64           `json:"maker_user_id,omitempty"`
	RejectReason RejectReason    `json:"reject_reason,omitempty"` // Reason for rejection, only set for Reject events
	CreatedAt    time.Time       `json:"created_at"`
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

func NewOpenLog(seqID uint64, marketID string, order *Order) *OrderBookLog {
	log := acquireBookLog()
	log.SequenceID = seqID
	log.Type = LogTypeOpen
	log.MarketID = marketID
	log.Side = order.Side
	log.Price = order.Price
	log.Size = order.Size
	log.OrderID = order.ID
	log.UserID = order.UserID
	log.OrderType = order.Type
	log.CreatedAt = time.Now().UTC()
	return log
}

func NewMatchLog(seqID uint64, tradeID uint64, marketID string, takerOrder *Order, makerOrder *Order, price decimal.Decimal, size decimal.Decimal) *OrderBookLog {
	log := acquireBookLog()
	log.SequenceID = seqID
	log.TradeID = tradeID
	log.Type = LogTypeMatch
	log.MarketID = marketID
	log.Side = takerOrder.Side
	log.Price = price
	log.Size = size
	log.Amount = price.Mul(size)
	log.OrderID = takerOrder.ID
	log.UserID = takerOrder.UserID
	log.OrderType = takerOrder.Type
	log.MakerOrderID = makerOrder.ID
	log.MakerUserID = makerOrder.UserID
	log.CreatedAt = time.Now().UTC()
	return log
}

func NewCancelLog(seqID uint64, marketID string, order *Order) *OrderBookLog {
	log := acquireBookLog()
	log.SequenceID = seqID
	log.Type = LogTypeCancel
	log.MarketID = marketID
	log.Side = order.Side
	log.Price = order.Price
	log.Size = order.Size
	log.OrderID = order.ID
	log.UserID = order.UserID
	log.OrderType = order.Type
	log.CreatedAt = time.Now().UTC()
	return log
}

func NewAmendLog(seqID uint64, marketID string, order *Order, oldPrice decimal.Decimal, oldSize decimal.Decimal) *OrderBookLog {
	log := acquireBookLog()
	log.SequenceID = seqID
	log.Type = LogTypeAmend
	log.MarketID = marketID
	log.Side = order.Side
	log.Price = order.Price
	log.Size = order.Size
	log.OldPrice = oldPrice
	log.OldSize = oldSize
	log.OrderID = order.ID
	log.UserID = order.UserID
	log.OrderType = order.Type
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
