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

// Release returns the batch and every log it holds to their pools.
//
// A LogBatch owns its logs. Once Release returns, those logs have been zeroed
// and recycled, so any pointer into b.Logs kept across the call is dangling.
// That makes Release the single point where a caller hands logs back, which is
// the whole reason the log pool is reachable at all: a caller that only ever
// uses batch.Release() can pool its logs without knowing the pool exists.
//
// A Publisher that needs the logs after Release must copy them first.
func (b *LogBatch) Release() {
	for _, log := range b.Logs {
		releaseBookLog(log)
	}
	b.Logs = b.Logs[:0]
	logBatchPool.Put(b)
}

// releaseLogBatchContainer returns only the batch to the pool and leaves its
// logs untouched. It exists for the one case where ownership of the logs has
// already moved to another batch: Release would recycle logs the new owner is
// still holding pointers to.
func releaseLogBatchContainer(b *LogBatch) {
	b.Logs = b.Logs[:0]
	logBatchPool.Put(b)
}

// OrderBookLog represents a single event in the order book's history.
// OrderBookLog is one thing the order book did. Only depth-changing events
// carry a SeqID; see the field's own note.
type OrderBookLog struct {
	// SeqID is the book depth version this log belongs to.
	//
	// One SDK call that moves visible depth advances the version by exactly one,
	// and every depth log of that call carries the new value: an open, a match, a
	// cancel or an amend, including the replenish open and the re-match of a
	// priority-losing amend. Hidden iceberg size is not depth, so a call that moves
	// only hidden size does not advance it. Refusals and admin commands carry 0.
	//
	// A no-op amend, and a shrink that stays inside the hidden size, move no visible
	// depth; they carry the current version, which the call did not advance.
	//
	// The consequence worth knowing: a consumer that follows depth through
	// DepthDeltaBuilder and AggregatedBook.ApplyDelta sees one version per call, with
	// no version used up by a refusal, so a version jump is a lost update and not
	// the side effect of a rejected order.
	//
	// It is not a timestamp and not comparable across markets - the counter
	// belongs to one order book.
	//
	// One exception shares this field's name but not its meaning:
	// MatchingEngine.handleUserEvent builds a user event with the upstream
	// message queue's command sequence, so for LogTypeUser this is that
	// sequence rather than a depth version. A user event moves no depth, so
	// DepthDeltaBuilder ignores it.
	SeqID     uint64           `json:"seq_id"`
	CommandID string           `json:"command_id"`
	EngineID  string           `json:"engine_id"`
	TradeID   uint64           `json:"trade_id,omitempty"`
	Type      protocol.LogType `json:"type"`
	MarketID  string           `json:"market_id"`
	Side      Side             `json:"side"`
	Price     udecimal.Decimal `json:"price"`
	Size      udecimal.Decimal `json:"size"`
	Amount    udecimal.Decimal `json:"amount"`
	OrderID   string           `json:"order_id"`
	UserID    uint64           `json:"user_id"`
	OrderType OrderType        `json:"order_type"`
	OldPrice  udecimal.Decimal `json:"old_price"`
	OldSize   udecimal.Decimal `json:"old_size"`
	// VisibleRemoved is the visible (depth) size this log takes off the book.
	VisibleRemoved udecimal.Decimal      `json:"visible_removed,omitzero"`
	MakerOrderID   string                `json:"maker_order_id,omitempty"`
	MakerUserID    uint64                `json:"maker_user_id,omitempty"`
	RejectReason   protocol.RejectReason `json:"reject_reason,omitempty"`
	EventType      string                `json:"event_type,omitempty"` // For Admin/User events
	Data           []byte                `json:"data,omitempty"`       // For User events
	Timestamp      int64                 `json:"timestamp"`
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
//
// Ownership: the logs passed to Publish belong to the engine, and the engine
// calls LogBatch.Release as soon as Publish returns. Release zeroes them and
// returns them to a sync.Pool, so a log read afterwards reads recycled memory.
// An implementation that needs the logs beyond the call - to hand them to a
// channel, a goroutine, or a queue - must copy them before returning.
//
// An implementation cannot release the logs itself: Publish receives the slice,
// not the *LogBatch that Release is a method on. A publisher that retains the
// slice without copying is a silent-corruption bug, not a slow path.
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
		// The caller may Release the batch right after Publish returns, which
		// recycles the logs. Copy so what we keep stays valid.
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
