package match

import (
	"sync"

	"github.com/quagmt/udecimal"

	"github.com/0x5487/matching-engine/protocol"
)

// LogBatch is a reusable container for a batch of order book logs.
type LogBatch struct {
	Logs []*OrderBookLog
}

var logBatchPool = sync.Pool{
	New: func() any {
		return &LogBatch{
			Logs: make([]*OrderBookLog, 0, 16), //nolint:mnd
		}
	},
}

// Release returns the LogBatch to the pool after clearing the logs.
func (b *LogBatch) Release() {
	b.Logs = b.Logs[:0]
	logBatchPool.Put(b)
}

// OrderBookLog represents a single event in the order book's history.
type OrderBookLog struct {
	SeqID        uint64                `json:"seq_id"`
	CommandID    string                `json:"command_id"`
	EngineID     string                `json:"engine_id"`
	TradeID      uint64                `json:"trade_id,omitempty"`
	Type         protocol.LogType      `json:"type"`
	MarketID     string                `json:"market_id"`
	Side         Side                  `json:"side"`
	Price        udecimal.Decimal      `json:"price"`
	Size         udecimal.Decimal      `json:"size"`
	Amount       udecimal.Decimal      `json:"amount"`
	OrderID      string                `json:"order_id"`
	UserID       uint64                `json:"user_id"`
	OrderType    OrderType             `json:"order_type"`
	OldPrice     udecimal.Decimal      `json:"old_price"`
	OldSize      udecimal.Decimal      `json:"old_size"`
	MakerOrderID string                `json:"maker_order_id,omitempty"`
	MakerUserID  uint64                `json:"maker_user_id,omitempty"`
	RejectReason protocol.RejectReason `json:"reject_reason,omitempty"`
	EventType    string                `json:"event_type,omitempty"` // For Admin/User events
	Data         []byte                `json:"data,omitempty"`       // For User events
	Timestamp    int64                 `json:"timestamp"`
}

var bookLogPool = sync.Pool{
	New: func() any {
		return &OrderBookLog{}
	},
}

func acquireBookLog() *OrderBookLog {
	return bookLogPool.Get().(*OrderBookLog) //nolint:revive,forcetypeassert
}

func releaseBookLog(log *OrderBookLog) {
	*log = OrderBookLog{}
	bookLogPool.Put(log)
}

// Publisher is the interface for receiving order book logs.
type Publisher interface {
	Publish(logs []*OrderBookLog)
}

// MemoryPublishLog is an in-memory publisher for testing and simple use cases.
type MemoryPublishLog struct {
	mu   sync.RWMutex
	logs []*OrderBookLog
}

// NewMemoryPublishLog creates a new MemoryPublishLog instance.
func NewMemoryPublishLog() *MemoryPublishLog {
	return &MemoryPublishLog{
		logs: make([]*OrderBookLog, 0),
	}
}

// Publish adds the logs to the in-memory buffer.
func (p *MemoryPublishLog) Publish(logs []*OrderBookLog) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, log := range logs {
		// We take a copy because logs are pooled
		copyLog := *log
		p.logs = append(p.logs, &copyLog)
	}
}

// Logs returns all captured logs.
func (p *MemoryPublishLog) Logs() []*OrderBookLog {
	p.mu.RLock()
	defer p.mu.RUnlock()
	res := make([]*OrderBookLog, len(p.logs))
	copy(res, p.logs)
	return res
}

// Count returns the number of captured logs.
func (p *MemoryPublishLog) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.logs)
}

// Get returns a log at the specific index.
func (p *MemoryPublishLog) Get(index int) *OrderBookLog {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.logs[index]
}

// DiscardPublishLog is a publisher that ignores all logs.
type DiscardPublishLog struct{}

// NewDiscardPublishLog creates a new DiscardPublishLog instance.
func NewDiscardPublishLog() *DiscardPublishLog {
	return &DiscardPublishLog{}
}

// Publish does nothing.
func (p *DiscardPublishLog) Publish(_ []*OrderBookLog) {
	// No-op
}

// --- Log Creation Helpers ---

// acquireLogBatch gets a LogBatch from the pool.
func acquireLogBatch() *LogBatch {
	return logBatchPool.Get().(*LogBatch) //nolint:revive,forcetypeassert
}

// NewOpenLog creates a new OrderBookLog for an open order event.
//
//nolint:revive // argument-limit
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
//nolint:revive // argument-limit
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
//nolint:revive // argument-limit
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
//nolint:revive // argument-limit
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
//nolint:revive // argument-limit
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
//nolint:revive // argument-limit
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
