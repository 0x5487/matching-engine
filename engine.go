package match

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/shopspring/decimal"
)

// MatchingEngine manages multiple order books for different markets.
type MatchingEngine struct {
	isShutdown    atomic.Bool
	orderbooks    sync.Map
	publishTrader PublishLog
}

// NewMatchingEngine creates a new matching engine instance.
func NewMatchingEngine(publishTrader PublishLog) *MatchingEngine {
	return &MatchingEngine{
		publishTrader: publishTrader,
	}
}

// AddOrder adds an order to the appropriate order book based on the market ID.
// Returns ErrShutdown if the engine is shutting down.
func (engine *MatchingEngine) AddOrder(ctx context.Context, order *Order) error {
	if engine.isShutdown.Load() {
		return ErrShutdown
	}
	orderbook := engine.OrderBook(order.MarketID)
	if orderbook == nil {
		return ErrShutdown
	}
	return orderbook.AddOrder(ctx, order)
}

// AmendOrder modifies an existing order in the appropriate order book.
// Returns ErrShutdown if the engine is shutting down.
func (engine *MatchingEngine) AmendOrder(ctx context.Context, marketID string, orderID string, newPrice decimal.Decimal, newSize decimal.Decimal) error {
	if engine.isShutdown.Load() {
		return ErrShutdown
	}
	orderbook := engine.OrderBook(marketID)
	if orderbook == nil {
		return ErrShutdown
	}
	return orderbook.AmendOrder(ctx, orderID, newPrice, newSize)
}

// CancelOrder cancels an order in the appropriate order book.
// Returns ErrShutdown if the engine is shutting down.
func (engine *MatchingEngine) CancelOrder(ctx context.Context, marketID string, orderID string) error {
	if engine.isShutdown.Load() {
		return ErrShutdown
	}
	orderbook := engine.OrderBook(marketID)
	if orderbook == nil {
		return ErrShutdown
	}
	return orderbook.CancelOrder(ctx, orderID)
}

// OrderBook retrieves the order book for a specific market ID, creating it if it doesn't exist.
// Returns nil if the engine is shutting down.
func (engine *MatchingEngine) OrderBook(marketID string) *OrderBook {
	// Do not create new order books during shutdown
	if engine.isShutdown.Load() {
		book, found := engine.orderbooks.Load(marketID)
		if !found {
			return nil
		}
		orderbook, _ := book.(*OrderBook)
		return orderbook
	}

	book, found := engine.orderbooks.Load(marketID)
	if !found {
		newbook := NewOrderBook(engine.publishTrader)
		var loaded bool
		book, loaded = engine.orderbooks.LoadOrStore(marketID, newbook)
		if !loaded {
			go func() {
				_ = newbook.Start()
			}()
		}
	}

	orderbook, _ := book.(*OrderBook)
	return orderbook
}

// Shutdown gracefully shuts down all order books in the engine.
// It blocks until all order books have completed their shutdown or the context is cancelled.
// Returns nil if all order books shut down successfully, or an aggregated error otherwise.
func (engine *MatchingEngine) Shutdown(ctx context.Context) error {
	// Set shutdown flag to prevent new orders and new market creation
	engine.isShutdown.Store(true)

	var wg sync.WaitGroup
	var errs []error
	var errMu sync.Mutex

	// Shutdown all order books in parallel
	engine.orderbooks.Range(func(key, value any) bool {
		wg.Add(1)
		go func(marketID string, book *OrderBook) {
			defer wg.Done()
			if err := book.Shutdown(ctx); err != nil {
				errMu.Lock()
				errs = append(errs, err)
				errMu.Unlock()
			}
		}(key.(string), value.(*OrderBook))
		return true
	})

	// Wait for all order books to complete shutdown
	wg.Wait()

	// Return aggregated errors if any
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
