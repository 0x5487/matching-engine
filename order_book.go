package match

import (
	"time"

	"github.com/shopspring/decimal"
)

type Side int8

const (
	Buy  Side = 1
	Sell Side = 2
)

type OrderType string

const (
	Market   OrderType = "market"
	Limit    OrderType = "limit"
	FOK      OrderType = "fok"       // 全部成交或立即取消
	IOC      OrderType = "ioc"       // 立即成交并取消剩余
	PostOnly OrderType = "post_only" // be maker order only
)

type Order struct {
	ID        string          `json:"id"`
	MarketID  string          `json:"market_id"`
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

func (book *OrderBook) PlaceOrder(order *Order) ([]*Trade, error) {
	if len(order.Type) == 0 || len(order.ID) == 0 {
		return nil, ErrInvalidParam
	}

	if order.Type == Market {
		return book.handleMarketOrder(order)
	}

	return book.handleOrder(order)
}

func (book *OrderBook) CancelOrder(id string) {
	order := book.askQueue.order(id)
	if order != nil {
		book.askQueue.removeOrder(order.Price, id)
		return
	}

	order = book.bidQueue.order(id)
	if order != nil {
		book.bidQueue.removeOrder(order.Price, id)
		return
	}
}

func (book *OrderBook) handleOrder(order *Order) ([]*Trade, error) {
	var myQueue, targetQueue *queue
	if order.Side == Buy {
		myQueue = book.bidQueue
		targetQueue = book.askQueue
	} else {
		myQueue = book.askQueue
		targetQueue = book.bidQueue
	}

	trades := []*Trade{}

	// ensure the order book can handle FOK order
	if order.Type == FOK {

		el := targetQueue.depthList.Front()
		orignalOrderSize := order.Size

		for {
			if el == nil {
				return trades, ErrCanceled
			}

			unit := el.Value.(*priceUnit)
			tOrd := unit.list.Front().Value.(*Order)

			if order.Side == Buy && order.Price.GreaterThanOrEqual(tOrd.Price) && order.Size.GreaterThanOrEqual(unit.totalSize) ||
				order.Side == Sell && order.Price.LessThanOrEqual(tOrd.Price) && order.Size.GreaterThanOrEqual(unit.totalSize) {
				order.Size = order.Size.Sub(tOrd.Size)

				if order.Size.Equal(decimal.Zero) {
					order.Size = orignalOrderSize
					break
				}
			}

			if order.Size.LessThan(decimal.Zero) {
				return trades, ErrCanceled
			}

			el = el.Next()
		}
	}

	for {
		tOrd := targetQueue.popHeadOrder()

		if tOrd == nil {
			switch order.Type {
			case Limit, PostOnly:
				myQueue.insertOrder(order, false)
				return trades, nil
			case IOC:
				return trades, ErrCanceled
			}
		}

		if order.Side == Buy && order.Price.LessThan(tOrd.Price) ||
			order.Side == Sell && order.Price.GreaterThan(tOrd.Price) {
			targetQueue.insertOrder(tOrd, true)

			switch order.Type {
			case Limit, PostOnly:
				myQueue.insertOrder(order, false)
				return trades, nil
			case IOC:
				return trades, ErrCanceled
			}
		}

		if order.Type == PostOnly {
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
	if order.Side == Buy {
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
