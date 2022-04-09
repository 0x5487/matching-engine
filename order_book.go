package engine

import (
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

type OrderSide int8

const (
	OrderSideDefault = 0
	OrderSideBuy     = 1
	OrderSideSell    = 2
)

type Order struct {
	ID        string          `json:"id"`
	Side      OrderSide       `json:"side"`
	Amount    decimal.Decimal `json:"amount"`
	Price     decimal.Decimal `json:"price"`
	CreatedAt time.Time       `json:"created_at"`
}

func (order *Order) FromJSON(msg []byte) error {
	return json.Unmarshal(msg, order)
}

func (order *Order) ToJSON() []byte {
	str, _ := json.Marshal(order)
	return str
}

type Trade struct {
	TakerOrderID string          `json:"taker_order_id"`
	MakerOrderID string          `json:"maker_order_id"`
	Amount       decimal.Decimal `json:"amount"`
	Price        decimal.Decimal `json:"price"`
	CreatedAt    time.Time       `json:"created_at"`
}

func (trade *Trade) FromJSON(msg []byte) error {
	return json.Unmarshal(msg, trade)
}

func (trade *Trade) ToJSON() []byte {
	str, _ := json.Marshal(trade)
	return str
}

// OrderBook type
type OrderBook struct {
	BuyOrders  []Order
	SellOrders []Order
	mu         sync.Mutex
}

func NewOrderBook() *OrderBook {
	return &OrderBook{
		BuyOrders:  make([]Order, 0, 100),
		SellOrders: make([]Order, 0, 100),
	}
}

// Add a buy order to the order book
func (book *OrderBook) addBuyOrder(order Order) {
	book.BuyOrders = append(book.BuyOrders, order)

	sort.Slice(book.BuyOrders, func(a, b int) bool {
		orderA := book.BuyOrders[a]
		orderB := book.BuyOrders[b]

		if orderA.Price.GreaterThan(orderB.Price) {
			return true
		} else if orderA.Price.Equal(orderB.Price) {
			return orderA.CreatedAt.Before(orderB.CreatedAt)
		} else {
			return false
		}
	})
}

// Add a sell order to the order book
func (book *OrderBook) addSellOrder(order Order) {
	book.SellOrders = append(book.SellOrders, order)

	sort.Slice(book.SellOrders, func(a, b int) bool {
		orderA := book.SellOrders[a]
		orderB := book.SellOrders[b]

		if orderA.Price.LessThan(orderB.Price) {
			return true
		} else if orderA.Price.Equal(orderB.Price) {
			return orderA.CreatedAt.Before(orderB.CreatedAt)
		} else {
			return false
		}
	})
}

// Remove a buy order from the order book at a given index
func (book *OrderBook) removeBuyOrder(index int) {
	book.BuyOrders = append(book.BuyOrders[:index], book.BuyOrders[index+1:]...)
}

// Remove a sell order from the order book at a given index
func (book *OrderBook) removeSellOrder(index int) {
	book.SellOrders = append(book.SellOrders[:index], book.SellOrders[index+1:]...)
}

// ProcessLimitOrder an order and return the trades generated before adding the remaining amount to the market
func (book *OrderBook) PlaceLimitOrder(order Order) []Trade {
	book.mu.Lock()
	defer book.mu.Unlock()

	if order.Side == OrderSideBuy {
		return book.processLimitBuy(order)
	}
	return book.processLimitSell(order)
}

// Process a limit buy order
func (book *OrderBook) processLimitBuy(order Order) []Trade {
	trades := make([]Trade, 0, 1)
	n := len(book.SellOrders)

	if n == 0 {
		book.addBuyOrder(order)
		return nil
	}

	// check if we have at least one matching order
	if book.SellOrders[0].Price.LessThanOrEqual(order.Price) {
		// traverse all orders that match
		for i := 0; i < len(book.SellOrders); i++ {
			sellOrder := book.SellOrders[i]
			if sellOrder.Price.GreaterThan(order.Price) {
				break
			}
			// fill the entire order
			if sellOrder.Amount.GreaterThanOrEqual(order.Amount) {
				trade := Trade{
					order.ID,
					sellOrder.ID,
					order.Amount,
					sellOrder.Price,
					time.Now().UTC()}
				trades = append(trades, trade)
				sellOrder.Amount = sellOrder.Amount.Sub(order.Amount)
				if sellOrder.Amount.IsZero() {
					book.removeSellOrder(i)
				}
				return trades
			}
			// fill a partial order and continue
			if sellOrder.Amount.LessThan(order.Amount) {
				trades = append(trades, Trade{order.ID, sellOrder.ID, sellOrder.Amount, sellOrder.Price, time.Now().UTC()})
				order.Amount = order.Amount.Sub(sellOrder.Amount)
				book.removeSellOrder(i)
				i--
				continue
			}
		}
	}

	// finally add the remaining order to the list
	book.addBuyOrder(order)
	return trades
}

// Process a limit sell order
func (book *OrderBook) processLimitSell(order Order) []Trade {
	trades := make([]Trade, 0, 1)
	n := len(book.BuyOrders)

	if n == 0 {
		book.addSellOrder(order)
		return nil
	}

	// check if we have at least one matching order
	if book.BuyOrders[0].Price.GreaterThanOrEqual(order.Price) {
		// traverse all orders that match
		for i := 0; i < len(book.BuyOrders); i++ {
			buyOrder := book.BuyOrders[i]
			if buyOrder.Price.LessThan(order.Price) {
				break
			}
			// fill the entire order
			if buyOrder.Amount.GreaterThanOrEqual(order.Amount) {
				trades = append(trades, Trade{order.ID, buyOrder.ID, order.Amount, buyOrder.Price, time.Now().UTC()})
				buyOrder.Amount = buyOrder.Amount.Sub(order.Amount)
				if buyOrder.Amount.IsZero() {
					book.removeBuyOrder(i)
				}
				return trades
			}
			// fill a partial order and continue
			if buyOrder.Amount.LessThan(order.Amount) {
				trades = append(trades, Trade{order.ID, buyOrder.ID, buyOrder.Amount, buyOrder.Price, time.Now().UTC()})
				order.Amount = order.Amount.Sub(buyOrder.Amount)
				book.removeBuyOrder(i)
				i--
				continue
			}
		}
	}

	// finally add the remaining order to the list
	book.addSellOrder(order)
	return trades
}

func (book *OrderBook) PlaceMarketOrder(order Order) []Trade {
	book.mu.Lock()
	defer book.mu.Unlock()

	if order.Side == OrderSideBuy {
		return book.processMarketBuy(order)
	}
	return book.processMarketSell(order)
}

// Process a limit buy order
func (book *OrderBook) processMarketBuy(order Order) []Trade {
	trades := make([]Trade, 0, 1)
	n := len(book.SellOrders)

	if n == 0 {
		book.addBuyOrder(order)
		return nil
	}

	// traverse all orders that match
	for i := 0; i < len(book.SellOrders); i++ {
		sellOrder := book.SellOrders[i]
		// fill the entire order
		if sellOrder.Amount.GreaterThanOrEqual(order.Amount) {
			trades = append(trades, Trade{order.ID, sellOrder.ID, order.Amount, sellOrder.Price, time.Now().UTC()})
			sellOrder.Amount = sellOrder.Amount.Sub(order.Amount)
			if sellOrder.Amount.IsZero() {
				book.removeSellOrder(i)
			}
			return trades
		}
		// fill a partial order and continue
		if sellOrder.Amount.LessThan(order.Amount) {
			trades = append(trades, Trade{order.ID, sellOrder.ID, sellOrder.Amount, sellOrder.Price, time.Now().UTC()})
			order.Amount = order.Amount.Sub(sellOrder.Amount)
			book.removeSellOrder(i)
			i--
			continue
		}
	}

	// finally add the remaining order to the list
	book.addBuyOrder(order)
	return trades
}

// Process a limit sell order
func (book *OrderBook) processMarketSell(order Order) []Trade {
	trades := make([]Trade, 0, 1)
	n := len(book.BuyOrders)

	if n == 0 {
		book.addSellOrder(order)
		return nil
	}

	// traverse all orders that match
	for i := 0; i < len(book.BuyOrders); i++ {
		buyOrder := book.BuyOrders[i]
		// fill the entire order
		if buyOrder.Amount.GreaterThanOrEqual(order.Amount) {
			trades = append(trades, Trade{order.ID, buyOrder.ID, order.Amount, buyOrder.Price, time.Now().UTC()})
			buyOrder.Amount = buyOrder.Amount.Sub(order.Amount)
			if buyOrder.Amount.IsZero() {
				book.removeBuyOrder(i)
			}
			return trades
		}
		// fill a partial order and continue
		if buyOrder.Amount.LessThan(order.Amount) {
			trades = append(trades, Trade{order.ID, buyOrder.ID, buyOrder.Amount, buyOrder.Price, time.Now().UTC()})
			order.Amount = order.Amount.Sub(buyOrder.Amount)
			book.removeBuyOrder(i)
			i--
			continue
		}
	}

	// finally add the remaining order to the list
	book.addSellOrder(order)
	return trades
}
