package match

import (
	"sync"

	"github.com/quagmt/udecimal"

	"github.com/0x5487/matching-engine/protocol"
	"github.com/0x5487/matching-engine/structure"
)

// UpdateEvent represents a change in price or size at a specific price level.
type UpdateEvent struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// priceUnit is the set of orders resting at one price, in time priority.
type priceUnit struct {
	// totalSize is the visible size of the level: what depth reports and what a
	// taker sees before any iceberg reloads.
	totalSize udecimal.Decimal
	// hiddenSize is the iceberg size behind the visible part. It is not depth, but
	// it is size a taker can reach, since a replenish brings it forward.
	hiddenSize udecimal.Decimal
	head       *Order
	tail       *Order
	count      int64
}

// priceUnitPool recycles the price units of levels that empty. A recycled unit is
// reset in acquirePriceUnit, so nothing of the level it held survives.
var priceUnitPool = sync.Pool{
	New: func() any {
		return &priceUnit{}
	},
}

// acquirePriceUnit returns an empty price unit holding order as its only order.
func acquirePriceUnit(order *Order) *priceUnit {
	unit, ok := priceUnitPool.Get().(*priceUnit)
	if !ok {
		unit = &priceUnit{}
	}
	*unit = priceUnit{
		totalSize:  order.Size,
		hiddenSize: order.HiddenSize,
		head:       order,
		tail:       order,
		count:      1,
	}
	return unit
}

// releasePriceUnit returns a unit whose level has emptied to the pool.
func releasePriceUnit(unit *priceUnit) {
	*unit = priceUnit{}
	priceUnitPool.Put(unit)
}

type queue struct {
	side          Side
	totalOrders   int64
	depths        int64
	orderedPrices *structure.PooledSkiplist // Ordered price levels
	priceList     map[priceKey]*priceUnit   // Quick lookup by canonical price; see priceKeyOf
	orders        map[string]*Order
}

const (
	defaultPriceCapacity = 3000
	defaultSkiplistSeed  = int64(1)
)

// newBuyerQueue creates a new queue for buy orders (bids).
// The orders are sorted by price in descending order (highest price first).
func newBuyerQueue(seeds ...int64) *queue {
	seed := defaultSkiplistSeed
	if len(seeds) > 0 && seeds[0] != 0 {
		seed = seeds[0]
	}
	return &queue{
		side: Buy,
		orderedPrices: structure.NewPooledSkiplistWithOptions(
			defaultPriceCapacity,
			seed,
			structure.SkiplistOptions{Descending: true}, // Highest first
		),
		priceList: make(map[priceKey]*priceUnit),
		orders:    make(map[string]*Order),
	}
}

// newSellerQueue creates a new queue for sell orders (asks).
// The orders are sorted by price in ascending order (lowest price first).
func newSellerQueue(seeds ...int64) *queue {
	seed := defaultSkiplistSeed
	if len(seeds) > 0 && seeds[0] != 0 {
		seed = seeds[0]
	}
	return &queue{
		side: Sell,
		orderedPrices: structure.NewPooledSkiplist(
			defaultPriceCapacity,
			seed,
		), // Default: Lowest first
		priceList: make(map[priceKey]*priceUnit),
		orders:    make(map[string]*Order),
	}
}

// order finds an order by its ID.
func (q *queue) order(id string) *Order {
	return q.orders[id]
}

// insertOrder inserts an order into the end of the queue for its price level.
func (q *queue) insertOrder(order *Order) {
	q.doInsert(order, false)
}

// pushFront inserts an order into the front of the queue for its price level.
func (q *queue) pushFront(order *Order) {
	q.doInsert(order, true)
}

//nolint:revive // flag-parameter: internal method used by insertOrder and pushFront
func (q *queue) doInsert(order *Order, isFront bool) {
	key := priceKeyOf(order.Price)
	unit, ok := q.priceList[key]
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
		unit.hiddenSize = unit.hiddenSize.Add(order.HiddenSize)
		unit.count++
		q.orders[order.ID] = order
		q.totalOrders++
	} else {
		unit := acquirePriceUnit(order)
		order.next = nil
		order.prev = nil

		q.orders[order.ID] = order
		q.priceList[key] = unit
		q.orderedPrices.MustInsert(order.Price)

		q.totalOrders++
		q.depths++
	}
}

// removeOrder removes an order from the queue by price and ID.
// It also cleans up the price unit if it becomes empty.
func (q *queue) removeOrder(price udecimal.Decimal, id string) {
	key := priceKeyOf(price)
	unit, ok := q.priceList[key]
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
	unit.hiddenSize = unit.hiddenSize.Sub(order.HiddenSize)
	unit.count--
	delete(q.orders, id)
	q.totalOrders--

	if unit.count == 0 {
		q.orderedPrices.Delete(price)
		delete(q.priceList, key)
		releasePriceUnit(unit)
		q.depths--
	}
}

// resizeOrder sets the visible and hidden size of an order in place, keeping its
// place in the queue, and moves the level totals by the same amount. This is used
// when an amend shrinks an order without moving it. The caller must not have
// changed order.Size or order.HiddenSize before calling this.
func (q *queue) resizeOrder(id string, newSize, newHidden udecimal.Decimal) {
	order, ok := q.orders[id]
	if !ok {
		return
	}

	unit, ok := q.priceList[priceKeyOf(order.Price)]
	if ok {
		unit.totalSize = unit.totalSize.Sub(order.Size).Add(newSize)
		unit.hiddenSize = unit.hiddenSize.Sub(order.HiddenSize).Add(newHidden)
	}
	order.Size = newSize
	order.HiddenSize = newHidden
}

// levelSize returns the visible size at price, zero when no order rests there.
func (q *queue) levelSize(price udecimal.Decimal) udecimal.Decimal {
	unit, ok := q.priceList[priceKeyOf(price)]
	if !ok {
		return udecimal.Zero
	}
	return unit.totalSize
}

// bestPrice returns the price at the front of the queue, false when it is empty.
func (q *queue) bestPrice() (udecimal.Decimal, bool) {
	return q.orderedPrices.Min()
}

// peekHeadOrder returns the order at the front of the queue (best price) without removing it.
func (q *queue) peekHeadOrder() *Order {
	bestPrice, ok := q.orderedPrices.Min()
	if !ok {
		return nil
	}

	unit, ok := q.priceList[priceKeyOf(bestPrice)]
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
		unit, ok := q.priceList[priceKeyOf(price)]
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
	priceList map[priceKey]*priceUnit
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
	return price, it.priceList[priceKeyOf(price)]
}
