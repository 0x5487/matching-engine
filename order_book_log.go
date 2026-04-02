package match

import (
	"sync"

	"github.com/quagmt/udecimal"

	"github.com/0x5487/matching-engine/protocol"
)

// PublishLog is an interface for publishing order book logs (trades, opens, cancels).
//
// IMPORTANT: Implementations must either:
//  1. Process logs synchronously before returning, OR
//  2. Clone the BookLog data before returning
//
// The caller recycles BookLog objects to a sync.Pool after Publish returns,
// so any asynchronous processing must work with cloned data.
type PublishLog interface {
	// Publish publishes order book logs. The slice comes from logSlicePool.
	Publish(logs []*OrderBookLog)
}

// MemoryPublishLog stores logs in memory, useful for testing.
type MemoryPublishLog struct {
	mu     sync.RWMutex
	Trades []*OrderBookLog
}

// NewMemoryPublishLog creates a new MemoryPublishLog.
func NewMemoryPublishLog() *MemoryPublishLog {
	return &MemoryPublishLog{
		Trades: make([]*OrderBookLog, 0),
	}
}

// Publish appends logs to the in-memory slice.
func (m *MemoryPublishLog) Publish(trades []*OrderBookLog) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, trade := range trades {
		cpy := new(OrderBookLog)
		*cpy = *trade
		m.Trades = append(m.Trades, cpy)
	}
}

// Count returns the number of logs stored.
func (m *MemoryPublishLog) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.Trades)
}

// Get returns the log at the specified index.
func (m *MemoryPublishLog) Get(index int) *OrderBookLog {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.Trades[index]
}

// Logs returns a copy of all logs stored.
func (m *MemoryPublishLog) Logs() []*OrderBookLog {
	m.mu.RLock()
	defer m.mu.RUnlock()

	logs := make([]*OrderBookLog, len(m.Trades))
	copy(logs, m.Trades)
	return logs
}

// DiscardPublishLog discards all logs, useful for benchmarking.
type DiscardPublishLog struct{}

// NewDiscardPublishLog creates a new DiscardPublishLog.
func NewDiscardPublishLog() *DiscardPublishLog {
	return &DiscardPublishLog{}
}

// Publish does nothing.
func (p *DiscardPublishLog) Publish(_ []*OrderBookLog) {
}

// OrderBookLog represents an event in the order book.
// SequenceID is a globally increasing ID for every event, used for ordering,
// deduplication, and rebuild synchronization in downstream systems.
// Use LogType to determine if the event affects order book state:
// - Open, Match, Cancel, Amend, Admin: affect order book state or lifecycle metadata
// - Reject: does not affect order book state.
type OrderBookLog struct {
	SeqID        uint64                `json:"seq_id"`
	CommandID    string                `json:"command_id,omitempty"`
	EngineID     string                `json:"engine_id,omitempty"`
	TradeID      uint64                `json:"trade_id,omitempty"` // Sequential trade ID, only set for Match events
	Type         protocol.LogType      `json:"type"`               // Event type: open, match, cancel, amend, reject
	MarketID     string                `json:"market_id"`
	Side         Side                  `json:"side"`
	Price        udecimal.Decimal      `json:"price"`
	Size         udecimal.Decimal      `json:"size"`
	Amount       udecimal.Decimal      `json:"amount,omitzero"` // Price * Size, only set for Match events
	OldPrice     udecimal.Decimal      `json:"old_price,omitzero"`
	OldSize      udecimal.Decimal      `json:"old_size,omitzero"`
	OrderID      string                `json:"order_id"`
	UserID       uint64                `json:"user_id"`
	OrderType    OrderType             `json:"order_type,omitempty"` // Order type: limit, market, ioc, fok
	MakerOrderID string                `json:"maker_order_id,omitempty"`
	MakerUserID  uint64                `json:"maker_user_id,omitempty"`
	RejectReason protocol.RejectReason `json:"reject_reason,omitempty"` // Reason for rejection, only set for Reject events
	EventType    string                `json:"event_type,omitempty"`    // User defined event type
	Data         []byte                `json:"data,omitempty"`          // Arbitrary data for user events
	Timestamp    int64                 `json:"timestamp"`               // Command timestamp for determinism
}

var bookLogPool = sync.Pool{
	New: func() any {
		return new(OrderBookLog)
	},
}

func acquireBookLog() *OrderBookLog {
	val := bookLogPool.Get()
	log, ok := val.(*OrderBookLog)
	if !ok {
		return new(OrderBookLog)
	}
	return log
}

func releaseBookLog(log *OrderBookLog) {
	// Reset structure to zero values.
	// For decimal.Decimal, the zero value (nil internal pointer) represents 0, which is valid.
	log.EventType = ""
	log.Data = nil
	*log = OrderBookLog{}
	bookLogPool.Put(log)
}

const defaultLogSliceCap = 8

// logSlicePool pools the *[]*OrderBookLog pointers to reduce allocations.
var logSlicePool = sync.Pool{
	New: func() any {
		s := make([]*OrderBookLog, 0, defaultLogSliceCap)
		return &s
	},
}

func acquireLogSlice() *[]*OrderBookLog {
	val := logSlicePool.Get()
	ps, ok := val.(*[]*OrderBookLog)
	if !ok {
		s := make([]*OrderBookLog, 0, defaultLogSliceCap)
		return &s
	}
	return ps
}

