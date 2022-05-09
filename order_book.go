package engine

import (
	"encoding/json"
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
	Size         decimal.Decimal `json:"size"`
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
	bidQueue *queue
	askQueue *queue
	mu       sync.Mutex
}

func NewOrderBook() *OrderBook {
	return &OrderBook{
		bidQueue: NewBuyerQueue(),
		askQueue: NewSellerQueue(),
	}
}

// Add a buy order to the order book
func (book *OrderBook) addBuyOrder(order *Order) {
	book.bidQueue.addOrder(order, false)
}

// Add a sell order to the order book
func (book *OrderBook) addSellOrder(order *Order) {
	book.askQueue.addOrder(order, false)
}

// Remove a buy order from the order book at a given index
func (book *OrderBook) removeBuyOrder(order *Order) {
	book.bidQueue.removeOrder(order)
}

// Remove a sell order from the order book at a given index
func (book *OrderBook) removeSellOrder(order *Order) {
	book.askQueue.removeOrder(order)
}

// ProcessLimitOrder an order and return the trades generated before adding the remaining Size to the market
func (book *OrderBook) PlaceLimitOrder(order *Order) []Trade {
	if order.Side == Side_Buy {
		return book.buyLimitOrder(order)
	}

	return book.sellLimitOrder(order)
}

func (book *OrderBook) buyLimitOrder(order *Order) []Trade {
	trades := []Trade{}

	for {
		tOrd := book.askQueue.popHeadOrder()

		if tOrd == nil {
			book.bidQueue.addOrder(order, false)
			return trades
		}

		if order.Price.LessThan(tOrd.Price) {
			book.bidQueue.addOrder(order, false)
			book.askQueue.addOrder(tOrd, false)
			return trades
		}

		if order.Size.GreaterThan(tOrd.Size) {
			trade := Trade{
				TakerOrderID: order.ID,
				MakerOrderID: tOrd.ID,
				Price:        tOrd.Price,
				Size:         tOrd.Size,
				CreatedAt:    time.Now().UTC(),
			}
			trades = append(trades, trade)
			order.Size = order.Size.Sub(tOrd.Size)
		} else {
			trade := Trade{
				TakerOrderID: order.ID,
				MakerOrderID: tOrd.ID,
				Price:        tOrd.Price,
				Size:         tOrd.Size,
				CreatedAt:    time.Now().UTC(),
			}
			trades = append(trades, trade)
			tOrd.Size = tOrd.Size.Sub(order.Size)
			book.askQueue.addOrder(tOrd, true)

			break
		}
	}

	return trades
}

func (book *OrderBook) sellLimitOrder(order *Order) []Trade {
	trades := []Trade{}

	for {
		tOrd := book.bidQueue.popHeadOrder()

		if tOrd == nil {
			book.askQueue.addOrder(order, false)
			return trades
		}

		if order.Price.GreaterThan(tOrd.Price) {
			book.askQueue.addOrder(order, false)
			book.bidQueue.addOrder(tOrd, false)
			return trades
		}

		if order.Size.GreaterThan(tOrd.Size) {
			trade := Trade{
				TakerOrderID: order.ID,
				MakerOrderID: tOrd.ID,
				Price:        tOrd.Price,
				Size:         tOrd.Size,
				CreatedAt:    time.Now().UTC(),
			}
			trades = append(trades, trade)
			order.Size = order.Size.Sub(tOrd.Size)
		} else {
			trade := Trade{
				TakerOrderID: order.ID,
				MakerOrderID: tOrd.ID,
				Price:        tOrd.Price,
				Size:         tOrd.Size,
				CreatedAt:    time.Now().UTC(),
			}
			trades = append(trades, trade)
			tOrd.Size = tOrd.Size.Sub(order.Size)
			book.bidQueue.addOrder(tOrd, true)

			break
		}
	}

	return trades
}

func (book *OrderBook) PlaceMarketOrder(order *Order) []Trade {
	targetQueue := book.bidQueue
	if order.Side == Side_Buy {
		targetQueue = book.askQueue
	}

	if targetQueue.orderCount() == 0 {
		if order.Side == Side_Buy {
			book.bidQueue.addOrder(order, false)
		} else {
			book.askQueue.addOrder(order, false)
		}
		return nil
	}

	trades := []Trade{}

	for {
		tOrd := targetQueue.popHeadOrder()
		if tOrd == nil {
			break
		}

		if order.Size.GreaterThan(tOrd.Size) {
			trade := Trade{
				TakerOrderID: order.ID,
				MakerOrderID: tOrd.ID,
				Price:        tOrd.Price,
				Size:         tOrd.Size,
				CreatedAt:    time.Now().UTC(),
			}
			trades = append(trades, trade)

			order.Size = order.Size.Sub(tOrd.Size)
		} else {
			trade := Trade{
				TakerOrderID: order.ID,
				MakerOrderID: tOrd.ID,
				Price:        tOrd.Price,
				Size:         tOrd.Size,
				CreatedAt:    time.Now().UTC(),
			}
			trades = append(trades, trade)
			tOrd.Size = tOrd.Size.Sub(order.Size)

			targetQueue.addOrder(tOrd, true)

			break
		}
	}

	return trades
}
