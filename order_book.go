package engine

import (
	"time"

	"github.com/shopspring/decimal"
)

type Side int8

const (
	Side_Default = 0
	Side_Buy     = 1
	Side_Sell    = 2
)

type TimeInForce string

const (
	TimeInForce_GTC = "gtc"
	TimeInForce_IOC = "ioc"
	TimeInForce_FOK = "fok"
)

type Order struct {
	ID          string          `json:"id"`
	Side        Side            `json:"side"`
	Price       decimal.Decimal `json:"price"`
	Size        decimal.Decimal `json:"size"`
	TimeInForce TimeInForce     `json:"tif"`
	PostOnly    bool            `json:"post_only"`
	CreatedAt   time.Time       `json:"created_at"`
}

type Trade struct {
	TakerOrderID string          `json:"taker_order_id"`
	MakerOrderID string          `json:"maker_order_id"`
	Size         decimal.Decimal `json:"size"`
	Price        decimal.Decimal `json:"price"`
	CreatedAt    time.Time       `json:"created_at"`
}

type OrderBookUpdateEvent struct {
	Bids []*UpdateEvent
	Asks []*UpdateEvent
	Time time.Time
}

// OrderBook type
type OrderBook struct {
	bidQueue        *queue
	askQueue        *queue
	updateEventChan chan *OrderBookUpdateEvent
}

func NewOrderBook() *OrderBook {
	return &OrderBook{
		bidQueue: NewBuyerQueue(),
		askQueue: NewSellerQueue(),
	}
}

// AddBuyOrder adds a buy order to the order book
func (book *OrderBook) AddBuyOrder(order *Order) {
	book.bidQueue.addOrder(order, false)
}

// AddSellOrder adds a sell order to the order book
func (book *OrderBook) AddSellOrder(order *Order) {
	book.askQueue.addOrder(order, false)
}

// RemoveBuyOrder removes a buy order from the order book at a given index
func (book *OrderBook) RemoveBuyOrder(order *Order) {
	book.bidQueue.removeOrder(order)
}

// RemoveSellOrder removes a sell order from the order book at a given index
func (book *OrderBook) RemoveSellOrder(order *Order) {
	book.askQueue.removeOrder(order)
}

// PlaceLimitOrder place an order and return the trades generated before adding the remaining Size to the market
func (book *OrderBook) PlaceLimitOrder(order *Order) ([]*Trade, error) {
	if order.Side == Side_Buy {
		return book.processLimitBuyOrder(order)
	}

	return book.processLimitSellOrder(order)
}

func (book *OrderBook) processLimitBuyOrder(order *Order) ([]*Trade, error) {
	trades := []*Trade{}

	for {
		tOrd := book.askQueue.popHeadOrder()

		if tOrd == nil {
			switch order.TimeInForce {
			case TimeInForce_GTC:
				book.bidQueue.addOrder(order, false)
				return trades, nil
			case TimeInForce_IOC:
				return trades, ErrCanceled
			case TimeInForce_FOK:
				return trades, ErrCanceled
			}
		}

		if order.Price.LessThan(tOrd.Price) {
			book.askQueue.addOrder(tOrd, true)

			switch order.TimeInForce {
			case TimeInForce_GTC:
				book.bidQueue.addOrder(order, false)
				return trades, nil
			case TimeInForce_IOC:
				return trades, ErrCanceled
			case TimeInForce_FOK:
				return trades, ErrCanceled
			}
		} else {

			if order.PostOnly {
				book.askQueue.addOrder(tOrd, true)
				return trades, ErrCanceled
			}

		}

		if order.Size.GreaterThan(tOrd.Size) {
			trade := Trade{
				TakerOrderID: order.ID,
				MakerOrderID: tOrd.ID,
				Price:        tOrd.Price,
				Size:         tOrd.Size,
				CreatedAt:    time.Now().UTC(),
			}
			trades = append(trades, &trade)
			order.Size = order.Size.Sub(tOrd.Size)
		} else {
			trade := Trade{
				TakerOrderID: order.ID,
				MakerOrderID: tOrd.ID,
				Price:        tOrd.Price,
				Size:         tOrd.Size,
				CreatedAt:    time.Now().UTC(),
			}
			trades = append(trades, &trade)
			tOrd.Size = tOrd.Size.Sub(order.Size)
			book.askQueue.addOrder(tOrd, true)

			break
		}
	}

	return trades, nil
}

