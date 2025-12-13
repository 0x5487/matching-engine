package match

import "sync"

// PublishTrader is an interface for publishing order book logs (trades, opens, cancels).
type PublishTrader interface {
	Publish(...*BookLog)
}

// MemoryPublishTrader stores logs in memory, useful for testing.
type MemoryPublishTrader struct {
	mu     sync.RWMutex
	Trades []*BookLog
}

// NewMemoryPublishTrader creates a new MemoryPublishTrader.
func NewMemoryPublishTrader() *MemoryPublishTrader {
	return &MemoryPublishTrader{
		Trades: make([]*BookLog, 0),
	}
}

// Publish appends logs to the in-memory slice.
func (m *MemoryPublishTrader) Publish(trades ...*BookLog) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Trades = append(m.Trades, trades...)
}

// Count returns the number of logs stored.
func (m *MemoryPublishTrader) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.Trades)
}

// Get returns the log at the specified index.
func (m *MemoryPublishTrader) Get(index int) *BookLog {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.Trades[index]
}

// DiscardPublishTrader discards all logs, useful for benchmarking.
type DiscardPublishTrader struct {
}

// NewDiscardPublishTrader creates a new DiscardPublishTrader.
func NewDiscardPublishTrader() *DiscardPublishTrader {
	return &DiscardPublishTrader{}
}

// Publish does nothing.
func (p *DiscardPublishTrader) Publish(trades ...*BookLog) {

}
