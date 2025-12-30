package match

import (
	"time"

	"github.com/0x5487/matching-engine/protocol"
	"github.com/0x5487/matching-engine/structure"
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

type queue struct {
	side          Side
	totalOrders   int64
	depths        int64
	orderedPrices *structure.PooledSkiplist       // Ordered price levels
	priceList     map[udecimal.Decimal]*priceUnit // Quick lookup by price
	orders        map[string]*Order
}

const defaultPriceCapacity = 3000

// NewBuyerQueue creates a new queue for buy orders (bids).
// The orders are sorted by price in descending order (highest price first).
func NewBuyerQueue() *queue {
	return &queue{
		side: Buy,
		orderedPrices: structure.NewPooledSkiplistWithOptions(
			defaultPriceCapacity,
			time.Now().UnixNano(),
			structure.SkiplistOptions{Descending: true}, // Highest first
		),
		priceList: make(map[udecimal.Decimal]*priceUnit),
		orders:    make(map[string]*Order),
	}
}

// NewSellerQueue creates a new queue for sell orders (asks).
// The orders are sorted by price in ascending order (lowest price first).
func NewSellerQueue() *queue {
	return &queue{
		side: Sell,
		orderedPrices: structure.NewPooledSkiplist(
			defaultPriceCapacity,
			time.Now().UnixNano(),
		), // Default: Lowest first
		priceList: make(map[udecimal.Decimal]*priceUnit),
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
	unit, ok := q.priceList[order.Price]
	if ok {
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
		q.priceList[order.Price] = unit
		q.orderedPrices.MustInsert(order.Price)

		q.totalOrders++
		q.depths++
	}
}

// removeOrder removes an order from the queue by price and ID.
// It also cleans up the price unit if it becomes empty.
func (q *queue) removeOrder(price udecimal.Decimal, id string) {
	unit, ok := q.priceList[price]
	if !ok {
		return
	}

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
		q.orderedPrices.Delete(price)
		delete(q.priceList, price)
		q.depths--
	}
}

// updateOrderSize updates the size of an order in-place.
// This is used when the size is decreased, preserving the order's priority.
// The caller should NOT update order.Size before calling this, or this method will not update unit.totalSize correctly.
func (q *queue) updateOrderSize(id string, newSize udecimal.Decimal) {
	order, ok := q.orders[id]
	if !ok {
		return
	}

	unit, ok := q.priceList[order.Price]
	if ok {
		diff := order.Size.Sub(newSize)
		unit.totalSize = unit.totalSize.Sub(diff)
		order.Size = newSize
	}
}

// peekHeadOrder returns the order at the front of the queue (best price) without removing it.
func (q *queue) peekHeadOrder() *Order {
	bestPrice, ok := q.orderedPrices.Min()
	if !ok {
		return nil
	}

	unit, ok := q.priceList[bestPrice]
	if !ok {
		return nil
	}
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

// toSnapshot serializes the queue into a slice of *Order pointers.
func (q *queue) toSnapshot() []*Order {
	snapshots := make([]*Order, 0, q.totalOrders)

	// Iterate over all price levels
	iter := q.orderedPrices.Iterator()
	for iter.Valid() {
		price := iter.Price()
		unit, ok := q.priceList[price]
		if !ok {
			iter.Next()
			continue
		}

		// Iterate over all orders at this price level
		order := unit.head
		for order != nil {
			o := &Order{
				ID:           order.ID,
				Side:         order.Side,
				Price:        order.Price,
				Size:         order.Size,
				UserID:       order.UserID,
				Type:         order.Type,
				Timestamp:    order.Timestamp,
				VisibleLimit: order.VisibleLimit,
				HiddenSize:   order.HiddenSize,
			}
			snapshots = append(snapshots, o)
			order = order.next
		}

		iter.Next()
	}

	return snapshots
}

// depth returns the order book depth up to the specified limit.
func (q *queue) depth(limit uint32) []*protocol.DepthItem {
	result := make([]*protocol.DepthItem, 0, limit)
	it := q.priceIterator()
	count := uint32(0)
	for it.Valid() && count < limit {
		price, unit := it.PriceUnit()
		d := &protocol.DepthItem{
			Price: price.String(),
			Size:  unit.totalSize.String(),
			Count: unit.count,
		}
		result = append(result, d)
		count++
		it.Next()
	}
	return result
}

// priceIterator returns an iterator over price levels.
// Used for FOK order validation.
func (q *queue) priceIterator() *queuePriceIterator {
	iter := q.orderedPrices.Iterator()
	return &queuePriceIterator{
		skipIter:  iter,
		priceList: q.priceList,
	}
}

// queuePriceIterator iterates over price levels in the queue.
type queuePriceIterator struct {
	skipIter  *structure.SkiplistIterator
	priceList map[udecimal.Decimal]*priceUnit
}

// Valid returns true if the iterator points to a valid price level.
func (it *queuePriceIterator) Valid() bool {
	return it.skipIter.Valid()
}

// Next advances the iterator to the next price level.
func (it *queuePriceIterator) Next() {
	it.skipIter.Next()
}

// PriceUnit returns the current price and its priceUnit.
func (it *queuePriceIterator) PriceUnit() (udecimal.Decimal, *priceUnit) {
	if !it.skipIter.Valid() {
		return udecimal.Zero, nil
	}
	price := it.skipIter.Price()
	return price, it.priceList[price]
}
