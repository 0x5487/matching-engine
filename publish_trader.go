package match

import "sync"

type PublishTrader interface {
	PublishTrades(...*Trade)
}

type MemoryPublishTrader struct {
	mu     sync.RWMutex
	Trades []*Trade
}

func NewMemoryPublishTrader() *MemoryPublishTrader {
	return &MemoryPublishTrader{
		Trades: make([]*Trade, 0),
	}
}

func (m *MemoryPublishTrader) PublishTrades(trades ...*Trade) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Trades = append(m.Trades, trades...)
}

func (m *MemoryPublishTrader) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.Trades)
}

func (m *MemoryPublishTrader) Get(index int) *Trade {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.Trades[index]
}

type DiscardPublishTrader struct {
}

func NewDiscardPublishTrader() *DiscardPublishTrader {
	return &DiscardPublishTrader{}
}

func (p *DiscardPublishTrader) PublishTrades(trades ...*Trade) {

}
