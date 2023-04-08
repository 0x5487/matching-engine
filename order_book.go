package engine

import (
	"time"

	"github.com/shopspring/decimal"
)

type Side int8

const (
	SideDefault = 0
	SideBuy     = 1
	SideSell    = 2
)

type OrderType string

const (
	OrderTypeMarket   = "market"
	OrderTypeLimit    = "limit"
	OrderTypeFOK      = "fok"       // 全部成交或立即取消
	OrderTypeIOC      = "ioc"       // 立即成交并取消剩余
	OrderTypePostOnly = "post_only" // be maker order only
)

type Order struct {
	ID        string          `json:"id"`
	Side      Side            `json:"side"`
	Price     decimal.Decimal `json:"price"`
	Size      decimal.Decimal `json:"size"`
	Type      OrderType       `json:"type"`
	CreatedAt time.Time       `json:"created_at"`
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
	book.bidQueue.insertOrder(order, false)
}

// AddSellOrder adds a sell order to the order book
func (book *OrderBook) AddSellOrder(order *Order) {
	book.askQueue.insertOrder(order, false)
}

// RemoveBuyOrder removes a buy order from the order book at a given index
func (book *OrderBook) RemoveBuyOrder(order *Order) {
	book.bidQueue.removeOrder(order)
}

// RemoveSellOrder removes a sell order from the order book at a given index
func (book *OrderBook) RemoveSellOrder(order *Order) {
	book.askQueue.removeOrder(order)
}

func (book *OrderBook) PlaceOrder(order *Order) ([]*Trade, error) {
	if len(order.Type) == 0 {
		return nil, ErrInvalidParam
	}

	if order.Type == OrderTypeMarket {
		return book.handleMarketOrder(order)
	}

	return book.handleOrder(order)
}

func (book *OrderBook) CancelOrder(order *Order) {

}

func (book *OrderBook) handleOrder(order *Order) ([]*Trade, error) {
	var myQueue, targetQueue *queue
	if order.Side == SideBuy {
		myQueue = book.bidQueue
		targetQueue = book.askQueue
	} else {
		myQueue = book.askQueue
		targetQueue = book.bidQueue
	}

	trades := []*Trade{}

	// ensure available volumn can handle FOK order
	if order.Type == OrderTypeFOK {
		vol := order.Price.Mul(order.Size)
		if targetQueue.availableVolume.LessThan(vol) {
			return trades, ErrCanceled
		}
	}

	for {
		tOrd := targetQueue.popHeadOrder()

		if tOrd == nil {
			switch order.Type {
			case OrderTypeLimit, OrderTypePostOnly:
				myQueue.insertOrder(order, false)
				return trades, nil
			case OrderTypeIOC:
				return trades, ErrCanceled
			}
		}

		if order.Side == SideBuy && order.Price.LessThan(tOrd.Price) ||
			order.Side == SideSell && order.Price.GreaterThan(tOrd.Price) {
			targetQueue.insertOrder(tOrd, true)

			switch order.Type {
			case OrderTypeLimit, OrderTypePostOnly:
				myQueue.insertOrder(order, false)
				return trades, nil
			case OrderTypeIOC:
				return trades, ErrCanceled
			case OrderTypeFOK:
				targetQueue.addOrder(tOrd)
				return trades, ErrCanceled
			}
		}

		if order.Type == OrderTypePostOnly {
			targetQueue.addOrder(tOrd)
			return trades, ErrCanceled
		}

		if order.Size.GreaterThanOrEqual(tOrd.Size) {
			trade := Trade{
				TakerOrderID: order.ID,
				MakerOrderID: tOrd.ID,
				Price:        tOrd.Price,
				Size:         tOrd.Size,
				CreatedAt:    time.Now().UTC(),
			}
			trades = append(trades, &trade)
			order.Size = order.Size.Sub(tOrd.Size)

			if order.Size.Equal(decimal.Zero) {
				break
			}
		} else {
			trade := Trade{
				TakerOrderID: order.ID,
				MakerOrderID: tOrd.ID,
				Price:        tOrd.Price,
				Size:         order.Size,
				CreatedAt:    time.Now().UTC(),
			}
			trades = append(trades, &trade)
			tOrd.Size = tOrd.Size.Sub(order.Size)
			targetQueue.insertOrder(tOrd, true)

			break
		}
	}

	return trades, nil
}

func (book *OrderBook) handleMarketOrder(order *Order) ([]*Trade, error) {
	targetQueue := book.bidQueue
	if order.Side == SideBuy {
		targetQueue = book.askQueue
	}

	trades := []*Trade{}

	for {
		tOrd := targetQueue.popHeadOrder()

		if tOrd == nil {
			return nil, ErrCanceled
		}

		// 市價單的 size 是總額，不是數量
		amount := tOrd.Price.Mul(tOrd.Size)

		if order.Size.GreaterThanOrEqual(amount) {
			trade := Trade{
				TakerOrderID: order.ID,
				MakerOrderID: tOrd.ID,
				Price:        tOrd.Price,
				Size:         tOrd.Size,
				CreatedAt:    time.Now().UTC(),
			}
			trades = append(trades, &trade)
			order.Size = order.Size.Sub(amount)
			if order.Size.Equal(decimal.Zero) {
				break
			}
		} else {
			tSize := order.Size.Div(tOrd.Price)

			trade := Trade{
				TakerOrderID: order.ID,
				MakerOrderID: tOrd.ID,
				Price:        tOrd.Price,
				Size:         tSize,
				CreatedAt:    time.Now().UTC(),
			}
			trades = append(trades, &trade)

			tOrd.Size = tOrd.Size.Sub(tSize)
			targetQueue.insertOrder(tOrd, true)

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
