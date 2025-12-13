package match

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nite-coder/blackbear/pkg/cast"
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
	FOK      OrderType = "fok"       // Fill Or Kill
	IOC      OrderType = "ioc"       // Immediate Or Cancel
	PostOnly OrderType = "post_only" // be maker order only
	Cancel   OrderType = "cancel"    // the order has been canceled
)

type Order struct {
	ID        string          `json:"id"`
	MarketID  string          `json:"market_id"`
	Side      Side            `json:"side"`
	Price     decimal.Decimal `json:"price"`
	Size      decimal.Decimal `json:"size"`
	Type      OrderType       `json:"type"`
	UserID    int64           `json:"user_id"`
	CreatedAt time.Time       `json:"created_at"`
}

type LogType string

const (
	LogTypeOpen   LogType = "open"
	LogTypeMatch  LogType = "match"
	LogTypeCancel LogType = "cancel"
	LogTypeAmend  LogType = "amend"
	LogTypeReject LogType = "reject"
)

// BookLog represents an event in the order book.
// When OrderBookID > 0, it indicates that the order book state has changed (e.g., Open, Match, Cancel, Amend).
// When OrderBookID == 0, the event does not affect order book state (e.g., Reject).
type BookLog struct {
	OrderBookID  uint64          `json:"id"`
	Type         LogType         `json:"type"` // Event type: open, match, cancel, amend, reject
	MarketID     string          `json:"market_id"`
	Side         Side            `json:"side"`
	Price        decimal.Decimal `json:"price"`
	Size         decimal.Decimal `json:"size"`
	OldPrice     decimal.Decimal `json:"old_price,omitempty"`
	OldSize      decimal.Decimal `json:"old_size,omitempty"`
	OrderID      string          `json:"order_id"`
	UserID       int64           `json:"user_id"`
	OrderType    OrderType       `json:"order_type,omitempty"` // Order type: limit, market, ioc, fok
	MakerOrderID string          `json:"maker_order_id,omitempty"`
	MakerUserID  int64           `json:"maker_user_id,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

var bookLogPool = sync.Pool{
	New: func() interface{} {
		return new(BookLog)
	},
}

func acquireBookLog() *BookLog {
	return bookLogPool.Get().(*BookLog)
}

func releaseBookLog(log *BookLog) {
	// Reset structure to zero values.
	// For decimal.Decimal, the zero value (nil internal pointer) represents 0, which is valid.
	*log = BookLog{}
	bookLogPool.Put(log)
}

type Response struct {
	Error error
	Data  any
}

type Message struct {
	Action  string
	Payload any
	Resp    chan *Response
}

type OrderBookUpdateEvent struct {
	Bids []*UpdateEvent
	Asks []*UpdateEvent
	Time time.Time
}

type Depth struct {
	UpdateID uint64       `json:"update_id"`
	Asks     []*DepthItem `json:"asks"`
	Bids     []*DepthItem `json:"bids"`
}

type AmendRequest struct {
	OrderID  string
	NewPrice decimal.Decimal
	NewSize  decimal.Decimal
}

// DepthChange represents a change in the order book depth.
type DepthChange struct {
	Side     Side
	Price    decimal.Decimal
	SizeDiff decimal.Decimal
}

// CalculateDepthChange calculates the depth change based on the book log.
// It returns a DepthChange struct indicating which side and price level should be updated.
// Note: For LogTypeMatch, the side returned is the Maker's side (opposite of the log's side).
func CalculateDepthChange(log *BookLog) DepthChange {
	switch log.Type {
	case LogTypeOpen:
		return DepthChange{
			Side:     log.Side,
			Price:    log.Price,
			SizeDiff: log.Size,
		}
	case LogTypeCancel:
		return DepthChange{
			Side:     log.Side,
			Price:    log.Price,
			SizeDiff: log.Size.Neg(),
		}
	case LogTypeMatch:
		// Match reduces liquidity from the Maker side.
		// The log.Side is the Taker's side, so we update the opposite side.
		makerSide := Buy
		if log.Side == Buy {
			makerSide = Sell
		}
		return DepthChange{
			Side:     makerSide,
			Price:    log.Price,
			SizeDiff: log.Size.Neg(),
		}
	case LogTypeAmend:
		// Scenario 1: Priority Lost (Price changed OR Size increased)
		// Logic: The order is removed from the book. The new order will be handled by a subsequent LogTypeOpen or LogTypeMatch.
		// So we only need to remove the OldSize from OldPrice.
		if !log.OldPrice.Equal(log.Price) || log.Size.GreaterThan(log.OldSize) {
			return DepthChange{
				Side:     log.Side,
				Price:    log.OldPrice,
				SizeDiff: log.OldSize.Neg(),
			}
		}

		// Scenario 2: Priority Kept (Price same AND Size decreased)
		// Logic: Update in-place. The difference is (NewSize - OldSize).
		return DepthChange{
			Side:     log.Side,
			Price:    log.Price,
			SizeDiff: log.Size.Sub(log.OldSize),
		}
	case LogTypeReject:
		// Rejected orders never entered the book, so no depth change.
		return DepthChange{}
	}

	return DepthChange{}
}

// OrderBook type
type OrderBook struct {
	id               atomic.Uint64
	isShutdown       atomic.Bool
	bidQueue         *queue
	askQueue         *queue
	orderChan        chan *Order
	amendChan        chan *AmendRequest
	cancelChan       chan string
	depthChan        chan *Message
	done             chan struct{}
	shutdownComplete chan struct{}
	publishTrader    PublishTrader
}

// NewOrderBook creates a new order book instance.
func NewOrderBook(publishTrader PublishTrader) *OrderBook {
	return &OrderBook{
		bidQueue:         NewBuyerQueue(),
		askQueue:         NewSellerQueue(),
		orderChan:        make(chan *Order, 10000),
		amendChan:        make(chan *AmendRequest, 10000),
		cancelChan:       make(chan string, 10000),
		depthChan:        make(chan *Message, 100),
		done:             make(chan struct{}),
		shutdownComplete: make(chan struct{}),
		publishTrader:    publishTrader,
	}
}

// AddOrder submits an order to the order book asynchronously.
// Returns ErrShutdown if the order book is shutting down.
func (book *OrderBook) AddOrder(ctx context.Context, order *Order) error {
	if book.isShutdown.Load() {
		return ErrShutdown
	}

	if len(order.Type) == 0 || len(order.ID) == 0 {
		return ErrInvalidParam
	}

	select {
	case book.orderChan <- order:
		return nil
	case <-ctx.Done():
		return ErrTimeout
	}
}

// AmendOrder submits a request to modify an existing order asynchronously.
func (book *OrderBook) AmendOrder(ctx context.Context, id string, newPrice decimal.Decimal, newSize decimal.Decimal) error {
	if len(id) == 0 || newSize.LessThanOrEqual(decimal.Zero) || newPrice.LessThanOrEqual(decimal.Zero) {
		return ErrInvalidParam
	}

	select {
	case book.amendChan <- &AmendRequest{OrderID: id, NewPrice: newPrice, NewSize: newSize}:
		return nil
	case <-ctx.Done():
		return ErrTimeout
	}
}

// CancelOrder submits a cancellation request for an order asynchronously.
func (book *OrderBook) CancelOrder(ctx context.Context, id string) error {
	if len(id) == 0 {
		return nil
	}

	select {
	case book.cancelChan <- id:
		return nil
	case <-ctx.Done():
		return ErrTimeout
	}
}

// Depth returns the current depth of the order book up to the specified limit.
func (book *OrderBook) Depth(limit uint32) (*Depth, error) {
	if limit == 0 {
		return nil, ErrInvalidParam
	}

	msg := &Message{
		Action:  "depth",
		Payload: limit,
		Resp:    make(chan *Response),
	}

	select {
	case book.depthChan <- msg:
		resp := <-msg.Resp
		if resp.Error != nil {
			return nil, resp.Error
		}

		if resp.Data != nil {
			result, ok := resp.Data.(*Depth)
			if ok {
				return result, nil
			}
		}

		return nil, nil
	case <-time.After(time.Second):
		return nil, ErrTimeout
	}
}

// Start starts the order book loop to process orders, cancellations, and depth requests.
// Returns nil when Shutdown() is called and all pending orders are drained.
func (book *OrderBook) Start() error {
	for {
		select {
		case <-book.done:
			return book.drain()
		case order := <-book.orderChan:
			book.addOrder(order)
		case req := <-book.amendChan:
			book.amendOrder(req)
		case orderID := <-book.cancelChan:
			book.cancelOrder(orderID)
		case msg := <-book.depthChan:
			limit, _ := cast.ToUint32(msg.Payload)
			result := book.depth(limit)
			resp := Response{
				Error: nil,
				Data:  result,
			}

			msg.Resp <- &resp
		}
	}
}

// Shutdown signals the order book to stop accepting new orders and waits for all pending orders to be processed.
// The method blocks until all orders are drained or the context is cancelled/timed out.
// Returns nil if shutdown completed successfully, or ctx.Err() if the context was cancelled.
func (book *OrderBook) Shutdown(ctx context.Context) error {
	if book.isShutdown.CompareAndSwap(false, true) {
		close(book.done)
	}

	select {
	case <-book.shutdownComplete:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// drain processes all remaining orders in the channels before returning.
func (book *OrderBook) drain() error {
	defer close(book.shutdownComplete)

	for {
		select {
		case order := <-book.orderChan:
			book.addOrder(order)
		case req := <-book.amendChan:
			book.amendOrder(req)
		case orderID := <-book.cancelChan:
			book.cancelOrder(orderID)
		default:
			// All channels empty, shutdown complete
			return nil
		}
	}
}

// addOrder processes the addition of an order based on its type.
func (book *OrderBook) addOrder(order *Order) {
	var logs []*BookLog

	switch order.Type {
	case Limit:
		logs = book.handleLimitOrder(order)
	case FOK:
		logs = book.handleFOKOrder(order)
	case IOC:
		logs = book.handleIOCOrder(order)
	case PostOnly:
		logs = book.handlePostOnlyOrder(order)
	case Market:
		logs = book.handleMarketOrder(order)
	}

	if len(logs) > 0 {
		book.publishTrader.Publish(logs...)
		for _, log := range logs {
			releaseBookLog(log)
		}
	}
}

// amendOrder processes the modification of an order.
func (book *OrderBook) amendOrder(req *AmendRequest) {
	var myQueue *queue
	order := book.askQueue.order(req.OrderID)
	if order != nil {
		myQueue = book.askQueue
	} else {
		order = book.bidQueue.order(req.OrderID)
		if order != nil {
			myQueue = book.bidQueue
		}
	}

	if order == nil {
		return
	}

	oldPrice := order.Price
	oldSize := order.Size

	// Scenario 1: Price changed OR Size increased -> Priority Lost (Remove and Re-insert)
	if !oldPrice.Equal(req.NewPrice) || req.NewSize.GreaterThan(oldSize) {
		myQueue.removeOrder(oldPrice, req.OrderID)

		order.Price = req.NewPrice
		order.Size = req.NewSize

		// Publish Amend Log FIRST to establish the new state
		book.id.Add(1)
		log := acquireBookLog()
		log.OrderBookID = book.id.Load()
		log.Type = LogTypeAmend
		log.MarketID = order.MarketID
		log.Side = order.Side
		log.Price = req.NewPrice
		log.Size = req.NewSize
		log.OldPrice = oldPrice
		log.OldSize = oldSize
		log.OrderID = order.ID
		log.UserID = order.UserID
		log.OrderType = order.Type
		log.CreatedAt = time.Now().UTC()
		book.publishTrader.Publish(log)
		releaseBookLog(log)

		// Trigger Matching Logic (Similar to handleLimitOrder)
		var logs []*BookLog
		switch order.Type {
		case Limit:
			logs = book.handleLimitOrder(order)
		case PostOnly:
			logs = book.handlePostOnlyOrder(order)
		default:
			logs = book.handleLimitOrder(order)
		}

		if len(logs) > 0 {
			book.publishTrader.Publish(logs...)
			for _, log := range logs {
				releaseBookLog(log)
			}
		}

	} else {
		// Scenario 2: Price same AND Size decreased -> Priority Kept (Update in-place)
		if req.NewSize.LessThan(oldSize) {
			myQueue.updateOrderSize(req.OrderID, req.NewSize)
		}

		// Publish Amend Log
		book.id.Add(1)
		log := acquireBookLog()
		log.OrderBookID = book.id.Load()
		log.Type = LogTypeAmend
		log.MarketID = order.MarketID
		log.Side = order.Side
		log.Price = req.NewPrice
		log.Size = req.NewSize
		log.OldPrice = oldPrice
		log.OldSize = oldSize
		log.OrderID = order.ID
		log.UserID = order.UserID
		log.OrderType = order.Type
		log.CreatedAt = time.Now().UTC()

		book.publishTrader.Publish(log)
		releaseBookLog(log)
	}
}

// cancelOrder processes the cancellation of an order.
func (book *OrderBook) cancelOrder(id string) {
	order := book.askQueue.order(id)
	if order != nil {
		book.askQueue.removeOrder(order.Price, id)
		book.id.Add(1)
		log := acquireBookLog()
		log.OrderBookID = book.id.Load()
		log.Type = LogTypeCancel
		log.MarketID = order.MarketID
		log.Side = order.Side
		log.Price = order.Price
		log.Size = order.Size
		log.OrderID = order.ID
		log.UserID = order.UserID
		log.OrderType = order.Type
		log.CreatedAt = time.Now().UTC()
		book.publishTrader.Publish(log)
		releaseBookLog(log)
		return
	}

	order = book.bidQueue.order(id)
	if order != nil {
		book.bidQueue.removeOrder(order.Price, id)
		book.id.Add(1)
		log := acquireBookLog()
		log.OrderBookID = book.id.Load()
		log.Type = LogTypeCancel
		log.MarketID = order.MarketID
		log.Side = order.Side
		log.Price = order.Price
		log.Size = order.Size
		log.OrderID = order.ID
		log.UserID = order.UserID
		log.OrderType = order.Type
		log.CreatedAt = time.Now().UTC()
		book.publishTrader.Publish(log)
		releaseBookLog(log)
		return
	}
}

// depth returns the snapshot of the order book depth.
func (book *OrderBook) depth(limit uint32) *Depth {
	return &Depth{
		UpdateID: book.id.Load(),
		Asks:     book.askQueue.depth(limit),
		Bids:     book.bidQueue.depth(limit),
	}
}

// handleLimitOrder handles Limit orders. It matches against the opposite queue and adds the remaining size to the book.
func (book *OrderBook) handleLimitOrder(order *Order) []*BookLog {
	var myQueue, targetQueue *queue
	if order.Side == Buy {
		myQueue = book.bidQueue
		targetQueue = book.askQueue
	} else {
		myQueue = book.askQueue
		targetQueue = book.bidQueue
	}

	// Pre-allocate slice and cache timestamp
	logs := make([]*BookLog, 0, 8)
	now := time.Now().UTC()

	for {
		// Peek first to check if matching is possible
		tOrd := targetQueue.getHeadOrder()

		if tOrd == nil {
			myQueue.insertOrder(order, false)
			book.id.Add(1)
			log := acquireBookLog()
			log.OrderBookID = book.id.Load()
			log.Type = LogTypeOpen
			log.MarketID = order.MarketID
			log.Side = order.Side
			log.Price = order.Price
			log.Size = order.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.CreatedAt = now
			logs = append(logs, log)
			return logs
		}

		// Check price condition before popping
		if order.Side == Buy && order.Price.LessThan(tOrd.Price) ||
			order.Side == Sell && order.Price.GreaterThan(tOrd.Price) {
			// Price doesn't match, add order to book without popping
			myQueue.insertOrder(order, false)
			book.id.Add(1)
			log := acquireBookLog()
			log.OrderBookID = book.id.Load()
			log.Type = LogTypeOpen
			log.MarketID = order.MarketID
			log.Side = order.Side
			log.Price = order.Price
			log.Size = order.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.CreatedAt = now
			logs = append(logs, log)
			return logs
		}

		// Price matches, now actually pop the order for matching
		tOrd = targetQueue.popHeadOrder()

		if order.Size.GreaterThanOrEqual(tOrd.Size) {
			book.id.Add(1)
			log := acquireBookLog()
			log.OrderBookID = book.id.Load()
			log.Type = LogTypeMatch
			log.MarketID = order.MarketID
			log.Side = order.Side
			log.Price = tOrd.Price
			log.Size = tOrd.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.MakerOrderID = tOrd.ID
			log.MakerUserID = tOrd.UserID
			log.CreatedAt = now
			logs = append(logs, log)
			order.Size = order.Size.Sub(tOrd.Size)

			if order.Size.Equal(decimal.Zero) {
				break
			}
		} else {
			book.id.Add(1)
			log := acquireBookLog()
			log.OrderBookID = book.id.Load()
			log.Type = LogTypeMatch
			log.MarketID = order.MarketID
			log.Side = order.Side
			log.Price = tOrd.Price
			log.Size = order.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.MakerOrderID = tOrd.ID
			log.MakerUserID = tOrd.UserID
			log.CreatedAt = now
			logs = append(logs, log)
			tOrd.Size = tOrd.Size.Sub(order.Size)
			targetQueue.insertOrder(tOrd, true)

			break
		}
	}

	return logs
}

// handleIOCOrder handles Immediate Or Cancel orders. It matches as much as possible and cancels the rest.
func (book *OrderBook) handleIOCOrder(order *Order) []*BookLog {
	var targetQueue *queue
	if order.Side == Buy {
		targetQueue = book.askQueue
	} else {
		targetQueue = book.bidQueue
	}

	// Pre-allocate slice and cache timestamp
	logs := make([]*BookLog, 0, 8)
	now := time.Now().UTC()

	for {
		// Peek first to check if matching is possible
		tOrd := targetQueue.getHeadOrder()

		if tOrd == nil {
			// IOC Cancel (No match) - Reject does not change order book state
			log := acquireBookLog()
			log.Type = LogTypeReject
			log.MarketID = order.MarketID
			log.Side = order.Side
			log.Price = order.Price
			log.Size = order.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.CreatedAt = now
			logs = append(logs, log)
			return logs
		}

		if order.Side == Buy && order.Price.LessThan(tOrd.Price) ||
			order.Side == Sell && order.Price.GreaterThan(tOrd.Price) {
			// IOC Cancel (Price mismatch) - Reject does not change order book state
			log := acquireBookLog()
			log.Type = LogTypeReject
			log.MarketID = order.MarketID
			log.Side = order.Side
			log.Price = order.Price
			log.Size = order.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.CreatedAt = now
			logs = append(logs, log)
			return logs
		}

		// Price matches, now actually pop the order for matching
		tOrd = targetQueue.popHeadOrder()

		if order.Size.GreaterThanOrEqual(tOrd.Size) {
			book.id.Add(1)
			log := acquireBookLog()
			log.OrderBookID = book.id.Load()
			log.Type = LogTypeMatch
			log.MarketID = order.MarketID
			log.Side = order.Side
			log.Price = tOrd.Price
			log.Size = tOrd.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.MakerOrderID = tOrd.ID
			log.MakerUserID = tOrd.UserID
			log.CreatedAt = now
			logs = append(logs, log)
			order.Size = order.Size.Sub(tOrd.Size)

			if order.Size.Equal(decimal.Zero) {
				break
			}
		} else {
			book.id.Add(1)
			log := acquireBookLog()
			log.OrderBookID = book.id.Load()
			log.Type = LogTypeMatch
			log.MarketID = order.MarketID
			log.Side = order.Side
			log.Price = tOrd.Price
			log.Size = order.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.MakerOrderID = tOrd.ID
			log.MakerUserID = tOrd.UserID
			log.CreatedAt = now
			logs = append(logs, log)
			tOrd.Size = tOrd.Size.Sub(order.Size)
			targetQueue.insertOrder(tOrd, true)

			break
		}
	}

	return logs
}

// handleFOKOrder handles Fill Or Kill orders. It checks if the order can be fully filled before matching.
func (book *OrderBook) handleFOKOrder(order *Order) []*BookLog {
	var targetQueue *queue
	if order.Side == Buy {
		targetQueue = book.askQueue
	} else {
		targetQueue = book.bidQueue
	}

	// Pre-allocate slice and cache timestamp
	logs := make([]*BookLog, 0, 8)
	now := time.Now().UTC()

	// Phase 1: Validate if the order can be fully filled
	el := targetQueue.depthList.Front()
	remainingSize := order.Size

	for remainingSize.GreaterThan(decimal.Zero) {
		if el == nil {
			// Not enough liquidity - Reject does not change order book state
			log := acquireBookLog()
			log.Type = LogTypeReject
			log.MarketID = order.MarketID
			log.Side = order.Side
			log.Price = order.Price
			log.Size = order.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.CreatedAt = now
			logs = append(logs, log)
			return logs
		}

		unit, _ := el.Value.(*priceUnit)
		tOrd, _ := unit.list.Front().Value.(*Order)

		// Check if the price is acceptable
		if order.Side == Buy && order.Price.LessThan(tOrd.Price) ||
			order.Side == Sell && order.Price.GreaterThan(tOrd.Price) {
			// Price not acceptable - Reject does not change order book state
			log := acquireBookLog()
			log.Type = LogTypeReject
			log.MarketID = order.MarketID
			log.Side = order.Side
			log.Price = order.Price
			log.Size = order.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.CreatedAt = now
			logs = append(logs, log)
			return logs
		}

		// Subtract the entire price level's total size from remaining
		remainingSize = remainingSize.Sub(unit.totalSize)
		el = el.Next()
	}

	// Phase 2: Execute the matching (order can be fully filled)
	for {
		tOrd := targetQueue.popHeadOrder()

		if order.Size.GreaterThanOrEqual(tOrd.Size) {
			book.id.Add(1)
			log := acquireBookLog()
			log.OrderBookID = book.id.Load()
			log.Type = LogTypeMatch
			log.MarketID = order.MarketID
			log.Side = order.Side
			log.Price = tOrd.Price
			log.Size = tOrd.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.MakerOrderID = tOrd.ID
			log.MakerUserID = tOrd.UserID
			log.CreatedAt = now
			logs = append(logs, log)
			order.Size = order.Size.Sub(tOrd.Size)

			if order.Size.Equal(decimal.Zero) {
				break
			}
		} else {
			book.id.Add(1)
			log := acquireBookLog()
			log.OrderBookID = book.id.Load()
			log.Type = LogTypeMatch
			log.MarketID = order.MarketID
			log.Side = order.Side
			log.Price = tOrd.Price
			log.Size = order.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.MakerOrderID = tOrd.ID
			log.MakerUserID = tOrd.UserID
			log.CreatedAt = now
			logs = append(logs, log)
			tOrd.Size = tOrd.Size.Sub(order.Size)
			targetQueue.insertOrder(tOrd, true)

			break
		}
	}

	return logs
}

// handlePostOnlyOrder handles Post Only orders. It ensures the order is added to the book without matching immediately.
func (book *OrderBook) handlePostOnlyOrder(order *Order) []*BookLog {
	var myQueue, targetQueue *queue
	if order.Side == Buy {
		myQueue = book.bidQueue
		targetQueue = book.askQueue
	} else {
		myQueue = book.askQueue
		targetQueue = book.bidQueue
	}

	// Pre-allocate slice and cache timestamp
	logs := make([]*BookLog, 0, 1)
	now := time.Now().UTC()

	// Use peek instead of pop to avoid unnecessary remove/insert operations
	tOrd := targetQueue.getHeadOrder()

	if tOrd == nil {
		// No opposing orders, safe to add
		myQueue.insertOrder(order, false)
		book.id.Add(1)
		log := acquireBookLog()
		log.OrderBookID = book.id.Load()
		log.Type = LogTypeOpen
		log.MarketID = order.MarketID
		log.Side = order.Side
		log.Price = order.Price
		log.Size = order.Size
		log.OrderID = order.ID
		log.UserID = order.UserID
		log.OrderType = order.Type
		log.CreatedAt = now
		logs = append(logs, log)
		return logs
	}

	// Check if order would cross the spread (would match)
	if order.Side == Buy && order.Price.LessThan(tOrd.Price) ||
		order.Side == Sell && order.Price.GreaterThan(tOrd.Price) {
		// Price doesn't cross, safe to add as maker
		myQueue.insertOrder(order, false)
		book.id.Add(1)
		log := acquireBookLog()
		log.OrderBookID = book.id.Load()
		log.Type = LogTypeOpen
		log.MarketID = order.MarketID
		log.Side = order.Side
		log.Price = order.Price
		log.Size = order.Size
		log.OrderID = order.ID
		log.UserID = order.UserID
		log.OrderType = order.Type
		log.CreatedAt = now
		logs = append(logs, log)
		return logs
	}

	// Price would cross - Reject does not change order book state
	log := acquireBookLog()
	log.Type = LogTypeReject
	log.MarketID = order.MarketID
	log.Side = order.Side
	log.Price = order.Price
	log.Size = order.Size
	log.OrderID = order.ID
	log.UserID = order.UserID
	log.OrderType = order.Type
	log.CreatedAt = now
	logs = append(logs, log)
	return logs
}

// handleMarketOrder handles Market orders. It matches against the best available prices until filled or liquidity is exhausted.
func (book *OrderBook) handleMarketOrder(order *Order) []*BookLog {
	targetQueue := book.bidQueue
	if order.Side == Buy {
		targetQueue = book.askQueue
	}

	// Pre-allocate slice and cache timestamp
	logs := make([]*BookLog, 0, 8)
	now := time.Now().UTC()

	for {
		tOrd := targetQueue.popHeadOrder()

		if tOrd == nil {
			// Market order ran out of liquidity - Reject does not change order book state
			log := acquireBookLog()
			log.Type = LogTypeReject
			log.MarketID = order.MarketID
			log.Side = order.Side
			log.Price = order.Price
			log.Size = order.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.CreatedAt = now
			logs = append(logs, log)
			return logs
		}

		// The size of the market order is the total amount, not the quantity.
		amount := tOrd.Price.Mul(tOrd.Size)

		if order.Size.GreaterThanOrEqual(amount) {
			book.id.Add(1)
			log := acquireBookLog()
			log.OrderBookID = book.id.Load()
			log.Type = LogTypeMatch
			log.MarketID = order.MarketID
			log.Side = order.Side
			log.Price = tOrd.Price
			log.Size = tOrd.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.MakerOrderID = tOrd.ID
			log.MakerUserID = tOrd.UserID
			log.CreatedAt = now
			logs = append(logs, log)
			order.Size = order.Size.Sub(amount)
			if order.Size.Equal(decimal.Zero) {
				break
			}
		} else {
			tSize := order.Size.Div(tOrd.Price)

			book.id.Add(1)
			log := acquireBookLog()
			log.OrderBookID = book.id.Load()
			log.Type = LogTypeMatch
			log.MarketID = order.MarketID
			log.Side = order.Side
			log.Price = tOrd.Price
			log.Size = tSize
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.MakerOrderID = tOrd.ID
			log.MakerUserID = tOrd.UserID
			log.CreatedAt = now
			logs = append(logs, log)

			tOrd.Size = tOrd.Size.Sub(tSize)
			targetQueue.insertOrder(tOrd, true)

			break
		}
	}

	return logs
}
