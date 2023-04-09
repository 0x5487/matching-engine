package match

import (
	"sync"
)

type MatchingEngine struct {
	orderbooks sync.Map
}

func NewMatchingEngine() *MatchingEngine {
	return &MatchingEngine{}
}

func (engine *MatchingEngine) PlaceOrder(order *Order) error {
	orderbook := engine.orderBook(order.MarketID)
	return orderbook.PlaceOrder(order)
}

func (engine *MatchingEngine) CancelOrder(marketID string, orderID string) error {
	orderbook := engine.orderBook(marketID)
	return orderbook.CancelOrder(orderID)
}

func (engine *MatchingEngine) orderBook(marketID string) *OrderBook {
	book, ok := engine.orderbooks.Load(marketID)
	if !ok {
		newbook := NewOrderBook()
		book, _ = engine.orderbooks.LoadOrStore(marketID, newbook)
		go newbook.Start()
	}

	orderbook, _ := book.(*OrderBook)
	return orderbook
}
