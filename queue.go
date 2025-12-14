package match

import (
	"container/list"

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

// NewBuyerQueue creates a new queue for buy orders (bids).
// The orders are sorted by price in descending order (highest price first).
func NewBuyerQueue() *queue {
	return &queue{
		side: Buy,
		depthList: skiplist.New(skiplist.GreaterThanFunc(func(lhs, rhs interface{}) int {
			d1, _ := lhs.(decimal.Decimal)
			d2, _ := rhs.(decimal.Decimal)

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

// NewSellerQueue creates a new queue for sell orders (asks).
// The orders are sorted by price in ascending order (lowest price first).
func NewSellerQueue() *queue {
	return &queue{
		side: Sell,
		depthList: skiplist.New(skiplist.GreaterThanFunc(func(lhs, rhs interface{}) int {
			d1, _ := lhs.(decimal.Decimal)
			d2, _ := rhs.(decimal.Decimal)

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

// order finds an order by its ID.
func (q *queue) order(id string) *Order {
	el, ok := q.orders[id]
	if ok {
		order, _ := el.Value.(*Order)
		return order
	}

	return nil
}

// insertOrder inserts an order into the queue.
// It updates the price list and depth list.
func (q *queue) insertOrder(order *Order, isFront bool) {
	el, ok := q.priceList[order.Price.String()]
	if ok {
		var orderElement *list.Element
		unit, _ := el.Value.(*priceUnit)
		if isFront {
			orderElement = unit.list.PushFront(order)
		} else {
			orderElement = unit.list.PushBack(order)
		}

		unit.totalSize = unit.totalSize.Add(order.Size)
		q.orders[order.ID] = orderElement

		q.totalOrders++
	} else {
		unit := priceUnit{
			list: list.New(),
		}

		orderElement := unit.list.PushFront(order)
		unit.totalSize = order.Size

		q.orders[order.ID] = orderElement

		el := q.depthList.Set(order.Price, &unit)
		q.priceList[order.Price.String()] = el

		q.totalOrders++
		q.depths++
	}
}

// removeOrder removes an order from the queue by price and ID.
// It also cleans up the price unit if it becomes empty.
func (q *queue) removeOrder(price decimal.Decimal, id string) {
	skipElement, ok := q.priceList[price.String()]
	if ok {
		unit, _ := skipElement.Value.(*priceUnit)

		orderElement, ok := q.orders[id]
		order, _ := orderElement.Value.(*Order)
		if ok {
			unit.list.Remove(orderElement)
			unit.totalSize = unit.totalSize.Sub(order.Size)
			delete(q.orders, id)
			q.totalOrders--
		}

		if unit.list.Len() == 0 {
			q.depthList.RemoveElement(skipElement)
			delete(q.priceList, order.Price.String())
			q.depths--
		}

	}
}

// updateOrderSize updates the size of an order in-place.
// This is used when the size is decreased, preserving the order's priority.
func (q *queue) updateOrderSize(id string, newSize decimal.Decimal) {
	el, ok := q.orders[id]
	if !ok {
		return
	}
	order, _ := el.Value.(*Order)

	skipElement, ok := q.priceList[order.Price.String()]
	if ok {
		unit, _ := skipElement.Value.(*priceUnit)
		diff := order.Size.Sub(newSize)
		unit.totalSize = unit.totalSize.Sub(diff)
		order.Size = newSize
	}
}

// peekHeadOrder returns the order at the front of the queue (best price) without removing it.
func (q *queue) peekHeadOrder() *Order {
	el := q.depthList.Front()
	if el == nil {
		return nil
	}

	unit, _ := el.Value.(*priceUnit)
	order, _ := unit.list.Front().Value.(*Order)
	return order
}

// popHeadOrder removes and returns the order at the front of the queue.
func (q *queue) popHeadOrder() *Order {
	ord := q.peekHeadOrder()

	if ord != nil {
		q.removeOrder(ord.Price, ord.ID)
	}

	return ord
}

// orderCount returns the total number of orders in the queue.
func (q *queue) orderCount() int64 {
	return q.totalOrders
}

// depthCount returns the number of price levels in the queue.
func (q *queue) depthCount() int64 {
	return q.depths
}

// depth returns the order book depth up to the specified limit.
func (q *queue) depth(limit uint32) []*DepthItem {
	result := make([]*DepthItem, 0, limit)

	el := q.depthList.Front()

	var i uint32 = 0
	for i < limit && el != nil {
		unit, _ := el.Value.(*priceUnit)
		order, _ := unit.list.Front().Value.(*Order)
		d := DepthItem{
			ID:    i,
			Price: order.Price,
			Size:  unit.totalSize,
		}

		result = append(result, &d)

		el = el.Next()
		i++
	}

	return result
}
