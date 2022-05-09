package engine

import (
	"container/list"
	"sync"
	"sync/atomic"

	"github.com/huandu/skiplist"
	"github.com/shopspring/decimal"
)

type queue struct {
	mu          sync.RWMutex
	side        Side
	totalOrders int64
	depths      int64
	depthList   *skiplist.SkipList
	priceMap    map[string]*skiplist.Element
	sizeMap     map[string]*list.Element
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
		priceMap: make(map[string]*skiplist.Element),
		sizeMap:  make(map[string]*list.Element),
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
		priceMap: make(map[string]*skiplist.Element),
		sizeMap:  make(map[string]*list.Element),
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
		_, ok := q.sizeMap[orderKey]
		if ok {
			// duplicate order; ignore it at the moment
			return
		}

		var orderElement *list.Element
		if isFront {
			orderElement = el.Value.(*list.List).PushFront(order)
		} else {
			orderElement = el.Value.(*list.List).PushBack(order)
		}

		q.sizeMap[orderKey] = orderElement

		atomic.AddInt64(&q.totalOrders, 1)
	} else {
		newList := list.New()
		orderElement := newList.PushFront(order)
		q.sizeMap[orderKey] = orderElement

		el := q.depthList.Set(order.Price, newList)
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
		sizeList := skipElement.Value.(*list.List)

		orderKey := order.Price.String() + "-" + order.ID
		orderElement, ok := q.sizeMap[orderKey]
		if ok {
			sizeList.Remove(orderElement)
			delete(q.sizeMap, orderKey)
			atomic.AddInt64(&q.totalOrders, -1)
		}

		if sizeList.Len() == 0 {
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

	return el.Value.(*list.List).Front().Value.(*Order)
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
