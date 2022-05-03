package engine

import (
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

type Side int8

const (
	Side_Default = 0
	Side_Buy     = 1
	Side_Sell    = 2
)

type Order struct {
	ID        string          `json:"id"`
	Side      Side            `json:"side"`
	Size      decimal.Decimal `json:"size"`
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
	Size         decimal.Decimal `json:"Size"`
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

// ProcessLimitOrder an order and return the trades generated before adding the remaining Size to the market
func (book *OrderBook) PlaceLimitOrder(order Order) []Trade {
	book.mu.Lock()
	defer book.mu.Unlock()

	if order.Side == Side_Buy {
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
			if sellOrder.Size.GreaterThanOrEqual(order.Size) {
				trade := Trade{
					order.ID,
					sellOrder.ID,
					order.Size,
					sellOrder.Price,
					time.Now().UTC()}
				trades = append(trades, trade)
				sellOrder.Size = sellOrder.Size.Sub(order.Size)
				if sellOrder.Size.IsZero() {
					book.removeSellOrder(i)
				}
				return trades
			}
			// fill a partial order and continue
			if sellOrder.Size.LessThan(order.Size) {
				trades = append(trades, Trade{order.ID, sellOrder.ID, sellOrder.Size, sellOrder.Price, time.Now().UTC()})
				order.Size = order.Size.Sub(sellOrder.Size)
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
			if buyOrder.Size.GreaterThanOrEqual(order.Size) {
				trades = append(trades, Trade{order.ID, buyOrder.ID, order.Size, buyOrder.Price, time.Now().UTC()})
				buyOrder.Size = buyOrder.Size.Sub(order.Size)
				if buyOrder.Size.IsZero() {
					book.removeBuyOrder(i)
				}
				return trades
			}
			// fill a partial order and continue
			if buyOrder.Size.LessThan(order.Size) {
				trades = append(trades, Trade{order.ID, buyOrder.ID, buyOrder.Size, buyOrder.Price, time.Now().UTC()})
				order.Size = order.Size.Sub(buyOrder.Size)
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

	if order.Side == Side_Buy {
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
		if sellOrder.Size.GreaterThanOrEqual(order.Size) {
			trades = append(trades, Trade{order.ID, sellOrder.ID, order.Size, sellOrder.Price, time.Now().UTC()})
			sellOrder.Size = sellOrder.Size.Sub(order.Size)
			if sellOrder.Size.IsZero() {
				book.removeSellOrder(i)
			}
			return trades
		}
		// fill a partial order and continue
		if sellOrder.Size.LessThan(order.Size) {
			trades = append(trades, Trade{order.ID, sellOrder.ID, sellOrder.Size, sellOrder.Price, time.Now().UTC()})
			order.Size = order.Size.Sub(sellOrder.Size)
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
		if buyOrder.Size.GreaterThanOrEqual(order.Size) {
			trades = append(trades, Trade{order.ID, buyOrder.ID, order.Size, buyOrder.Price, time.Now().UTC()})
			buyOrder.Size = buyOrder.Size.Sub(order.Size)
			if buyOrder.Size.IsZero() {
				book.removeBuyOrder(i)
			}
			return trades
		}
		// fill a partial order and continue
		if buyOrder.Size.LessThan(order.Size) {
			trades = append(trades, Trade{order.ID, buyOrder.ID, buyOrder.Size, buyOrder.Price, time.Now().UTC()})
			order.Size = order.Size.Sub(buyOrder.Size)
			book.removeBuyOrder(i)
			i--
			continue
		}
	}

	// finally add the remaining order to the list
	book.addSellOrder(order)
	return trades
}
