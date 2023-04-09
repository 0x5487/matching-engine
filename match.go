package match

import "sync"

type MatchingEngine struct {
	orderbooks sync.Map
}

func NewMatchingEngine() *MatchingEngine {
	return &MatchingEngine{}
}

func (engine *MatchingEngine) PlaceOrder(order *Order) ([]*Trade, error) {
	if len(order.Type) == 0 || len(order.ID) == 0 {
		return nil, ErrInvalidParam
	}

	orderbook := engine.orderBook(order.MarketID)
	return orderbook.PlaceOrder(order)
}

func (engine *MatchingEngine) CancelOrder(marketID string, orderID string) {
	orderbook := engine.orderBook(marketID)
	orderbook.CancelOrder(orderID)
}

func (engine *MatchingEngine) orderBook(marketID string) *OrderBook {
	book, _ := engine.orderbooks.LoadOrStore(marketID, NewOrderBook())

	orderbook, _ := book.(*OrderBook)
	return orderbook
}