func releaseLogSlice(ps *[]*OrderBookLog) {
	s := *ps
	// Clear the slice but keep capacity for reuse.
	*ps = s[:0]
	logSlicePool.Put(ps)
}

// NewOpenLog creates a new OrderBookLog for an open order event.
//
//nolint:revive // argument-limit: many parameters needed for log creation
func NewOpenLog(
	seqID uint64,
	commandID, engineID, marketID string,
	orderID string,
	userID uint64,
	side Side,
	price, size udecimal.Decimal,
	orderType OrderType,
	timestamp int64,
) *OrderBookLog {
	log := acquireBookLog()
	log.SeqID = seqID
	log.CommandID = commandID
	log.EngineID = engineID
	log.Type = protocol.LogTypeOpen
	log.MarketID = marketID
	log.Side = side
	log.Price = price
	log.Size = size
	log.OrderID = orderID
	log.UserID = userID
	log.OrderType = orderType
	log.Timestamp = timestamp
	return log
}

// NewMatchLog creates a new OrderBookLog for a trade match event.
//
//nolint:revive // argument-limit: many parameters needed for log creation
func NewMatchLog(
	seqID uint64,
	commandID, engineID string,
	tradeID uint64,
	marketID string,
	takerID string,
	takerUserID uint64,
	takerSide Side,
	takerType OrderType,
	makerID string,
	makerUserID uint64,
	price udecimal.Decimal,
	size udecimal.Decimal,
	timestamp int64,
) *OrderBookLog {
	log := acquireBookLog()
	log.SeqID = seqID
	log.CommandID = commandID
	log.EngineID = engineID
	log.TradeID = tradeID
	log.Type = protocol.LogTypeMatch
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
	log.Timestamp = timestamp
	return log
}

// NewCancelLog creates a new OrderBookLog for an order cancellation event.
//
//nolint:revive // argument-limit: many parameters needed for log creation
func NewCancelLog(
	seqID uint64,
	commandID, engineID, marketID string,
	orderID string,
	userID uint64,
	side Side,
	price, size udecimal.Decimal,
	orderType OrderType,
	timestamp int64,
) *OrderBookLog {
	log := acquireBookLog()
	log.SeqID = seqID
	log.CommandID = commandID
	log.EngineID = engineID
	log.Type = protocol.LogTypeCancel
	log.MarketID = marketID
	log.Side = side
	log.Price = price
	log.Size = size
	log.OrderID = orderID
	log.UserID = userID
	log.OrderType = orderType
	log.Timestamp = timestamp
	return log
}

// NewAmendLog creates a new OrderBookLog for an order amendment event.
//
//nolint:revive // argument-limit: many parameters needed for log creation
func NewAmendLog(
	seqID uint64,
	commandID, engineID, marketID string,
	orderID string,
	userID uint64,
	side Side,
	price, size udecimal.Decimal,
	oldPrice udecimal.Decimal,
	oldSize udecimal.Decimal,
	orderType OrderType,
	timestamp int64,
) *OrderBookLog {
	log := acquireBookLog()
	log.SeqID = seqID
	log.CommandID = commandID
	log.EngineID = engineID
	log.Type = protocol.LogTypeAmend
	log.MarketID = marketID
	log.Side = side
	log.Price = price
	log.Size = size
	log.OldPrice = oldPrice
	log.OldSize = oldSize
	log.OrderID = orderID
	log.UserID = userID
	log.OrderType = orderType
	log.Timestamp = timestamp
	return log
}

// NewRejectLog creates a new OrderBookLog for an order rejection event.
//
//nolint:revive // argument-limit: many parameters needed for log creation
func NewRejectLog(
	seqID uint64,
	commandID, engineID, marketID string,
	orderID string,
	userID uint64,
	reason protocol.RejectReason,
	timestamp int64,
) *OrderBookLog {
	log := acquireBookLog()
	log.SeqID = seqID
	log.CommandID = commandID
	log.EngineID = engineID
	log.Type = protocol.LogTypeReject
	log.MarketID = marketID
	log.OrderID = orderID
	log.UserID = userID
	log.RejectReason = reason
	log.Timestamp = timestamp
	return log
}

// NewUserEventLog creates a new OrderBookLog for a generic user event.
//
//nolint:revive // argument-limit: many parameters needed for log creation
func NewUserEventLog(
	seqID uint64,
	commandID, engineID string,
	userID uint64,
	eventType string,
	key string,
	data []byte,
	timestamp int64,
) *OrderBookLog {
	log := acquireBookLog()
	log.SeqID = seqID
	log.CommandID = commandID
	log.EngineID = engineID
	log.Type = protocol.LogTypeUser
	log.UserID = userID
	log.EventType = eventType
	log.OrderID = key // We use OrderID field to store the Key for simpler indexing/lookup if needed
	log.Data = data
	log.Timestamp = timestamp
	return log
}

// NewAdminLog creates a new OrderBookLog for a successful management event.
func NewAdminLog(
	seqID uint64,
	commandID, engineID, marketID string,
	userID uint64,
	eventType string,
	timestamp int64,
) *OrderBookLog {
	log := acquireBookLog()
	log.SeqID = seqID
	log.CommandID = commandID
	log.EngineID = engineID
	log.Type = protocol.LogTypeAdmin
	log.MarketID = marketID
	log.UserID = userID
	log.EventType = eventType
	log.Timestamp = timestamp
	return log
}
