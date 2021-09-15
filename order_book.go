package engine

import (
	"encoding/json"
	"sort"
	"sync"
)

type Order struct {
	Amount uint64 `json:"amount"`
	Price  uint64 `json:"price"`
	ID     string `json:"id"`
	Side   int8   `json:"side"` // 0: sell, 1: buy
}

func (order *Order) FromJSON(msg []byte) error {
	return json.Unmarshal(msg, order)
}

func (order *Order) ToJSON() []byte {
	str, _ := json.Marshal(order)
	return str
}

type Trade struct {
	TakerOrderID string `json:"taker_order_id"`
	MakerOrderID string `json:"maker_order_id"`
	Amount       uint64 `json:"amount"`
	Price        uint64 `json:"price"`
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

	sort.Slice(book.BuyOrders, func(i, j int) bool {
		return book.BuyOrders[i].Price > book.BuyOrders[j].Price
	})
}

// Add a sell order to the order book
func (book *OrderBook) addSellOrder(order Order) {
	book.SellOrders = append(book.SellOrders, order)

	sort.Slice(book.SellOrders, func(i, j int) bool {
		return book.SellOrders[i].Price < book.SellOrders[j].Price
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

	if order.Side == 1 {
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
	if book.SellOrders[0].Price <= order.Price {
		// traverse all orders that match
		for i := 0; i < len(book.SellOrders); i++ {
			sellOrder := book.SellOrders[i]
			if sellOrder.Price > order.Price {
				break
			}
			// fill the entire order
			if sellOrder.Amount >= order.Amount {
				trades = append(trades, Trade{order.ID, sellOrder.ID, order.Amount, sellOrder.Price})
				sellOrder.Amount -= order.Amount
				if sellOrder.Amount == 0 {
					book.removeSellOrder(i)
				}
				return trades
			}
			// fill a partial order and continue
			if sellOrder.Amount < order.Amount {
				trades = append(trades, Trade{order.ID, sellOrder.ID, sellOrder.Amount, sellOrder.Price})
				order.Amount -= sellOrder.Amount
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
	if book.BuyOrders[0].Price >= order.Price {
		// traverse all orders that match
		for i := 0; i < len(book.BuyOrders); i++ {
			buyOrder := book.BuyOrders[i]
			if buyOrder.Price < order.Price {
				break
			}
			// fill the entire order
			if buyOrder.Amount >= order.Amount {
				trades = append(trades, Trade{order.ID, buyOrder.ID, order.Amount, buyOrder.Price})
				buyOrder.Amount -= order.Amount
				if buyOrder.Amount == 0 {
					book.removeBuyOrder(i)
				}
				return trades
			}
			// fill a partial order and continue
			if buyOrder.Amount < order.Amount {
				trades = append(trades, Trade{order.ID, buyOrder.ID, buyOrder.Amount, buyOrder.Price})
				order.Amount -= buyOrder.Amount
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

	if order.Side == 1 {
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
		if sellOrder.Amount >= order.Amount {
			trades = append(trades, Trade{order.ID, sellOrder.ID, order.Amount, sellOrder.Price})
			sellOrder.Amount -= order.Amount
			if sellOrder.Amount == 0 {
				book.removeSellOrder(i)
			}
			return trades
		}
		// fill a partial order and continue
		if sellOrder.Amount < order.Amount {
			trades = append(trades, Trade{order.ID, sellOrder.ID, sellOrder.Amount, sellOrder.Price})
			order.Amount -= sellOrder.Amount
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
		if buyOrder.Amount >= order.Amount {
			trades = append(trades, Trade{order.ID, buyOrder.ID, order.Amount, buyOrder.Price})
			buyOrder.Amount -= order.Amount
			if buyOrder.Amount == 0 {
				book.removeBuyOrder(i)
			}
			return trades
		}
		// fill a partial order and continue
		if buyOrder.Amount < order.Amount {
			trades = append(trades, Trade{order.ID, buyOrder.ID, buyOrder.Amount, buyOrder.Price})
			order.Amount -= buyOrder.Amount
			book.removeBuyOrder(i)
			i--
			continue
		}
	}

	// finally add the remaining order to the list
	book.addSellOrder(order)
	return trades
}
