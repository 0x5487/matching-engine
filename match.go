package match

import (
	"sync"
)

type MatchingEngine struct {
	orderbooks sync.Map
	tradeChan  chan *Trade
}

func NewMatchingEngine(tradeChan chan *Trade) *MatchingEngine {
	return &MatchingEngine{
		tradeChan: tradeChan,
	}
}

func (engine *MatchingEngine) PlaceOrder(order *Order) error {
	orderbook := engine.OrderBook(order.MarketID)
	return orderbook.AddOrder(order)
}

func (engine *MatchingEngine) CancelOrder(marketID string, orderID string) error {
	orderbook := engine.OrderBook(marketID)
	return orderbook.CancelOrder(orderID)
}

func (engine *MatchingEngine) OrderBook(marketID string) *OrderBook {
	book, found := engine.orderbooks.Load(marketID)
	if !found {
		newbook := NewOrderBook(engine.tradeChan)
		book, _ = engine.orderbooks.LoadOrStore(marketID, newbook)
		go newbook.Start()
	}

	orderbook, _ := book.(*OrderBook)
	return orderbook
}
