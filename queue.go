package engine

import (
	"container/list"
	"sync"
	"sync/atomic"

	"github.com/huandu/skiplist"
	"github.com/shopspring/decimal"
)

type UpdateEvent struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

type priceUnit struct {
	totalSize decimal.Decimal
	list      *list.List
}

type queue struct {
	mu             sync.RWMutex
	side           Side
	totalOrders    int64
	depths         int64
	depthList      *skiplist.SkipList
	priceMap       map[string]*skiplist.Element
	orderMap       map[string]*list.Element
	updateEventMap *sync.Map
}

func NewBuyerQueue() *queue {
	return &queue{
		side: Side_Buy,
		depthList: skiplist.New(skiplist.GreaterThanFunc(func(lhs, rhs interface{}) int {
			d1 := lhs.(decimal.Decimal)
			d2 := rhs.(decimal.Decimal)

			if d1.LessThan(d2) {
				return 1
			} else if d1.GreaterThan(d2) {
				return -1
			}

			return 0
		})),
		priceMap:       make(map[string]*skiplist.Element),
		orderMap:       make(map[string]*list.Element),
		updateEventMap: &sync.Map{},
	}
}

func NewSellerQueue() *queue {
	return &queue{
		side: Side_Sell,
		depthList: skiplist.New(skiplist.GreaterThanFunc(func(lhs, rhs interface{}) int {
			d1 := lhs.(decimal.Decimal)
			d2 := rhs.(decimal.Decimal)

			if d1.GreaterThan(d2) {
				return 1
			} else if d1.LessThan(d2) {
				return -1
			}

			return 0
		})),
		priceMap:       make(map[string]*skiplist.Element),
		orderMap:       make(map[string]*list.Element),
		updateEventMap: &sync.Map{},
	}
}

func (q *queue) addOrder(order *Order, isFront bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if order.ID == "" {
		return
	}

	orderKey := order.Price.String() + "-" + order.ID

	el, ok := q.priceMap[order.Price.String()]
	if ok {
		_, ok := q.orderMap[orderKey]
		if ok {
			// duplicate order; ignore it at the moment
			return
		}

		var orderElement *list.Element
		unit := el.Value.(*priceUnit)
		if isFront {
			orderElement = unit.list.PushFront(order)
		} else {
			orderElement = unit.list.PushBack(order)
		}

		unit.totalSize = unit.totalSize.Add(order.Size)
		q.addUpdateEvent(order.Price.String(), unit.totalSize.String())
		q.orderMap[orderKey] = orderElement

		atomic.AddInt64(&q.totalOrders, 1)
	} else {
		unit := priceUnit{
			list: list.New(),
		}

		orderElement := unit.list.PushFront(order)
		unit.totalSize = order.Size
		q.addUpdateEvent(order.Price.String(), unit.totalSize.String())

		q.orderMap[orderKey] = orderElement

		el := q.depthList.Set(order.Price, &unit)
		q.priceMap[order.Price.String()] = el

		atomic.AddInt64(&q.totalOrders, 1)
		atomic.AddInt64(&q.depths, 1)
	}

}

func (q *queue) removeOrder(order *Order) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if order.ID == "" {
		return
	}

	skipElement, ok := q.priceMap[order.Price.String()]
	if ok {
		unit := skipElement.Value.(*priceUnit)

		orderKey := order.Price.String() + "-" + order.ID
		orderElement, ok := q.orderMap[orderKey]
		if ok {
			unit.list.Remove(orderElement)
			unit.totalSize = unit.totalSize.Sub(order.Size)
			q.addUpdateEvent(order.Price.String(), unit.totalSize.String())

			delete(q.orderMap, orderKey)
			atomic.AddInt64(&q.totalOrders, -1)
		}

		if unit.list.Len() == 0 {
			q.addUpdateEvent(order.Price.String(), unit.totalSize.String())
			q.depthList.RemoveElement(skipElement)
			delete(q.priceMap, order.Price.String())
			atomic.AddInt64(&q.depths, -1)
		}
	}
}

func (q *queue) getHeadOrder() *Order {
	q.mu.RLock()
	defer q.mu.RUnlock()

	el := q.depthList.Front()
	if el == nil {
		return nil
	}

	unit := el.Value.(*priceUnit)
	return unit.list.Front().Value.(*Order)
}

func (q *queue) popHeadOrder() *Order {
	ord := q.getHeadOrder()

	if ord != nil {
		q.removeOrder(ord)
	}

	return ord
}

func (q *queue) orderCount() int64 {
	return atomic.LoadInt64(&q.totalOrders)
}

func (q *queue) depthCount() int64 {
	return atomic.LoadInt64(&q.depths)
}

func (q *queue) addUpdateEvent(price, size string) {
	q.updateEventMap.Store(price, size)
}

func (q *queue) sinceLastUpdateEvents() []*UpdateEvent {
	events := []*UpdateEvent{}

	q.updateEventMap.Range(func(key, value interface{}) bool {
		events = append(events, &UpdateEvent{
			Price: key.(string),
			Size:  value.(string),
		})
		q.updateEventMap.Delete(key)
		return true
	})

	return events
}
