package match

import (
	"sync"

	"github.com/quagmt/udecimal"

	"github.com/0x5487/matching-engine/protocol"
)

// LogBatch is the logs one call produced, in the order the book produced them.
//
// A book returns its own batch from PlaceOrder, CancelOrder and AmendOrder. The
// batch and the logs in it are valid until the next call on the same book, which
// reclaims them when it starts: those logs go back to the pool and the batch is
// emptied. A caller that keeps a log, or the Logs slice, past that next call reads
// logs that now belong to another call. The same rule covers a Publisher (see
// Publisher): copy inside Publish.
type LogBatch struct {
	Logs []*OrderBookLog
}

// reclaim returns the logs the batch holds to the log pool and empties the batch,
// keeping its slice for the next call. Each book calls it when a call starts, and
// the engine calls it before it reuses its own batch.
func (b *LogBatch) reclaim() {
	for _, log := range b.Logs {
		releaseBookLog(log)
	}
	b.Logs = b.Logs[:0]
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
	// SeqID is 0 for LogTypeUser: a user event moves no depth and has no depth
	// version. Its upstream command sequence is in CmdSeqID.
	SeqID uint64 `json:"seq_id"`
	// CmdSeqID is the upstream message queue's command sequence. Only a user event
	// carries it, from MatchingEngine.handleUserEvent; every other log carries 0.
	CmdSeqID  uint64           `json:"cmd_seq_id,omitempty"`
	CommandID string           `json:"command_id"`
	EngineID  string           `json:"engine_id"`
	TradeID   uint64           `json:"trade_id,omitempty"`
	Type      protocol.LogType `json:"type"`
	MarketID  string           `json:"market_id"`
	Side      Side             `json:"side"`
	Price     udecimal.Decimal `json:"price"`
	Amount    udecimal.Decimal `json:"amount"`
	OrderID   string           `json:"order_id"`
	UserID    uint64           `json:"user_id"`
	OrderType OrderType        `json:"order_type"`
	OldPrice  udecimal.Decimal `json:"old_price"`
	// The quantity fields below each have one meaning. A log that does not use a
	// field leaves it zero, and the table in the README says which log fills which.
	//
	// MatchSize is the base quantity a Match log trades. A match consumes only the
	// visible part of the maker, so it is also the visible size the maker gives up.
	MatchSize udecimal.Decimal `json:"match_size,omitzero"`
	// Remaining is the order's total remaining base size, visible plus hidden. An
	// Open carries it after the order rests, an Amend its new total, and a Cancel
	// the total the cancel removes. A Reject carries the base quantity the order did
	// not get.
	Remaining udecimal.Decimal `json:"remaining,omitzero"`
	// OldRemaining is an Amend's total before the amend. Zero on every other log.
	OldRemaining udecimal.Decimal `json:"old_remaining,omitzero"`
	// VisibleAdded is the visible size an Open puts on the book: the visible part
	// of Remaining. The hidden part of an iceberg is never depth.
	VisibleAdded udecimal.Decimal `json:"visible_added,omitzero"`
	// VisibleRemoved is the visible (depth) size a Cancel or an Amend takes off the
	// book.
	VisibleRemoved udecimal.Decimal `json:"visible_removed,omitzero"`
	// QuoteRemaining is the quote a Reject of a quote-sized market order leaves
	// unspent. Zero on every other log, including a base-sized reject.
	QuoteRemaining udecimal.Decimal `json:"quote_remaining,omitzero"`
	MakerOrderID   string           `json:"maker_order_id,omitempty"`
	MakerUserID    uint64           `json:"maker_user_id,omitempty"`
	// MakerOrder is the maker of a Match log as the trade left it: a copy of its routing and of its size, filled and
	// hidden after this trade and before any replenish it triggers. Zero on every other log.
	MakerOrder   Order                 `json:"maker_order,omitzero"`
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
//
// Ownership: the logs passed to Publish belong to the book or engine that produced
// them. They stay valid until that book's next call (or the engine's next event),
// which reclaims them, so an implementation that needs the logs beyond the call -
// to hand them to a channel, a goroutine, or a queue - must copy them before
// returning.
//
// An implementation cannot reclaim the logs itself: Publish receives the slice,
// not the *LogBatch it belongs to. A publisher that retains the slice without
// copying is a silent-corruption bug, not a slow path.
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
		// The book reclaims these logs at its next call, which recycles them. Copy
		// so what we keep stays valid.
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

// NewOpenLog creates a new OrderBookLog for an open order event. visible is the
// visible size the order puts on the book and remaining its total, visible plus
// hidden.
//
//nolint:revive // argument-limit
func NewOpenLog(
	seqID uint64,
	commandID, engineID, marketID string,
	orderID string,
	userID uint64,
	side Side,
	price, visible, remaining udecimal.Decimal,
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
	log.VisibleAdded = visible
	log.Remaining = remaining
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
	matchSize udecimal.Decimal,
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
	log.MatchSize = matchSize
	log.Amount = price.Mul(matchSize)
	log.OrderID = takerID
	log.UserID = takerUserID
	log.OrderType = takerType
	log.MakerOrderID = makerID
	log.MakerUserID = makerUserID
	log.Timestamp = timestamp
	return log
}

// NewCancelLog creates a new OrderBookLog for an order cancellation event. remaining
// is the order's total before the cancel. The caller sets VisibleRemoved.
//
//nolint:revive // argument-limit
func NewCancelLog(
	seqID uint64,
	commandID, engineID, marketID string,
	orderID string,
	userID uint64,
	side Side,
	price, remaining udecimal.Decimal,
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
	log.Remaining = remaining
	log.OrderID = orderID
	log.UserID = userID
	log.OrderType = orderType
	log.Timestamp = timestamp
	return log
}

// NewAmendLog creates a new OrderBookLog for an order amendment event. remaining is
// the order's total after the amend, oldRemaining its total before. The caller sets
// VisibleRemoved.
//
//nolint:revive // argument-limit
func NewAmendLog(
	seqID uint64,
	commandID, engineID, marketID string,
	orderID string,
	userID uint64,
	side Side,
	price, remaining udecimal.Decimal,
	oldPrice udecimal.Decimal,
	oldRemaining udecimal.Decimal,
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
	log.Remaining = remaining
	log.OldPrice = oldPrice
	log.OldRemaining = oldRemaining
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

// NewUserEventLog creates a new OrderBookLog for a generic user event. cmdSeqID is
// the upstream command sequence; the event's SeqID stays 0.
//
//nolint:revive // argument-limit
func NewUserEventLog(
	cmdSeqID uint64,
	commandID, engineID string,
	userID uint64,
	eventType string,
	key string,
	data []byte,
	timestamp int64,
) *OrderBookLog {
	log := acquireBookLog()
	log.CmdSeqID = cmdSeqID
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
