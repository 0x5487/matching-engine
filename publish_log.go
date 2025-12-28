package match

import "sync"

// PublishLog is an interface for publishing order book logs (trades, opens, cancels).
//
// IMPORTANT: Implementations must either:
//  1. Process logs synchronously before returning, OR
//  2. Clone the BookLog data before returning
//
// The caller recycles BookLog objects to a sync.Pool after Publish returns,
// so any asynchronous processing must work with cloned data.
type PublishLog interface {
	Publish(...*OrderBookLog)
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
func (m *MemoryPublishLog) Publish(trades ...*OrderBookLog) {
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
type DiscardPublishLog struct {
}

// NewDiscardPublishLog creates a new DiscardPublishLog.
func NewDiscardPublishLog() *DiscardPublishLog {
	return &DiscardPublishLog{}
}

// Publish does nothing.
func (p *DiscardPublishLog) Publish(trades ...*OrderBookLog) {

}
