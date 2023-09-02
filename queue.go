package match

import (
	"container/list"
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

type DepthItem struct {
	ID    uint32
	Price decimal.Decimal
	Size  decimal.Decimal
}

type queue struct {
	side        Side
	totalOrders int64
	depths      int64
	depthList   *skiplist.SkipList
	priceList   map[string]*skiplist.Element
	orders      map[string]*list.Element
}

func NewBuyerQueue() *queue {
	return &queue{
		side: Buy,
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
		priceList: make(map[string]*skiplist.Element),
		orders:    make(map[string]*list.Element),
	}
}

func NewSellerQueue() *queue {
	return &queue{
		side: Sell,
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
		priceList: make(map[string]*skiplist.Element),
		orders:    make(map[string]*list.Element),
	}
}

func (q *queue) addOrder(order *Order) {
	if order.Side == Buy {
		q.insertOrder(order, false)
	} else {
		q.insertOrder(order, true)
	}
}

func (q *queue) order(id string) *Order {
	el, ok := q.orders[id]
	if ok {
		order := el.Value.(*Order)
		return order
	}

	return nil
}

func (q *queue) insertOrder(order *Order, isFront bool) {
	el, ok := q.priceList[order.Price.String()]
	if ok {
		var orderElement *list.Element
		unit := el.Value.(*priceUnit)
		if isFront {
			orderElement = unit.list.PushFront(order)
		} else {
			orderElement = unit.list.PushBack(order)
		}

		unit.totalSize = unit.totalSize.Add(order.Size)
		q.orders[order.ID] = orderElement

		atomic.AddInt64(&q.totalOrders, 1)
	} else {
		unit := priceUnit{
			list: list.New(),
		}

		orderElement := unit.list.PushFront(order)
		unit.totalSize = order.Size

		q.orders[order.ID] = orderElement

		el := q.depthList.Set(order.Price, &unit)
		q.priceList[order.Price.String()] = el

		atomic.AddInt64(&q.totalOrders, 1)
		atomic.AddInt64(&q.depths, 1)
	}
}

func (q *queue) removeOrder(price decimal.Decimal, id string) {
	skipElement, ok := q.priceList[price.String()]
	if ok {
		unit := skipElement.Value.(*priceUnit)

		orderElement, ok := q.orders[id]
		order := orderElement.Value.(*Order)
		if ok {
			unit.list.Remove(orderElement)
			unit.totalSize = unit.totalSize.Sub(order.Size)
			delete(q.orders, id)
			atomic.AddInt64(&q.totalOrders, -1)
		}

		if unit.list.Len() == 0 {
			q.depthList.RemoveElement(skipElement)
			delete(q.priceList, order.Price.String())
			atomic.AddInt64(&q.depths, -1)
		}

	}
}

func (q *queue) getHeadOrder() *Order {
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
		q.removeOrder(ord.Price, ord.ID)
	}

	return ord
}

func (q *queue) orderCount() int64 {
	return atomic.LoadInt64(&q.totalOrders)
}

func (q *queue) depthCount() int64 {
	return atomic.LoadInt64(&q.depths)
}

func (q *queue) depth(limit uint32) []*DepthItem {
	result := make([]*DepthItem, 0, limit)

	el := q.depthList.Front()

	var i uint32 = 1
	for i < limit && el != nil {
		unit := el.Value.(*priceUnit)
		order := unit.list.Front().Value.(*Order)
		d := DepthItem{
			ID:    i,
			Price: order.Price,
			Size:  unit.totalSize,
		}

		result = append(result, &d)

		el = el.Next()
	}

	return result
}
