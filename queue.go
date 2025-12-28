package match

import (
	"github.com/huandu/skiplist"
	"github.com/quagmt/udecimal"
)

type UpdateEvent struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

type priceUnit struct {
	totalSize udecimal.Decimal
	head      *Order
	tail      *Order
	count     int64
}

type DepthItem struct {
	ID    uint32
	Price udecimal.Decimal
	Size  udecimal.Decimal
}

type queue struct {
	side        Side
	totalOrders int64
	depths      int64
	depthList   *skiplist.SkipList
	priceList   map[udecimal.Decimal]*skiplist.Element
	orders      map[string]*Order
}

// NewBuyerQueue creates a new queue for buy orders (bids).
// The orders are sorted by price in descending order (highest price first).
func NewBuyerQueue() *queue {
	return &queue{
		side: Buy,
		depthList: skiplist.New(skiplist.GreaterThanFunc(func(lhs, rhs any) int {
			d1, _ := lhs.(udecimal.Decimal)
			d2, _ := rhs.(udecimal.Decimal)

			if d1.LessThan(d2) {
				return 1
			} else if d1.GreaterThan(d2) {
				return -1
			}

			return 0
		})),
		priceList: make(map[udecimal.Decimal]*skiplist.Element),
		orders:    make(map[string]*Order),
	}
}

// NewSellerQueue creates a new queue for sell orders (asks).
// The orders are sorted by price in ascending order (lowest price first).
func NewSellerQueue() *queue {
	return &queue{
		side: Sell,
		depthList: skiplist.New(skiplist.GreaterThanFunc(func(lhs, rhs any) int {
			d1, _ := lhs.(udecimal.Decimal)
			d2, _ := rhs.(udecimal.Decimal)

			if d1.GreaterThan(d2) {
				return 1
			} else if d1.LessThan(d2) {
				return -1
			}

			return 0
		})),
		priceList: make(map[udecimal.Decimal]*skiplist.Element),
		orders:    make(map[string]*Order),
	}
}

// order finds an order by its ID.
func (q *queue) order(id string) *Order {
	return q.orders[id]
}

// insertOrder inserts an order into the queue.
// It updates the price list and depth list.
func (q *queue) insertOrder(order *Order, isFront bool) {
	el, ok := q.priceList[order.Price]
	if ok {
		unit, _ := el.Value.(*priceUnit)
		if isFront {
			// Push Front
			order.next = unit.head
			order.prev = nil
			if unit.head != nil {
				unit.head.prev = order
			}
			unit.head = order
			if unit.tail == nil {
				unit.tail = order
			}
		} else {
			// Push Back
			order.prev = unit.tail
			order.next = nil
			if unit.tail != nil {
				unit.tail.next = order
			}
			unit.tail = order
			if unit.head == nil {
				unit.head = order
			}
		}

		unit.totalSize = unit.totalSize.Add(order.Size)
		unit.count++
		q.orders[order.ID] = order
		q.totalOrders++
	} else {
		unit := &priceUnit{
			head:      order,
			tail:      order,
			totalSize: order.Size,
			count:     1,
		}
		order.next = nil
		order.prev = nil

		q.orders[order.ID] = order

		el := q.depthList.Set(order.Price, unit)
		q.priceList[order.Price] = el

		q.totalOrders++
		q.depths++
	}
}

// removeOrder removes an order from the queue by price and ID.
// It also cleans up the price unit if it becomes empty.
func (q *queue) removeOrder(price udecimal.Decimal, id string) {
	skipElement, ok := q.priceList[price]
	if !ok {
		return
	}
	unit, _ := skipElement.Value.(*priceUnit)

	order, ok := q.orders[id]
	if !ok {
		return
	}

	// Remove from linked list
	if order.prev != nil {
		order.prev.next = order.next
	} else {
		unit.head = order.next
	}

	if order.next != nil {
		order.next.prev = order.prev
	} else {
		unit.tail = order.prev
	}

	// Clear pointers to avoid leaks and for safety in pool
	order.next = nil
	order.prev = nil

	unit.totalSize = unit.totalSize.Sub(order.Size)
	unit.count--
	delete(q.orders, id)
	q.totalOrders--

	if unit.count == 0 {
		q.depthList.RemoveElement(skipElement)
		delete(q.priceList, price)
		q.depths--
	}
}

// updateOrderSize updates the size of an order in-place.
// This is used when the size is decreased, preserving the order's priority.
func (q *queue) updateOrderSize(id string, newSize udecimal.Decimal) {
	order, ok := q.orders[id]
	if !ok {
		return
	}

	skipElement, ok := q.priceList[order.Price]
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
	return unit.head
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

// toSnapshot serializes the queue into a slice of Order structs.
// It iterates through the skip list (price levels) and then the linked list (orders) to preserve priority.
func (q *queue) toSnapshot() []Order {
	snapshots := make([]Order, 0, q.totalOrders)

	// Iterate over all price levels
	elem := q.depthList.Front()
	for elem != nil {
		unit := elem.Value.(*priceUnit)

		// Iterate over all orders at this price level
		order := unit.head
		for order != nil {
			snapshots = append(snapshots, Order{
				ID:        order.ID,
				Side:      order.Side,
				Price:     order.Price,
				Size:      order.Size,
				UserID:    order.UserID,
				Type:      order.Type,
				Timestamp: order.Timestamp,
			})
			order = order.next
		}

		elem = elem.Next()
	}

	return snapshots
}

// depth returns the order book depth up to the specified limit.
func (q *queue) depth(limit uint32) []*DepthItem {
	result := make([]*DepthItem, 0, limit)

	el := q.depthList.Front()

	var i uint32 = 0
	for i < limit && el != nil {
		unit, _ := el.Value.(*priceUnit)
		d := DepthItem{
			ID:    i,
			Price: unit.head.Price,
			Size:  unit.totalSize,
		}

		result = append(result, &d)

		el = el.Next()
		i++
	}

	return result
}