func (book *OrderBook) processLimitSellOrder(order *Order) ([]*Trade, error) {
	trades := []*Trade{}

	for {
		tOrd := book.bidQueue.popHeadOrder()

		if tOrd == nil {
			switch order.TimeInForce {
			case TimeInForce_GTC:
				book.askQueue.addOrder(order, false)
				return trades, nil
			case TimeInForce_IOC:
				return trades, ErrCanceled
			case TimeInForce_FOK:
				return trades, ErrCanceled
			}
		}

		if order.Price.GreaterThan(tOrd.Price) {
			book.bidQueue.addOrder(tOrd, true)

			switch order.TimeInForce {
			case TimeInForce_GTC:
				book.askQueue.addOrder(order, false)
				return trades, nil
			case TimeInForce_IOC:
				return trades, ErrCanceled
			case TimeInForce_FOK:
				return trades, ErrCanceled
			}
		} else {

			if order.PostOnly {
				book.bidQueue.addOrder(tOrd, true)
				return trades, ErrCanceled
			}

		}

		if order.Size.GreaterThan(tOrd.Size) {
			trade := Trade{
				TakerOrderID: order.ID,
				MakerOrderID: tOrd.ID,
				Price:        tOrd.Price,
				Size:         tOrd.Size,
				CreatedAt:    time.Now().UTC(),
			}
			trades = append(trades, &trade)
			order.Size = order.Size.Sub(tOrd.Size)
		} else {
			trade := Trade{
				TakerOrderID: order.ID,
				MakerOrderID: tOrd.ID,
				Price:        tOrd.Price,
				Size:         tOrd.Size,
				CreatedAt:    time.Now().UTC(),
			}
			trades = append(trades, &trade)
			tOrd.Size = tOrd.Size.Sub(order.Size)
			book.bidQueue.addOrder(tOrd, true)

			break
		}
	}

	return trades, nil
}

func (book *OrderBook) PlaceMarketOrder(order *Order) ([]*Trade, error) {
	targetQueue := book.bidQueue
	if order.Side == Side_Buy {
		targetQueue = book.askQueue
	}

	if targetQueue.orderCount() == 0 {
		return nil, ErrCanceled
	}

	trades := []*Trade{}

	for {
		tOrd := targetQueue.popHeadOrder()
		if tOrd == nil {
			if order.Size.GreaterThan(decimal.Zero) {
				return trades, ErrCanceled
			}
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
			trades = append(trades, &trade)

			order.Size = order.Size.Sub(tOrd.Size)
		} else {
			trade := Trade{
				TakerOrderID: order.ID,
				MakerOrderID: tOrd.ID,
				Price:        tOrd.Price,
				Size:         tOrd.Size,
				CreatedAt:    time.Now().UTC(),
			}
			trades = append(trades, &trade)
			tOrd.Size = tOrd.Size.Sub(order.Size)

			targetQueue.addOrder(tOrd, true)

			break
		}
	}

	return trades, nil
}

func (book *OrderBook) RegisterUpdateEventChan(updateEventChan chan *OrderBookUpdateEvent) {
	book.updateEventChan = updateEventChan

	go func() {
		updateEventPeriod := time.Duration(1) * time.Second
		updateEventTicker := time.NewTicker(updateEventPeriod)

		for {
			<-updateEventTicker.C
			bidUpdateEvents := book.bidQueue.sinceLastUpdateEvents()
			askUpdateEvents := book.askQueue.sinceLastUpdateEvents()

			if len(bidUpdateEvents) == 0 && len(askUpdateEvents) == 0 {
				continue
			}

			bookUpdateEvt := OrderBookUpdateEvent{
				Bids: bidUpdateEvents,
				Asks: askUpdateEvents,
				Time: time.Now().UTC(),
			}

			book.updateEventChan <- &bookUpdateEvt
		}
	}()
}
