package match

import (
	"context"
	"sync"
)

type MatchingEngine struct {
	orderbooks    sync.Map
	publishTrader PublishTrader
}

func NewMatchingEngine(publishTrader PublishTrader) *MatchingEngine {
	return &MatchingEngine{
		publishTrader: publishTrader,
	}
}

func (engine *MatchingEngine) AddOrder(ctx context.Context, order *Order) error {
	orderbook := engine.OrderBook(order.MarketID)
	return orderbook.AddOrder(ctx, order)
}

func (engine *MatchingEngine) CancelOrder(ctx context.Context, marketID string, orderID string) error {
	orderbook := engine.OrderBook(marketID)
	return orderbook.CancelOrder(ctx, orderID)
}

func (engine *MatchingEngine) OrderBook(marketID string) *OrderBook {
	book, found := engine.orderbooks.Load(marketID)
	if !found {
		newbook := NewOrderBook(engine.publishTrader)
		book, _ = engine.orderbooks.LoadOrStore(marketID, newbook)
		go func() {
			_ = newbook.Start()
		}()
	}

	orderbook, _ := book.(*OrderBook)
	return orderbook
}
