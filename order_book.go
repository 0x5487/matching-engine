package match

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
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
	FOK      OrderType = "fok"       // Fill Or Kill
	IOC      OrderType = "ioc"       // Immediate Or Cancel
	PostOnly OrderType = "post_only" // be maker order only
	Cancel   OrderType = "cancel"    // the order has been canceled
)

// PlaceOrderCommand is the input command for placing an order.
// QuoteSize is only used for Market orders to specify amount in quote currency.
type PlaceOrderCommand struct {
	MarketID  string          `json:"market_id"`
	ID        string          `json:"id"`
	Side      Side            `json:"side"`
	Type      OrderType       `json:"type"`
	Price     decimal.Decimal `json:"price"`                // Limit order price
	Size      decimal.Decimal `json:"size"`                 // Base currency quantity (e.g., BTC)
	QuoteSize decimal.Decimal `json:"quote_size,omitempty"` // Quote currency amount (e.g., USDT), only for Market orders
	UserID    int64           `json:"user_id"`
}

// Order represents the state of an order in the order book.
// This is the serializable state used for snapshots.
type Order struct {
	ID        string          `json:"id"`
	Side      Side            `json:"side"`
	Price     decimal.Decimal `json:"price"`
	Size      decimal.Decimal `json:"size"` // Remaining size
	Type      OrderType       `json:"type"`
	UserID    int64           `json:"user_id"`
	Timestamp int64           `json:"timestamp"` // Unix nano, creation time
}

type LogType string

const (
	LogTypeOpen   LogType = "open"
	LogTypeMatch  LogType = "match"
	LogTypeCancel LogType = "cancel"
	LogTypeAmend  LogType = "amend"
	LogTypeReject LogType = "reject"
)

// RejectReason represents the reason why an order was rejected.
type RejectReason string

const (
	RejectReasonNone             RejectReason = ""
	RejectReasonNoLiquidity      RejectReason = "no_liquidity"       // Market/IOC/FOK: No orders available to match
	RejectReasonPriceMismatch    RejectReason = "price_mismatch"     // IOC/FOK: Price does not meet requirements
	RejectReasonInsufficientSize RejectReason = "insufficient_size"  // FOK: Cannot be fully filled
	RejectReasonWouldCrossSpread RejectReason = "would_cross_spread" // PostOnly: Would match immediately
)

// BookLog represents an event in the order book.
// SequenceID is a globally increasing ID for every event, used for ordering,
// deduplication, and rebuild synchronization in downstream systems.
// Use LogType to determine if the event affects order book state:
// - Open, Match, Cancel, Amend: affect order book state
// - Reject: does not affect order book state
type BookLog struct {
	SequenceID   uint64          `json:"seq_id"`
	TradeID      uint64          `json:"trade_id,omitempty"` // Sequential trade ID, only set for Match events
	Type         LogType         `json:"type"`               // Event type: open, match, cancel, amend, reject
	MarketID     string          `json:"market_id"`
	Side         Side            `json:"side"`
	Price        decimal.Decimal `json:"price"`
	Size         decimal.Decimal `json:"size"`
	Amount       decimal.Decimal `json:"amount,omitempty"` // Price * Size, only set for Match events
	OldPrice     decimal.Decimal `json:"old_price,omitempty"`
	OldSize      decimal.Decimal `json:"old_size,omitempty"`
	OrderID      string          `json:"order_id"`
	UserID       int64           `json:"user_id"`
	OrderType    OrderType       `json:"order_type,omitempty"` // Order type: limit, market, ioc, fok
	MakerOrderID string          `json:"maker_order_id,omitempty"`
	MakerUserID  int64           `json:"maker_user_id,omitempty"`
	RejectReason RejectReason    `json:"reject_reason,omitempty"` // Reason for rejection, only set for Reject events
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

// CommandType represents the type of command sent to the order book.
type CommandType int

const (
	CmdPlaceOrder CommandType = iota
	CmdCancelOrder
	CmdAmendOrder
	CmdDepth
	CmdGetStats
	CmdSnapshot
)

// BookStats contains statistics about the order book queues
type BookStats struct {
	AskDepthCount int64
	AskOrderCount int64
	BidDepthCount int64
	BidOrderCount int64
}

// Command represents a unified command sent to the order book.
// It improves deterministic ordering and performance by using a single channel.
type Command struct {
	SeqID   uint64
	Type    CommandType
	Payload any
	Resp    chan any // Optional: for synchronous response (e.g. CmdDepth)
}

// OrderBook type
type OrderBook struct {
	marketID         string
	seqID            atomic.Uint64 // Globally increasing sequence ID for BookLog production; used by any event that generates an order book log
	lastCmdSeqID     atomic.Uint64 // Last sequence ID of the command
	tradeID          atomic.Uint64 // Sequential trade ID counter, only incremented for Match events
	isShutdown       atomic.Bool
	bidQueue         *queue
	askQueue         *queue
	cmdChan          chan Command
	done             chan struct{}
	shutdownComplete chan struct{}
	publishTrader    PublishLog
}

// NewOrderBook creates a new order book instance.
func NewOrderBook(marketID string, publishTrader PublishLog) *OrderBook {
	return &OrderBook{
		marketID:         marketID,
		bidQueue:         NewBuyerQueue(),
		askQueue:         NewSellerQueue(),
		cmdChan:          make(chan Command, 32768),
		done:             make(chan struct{}),
		shutdownComplete: make(chan struct{}),
		publishTrader:    publishTrader,
	}
}

// AddOrder submits an order to the order book asynchronously.
// Returns ErrShutdown if the order book is shutting down.
func (book *OrderBook) AddOrder(ctx context.Context, cmd *PlaceOrderCommand) error {
	if book.isShutdown.Load() {
		return ErrShutdown
	}

	if len(cmd.Type) == 0 || len(cmd.ID) == 0 {
		return ErrInvalidParam
	}

	select {
	case book.cmdChan <- Command{Type: CmdPlaceOrder, Payload: cmd}:
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
	case book.cmdChan <- Command{Type: CmdAmendOrder, Payload: &AmendRequest{OrderID: id, NewPrice: newPrice, NewSize: newSize}}:
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
	case book.cmdChan <- Command{Type: CmdCancelOrder, Payload: id}:
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

	respChan := make(chan any, 1) // Create a response channel for this specific command

	select {
	case book.cmdChan <- Command{Type: CmdDepth, Payload: limit, Resp: respChan}:
		// Request sent, now wait for response
	case <-time.After(time.Second):
		return nil, ErrTimeout
	}

	select {
	case res := <-respChan:
		// If the response is directly *Depth (not wrapped in *Response), handle it
		if result, ok := res.(*Depth); ok {
			return result, nil
		}

		// If it's *Response (kept for compatibility if needed, though we simplified in Start loop)
		if r, ok := res.(*Response); ok {
			if r.Error != nil {
				return nil, r.Error
			}
			if r.Data != nil {
				if result, ok := r.Data.(*Depth); ok {
					return result, nil
				}
			}
		}
		return nil, nil // Unexpected response type
	case <-time.After(time.Second):
		return nil, ErrTimeout
	}
}

// GetStats returns usage statistics for the order book.
// It is thread-safe and interacts with the order book loop via a channel.
func (book *OrderBook) GetStats() (*BookStats, error) {
	respChan := make(chan any, 1)

	select {
	case book.cmdChan <- Command{Type: CmdGetStats, Resp: respChan}:
		// Request sent, now wait for response
	case <-time.After(time.Second):
		return nil, ErrTimeout
	}

	select {
	case res := <-respChan:
		if result, ok := res.(*BookStats); ok {
			return result, nil
		}
		return nil, nil
	case <-time.After(time.Second):
		return nil, ErrTimeout
	}
}

// LastCmdSeqID returns the sequence ID of the last processed command.
// This is used for snapshot recovery to know where to resume consuming from MQ.
func (book *OrderBook) LastCmdSeqID() uint64 {
	return book.lastCmdSeqID.Load()
}

// Start starts the order book loop to process orders, cancellations, and depth requests.
// Returns nil when Shutdown() is called and all pending orders are drained.
func (book *OrderBook) Start() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	for {
		select {
		case <-book.done:
			return book.drain()
		case cmd := <-book.cmdChan: // Process commands from the unified channel
			switch cmd.Type {
			case CmdPlaceOrder:
				if placeCmd, ok := cmd.Payload.(*PlaceOrderCommand); ok {
					book.addOrder(placeCmd)
				}
			case CmdAmendOrder:
				if req, ok := cmd.Payload.(*AmendRequest); ok {
					book.amendOrder(req)
				}
			case CmdCancelOrder:
				if orderID, ok := cmd.Payload.(string); ok {
					book.cancelOrder(orderID)
				}
			case CmdDepth:
				if limit, ok := cmd.Payload.(uint32); ok {
					result := book.depth(limit)
					// Respond synchronously if a response channel is provided
					if cmd.Resp != nil {
						select {
						case cmd.Resp <- &Response{Data: result}:
						default:
							// Non-blocking send, if no one is listening, just drop it
						}
					}
				}
			case CmdGetStats:
				stats := &BookStats{
					AskDepthCount: book.askQueue.depthCount(),
					AskOrderCount: book.askQueue.orderCount(),
					BidDepthCount: book.bidQueue.depthCount(),
					BidOrderCount: book.bidQueue.orderCount(),
				}
				if cmd.Resp != nil {
					select {
					case cmd.Resp <- stats:
					default:
					}
				}
			case CmdSnapshot:
				snap := book.createSnapshot()
				if cmd.Resp != nil {
					select {
					case cmd.Resp <- snap:
					default:
					}
				}
			}
			// Update lastCmdSeqID after processing each command (for snapshot recovery)
			if cmd.SeqID > 0 {
				book.lastCmdSeqID.Store(cmd.SeqID)
			}
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
		case cmd := <-book.cmdChan:
			switch cmd.Type {
			case CmdPlaceOrder:
				if placeCmd, ok := cmd.Payload.(*PlaceOrderCommand); ok {
					book.addOrder(placeCmd)
				}
			case CmdAmendOrder:
				if req, ok := cmd.Payload.(*AmendRequest); ok {
					book.amendOrder(req)
				}
			case CmdCancelOrder:
				if orderID, ok := cmd.Payload.(string); ok {
					book.cancelOrder(orderID)
				}
			// CmdDepth is read-only, we can skip it during drain or process it if strictly needed.
			// Usually during shutdown/drain we only care about state-mutating commands.
			// However, to strictly drain the channel, we should consume it.
			// However, to strictly drain the channel, we should consume it.
			case CmdDepth, CmdGetStats, CmdSnapshot:
				// Read-only commands, no-op during drain
			}
		default:
			// All channels empty, shutdown complete
			return nil
		}
	}
}

// addOrder processes the addition of an order based on its type.
func (book *OrderBook) addOrder(cmd *PlaceOrderCommand) {
	// Convert command to order state
	order := &Order{
		ID:        cmd.ID,
		Side:      cmd.Side,
		Price:     cmd.Price,
		Size:      cmd.Size,
		Type:      cmd.Type,
		UserID:    cmd.UserID,
		Timestamp: time.Now().UnixNano(),
	}

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
		logs = book.handleMarketOrder(order, cmd.QuoteSize)
	case Cancel:
		// Not a valid order type for placement
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
		log := acquireBookLog()
		log.SequenceID = book.seqID.Add(1)
		log.Type = LogTypeAmend
		log.MarketID = book.marketID
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
		log := acquireBookLog()
		log.SequenceID = book.seqID.Add(1)
		log.Type = LogTypeAmend
		log.MarketID = book.marketID
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
		log := acquireBookLog()
		log.SequenceID = book.seqID.Add(1)
		log.Type = LogTypeCancel
		log.MarketID = book.marketID
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
		log := acquireBookLog()
		log.SequenceID = book.seqID.Add(1)
		log.Type = LogTypeCancel
		log.MarketID = book.marketID
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
		UpdateID: book.seqID.Load(),
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
		tOrd := targetQueue.peekHeadOrder()

		if tOrd == nil {
			myQueue.insertOrder(order, false)
			log := acquireBookLog()
			log.SequenceID = book.seqID.Add(1)
			log.Type = LogTypeOpen
			log.MarketID = book.marketID
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
			log := acquireBookLog()
			log.SequenceID = book.seqID.Add(1)
			log.Type = LogTypeOpen
			log.MarketID = book.marketID
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
			log := acquireBookLog()
			log.SequenceID = book.seqID.Add(1)
			log.TradeID = book.tradeID.Add(1)
			log.Type = LogTypeMatch
			log.MarketID = book.marketID
			log.Side = order.Side
			log.Price = tOrd.Price
			log.Size = tOrd.Size
			log.Amount = tOrd.Price.Mul(tOrd.Size)
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
			log := acquireBookLog()
			log.SequenceID = book.seqID.Add(1)
			log.TradeID = book.tradeID.Add(1)
			log.Type = LogTypeMatch
			log.MarketID = book.marketID
			log.Side = order.Side
			log.Price = tOrd.Price
			log.Size = order.Size
			log.Amount = tOrd.Price.Mul(order.Size)
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
		tOrd := targetQueue.peekHeadOrder()

		if tOrd == nil {
			// IOC Cancel (No match) - Reject does not change order book state
			log := acquireBookLog()
			log.SequenceID = book.seqID.Add(1)
			log.Type = LogTypeReject
			log.MarketID = book.marketID
			log.Side = order.Side
			log.Price = order.Price
			log.Size = order.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.RejectReason = RejectReasonNoLiquidity
			log.CreatedAt = now
			logs = append(logs, log)
			return logs
		}

		if order.Side == Buy && order.Price.LessThan(tOrd.Price) ||
			order.Side == Sell && order.Price.GreaterThan(tOrd.Price) {
			// IOC Cancel (Price mismatch) - Reject does not change order book state
			log := acquireBookLog()
			log.SequenceID = book.seqID.Add(1)
			log.Type = LogTypeReject
			log.MarketID = book.marketID
			log.Side = order.Side
			log.Price = order.Price
			log.Size = order.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.RejectReason = RejectReasonPriceMismatch
			log.CreatedAt = now
			logs = append(logs, log)
			return logs
		}

		// Price matches, now actually pop the order for matching
		tOrd = targetQueue.popHeadOrder()

		if order.Size.GreaterThanOrEqual(tOrd.Size) {
			log := acquireBookLog()
			log.SequenceID = book.seqID.Add(1)
			log.TradeID = book.tradeID.Add(1)
			log.Type = LogTypeMatch
			log.MarketID = book.marketID
			log.Side = order.Side
			log.Price = tOrd.Price
			log.Size = tOrd.Size
			log.Amount = tOrd.Price.Mul(tOrd.Size)
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
			log := acquireBookLog()
			log.SequenceID = book.seqID.Add(1)
			log.TradeID = book.tradeID.Add(1)
			log.Type = LogTypeMatch
			log.MarketID = book.marketID
			log.Side = order.Side
			log.Price = tOrd.Price
			log.Size = order.Size
			log.Amount = tOrd.Price.Mul(order.Size)
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
			log.SequenceID = book.seqID.Add(1)
			log.Type = LogTypeReject
			log.MarketID = book.marketID
			log.Side = order.Side
			log.Price = order.Price
			log.Size = order.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.RejectReason = RejectReasonInsufficientSize
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
			log.SequenceID = book.seqID.Add(1)
			log.Type = LogTypeReject
			log.MarketID = book.marketID
			log.Side = order.Side
			log.Price = order.Price
			log.Size = order.Size
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.RejectReason = RejectReasonPriceMismatch
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
			log := acquireBookLog()
			log.SequenceID = book.seqID.Add(1)
			log.TradeID = book.tradeID.Add(1)
			log.Type = LogTypeMatch
			log.MarketID = book.marketID
			log.Side = order.Side
			log.Price = tOrd.Price
			log.Size = tOrd.Size
			log.Amount = tOrd.Price.Mul(tOrd.Size)
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
			log := acquireBookLog()
			log.SequenceID = book.seqID.Add(1)
			log.TradeID = book.tradeID.Add(1)
			log.Type = LogTypeMatch
			log.MarketID = book.marketID
			log.Side = order.Side
			log.Price = tOrd.Price
			log.Size = order.Size
			log.Amount = tOrd.Price.Mul(order.Size)
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
	tOrd := targetQueue.peekHeadOrder()

	if tOrd == nil {
		// No opposing orders, safe to add
		myQueue.insertOrder(order, false)
		log := acquireBookLog()
		log.SequenceID = book.seqID.Add(1)
		log.Type = LogTypeOpen
		log.MarketID = book.marketID
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
		log := acquireBookLog()
		log.SequenceID = book.seqID.Add(1)
		log.Type = LogTypeOpen
		log.MarketID = book.marketID
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
	log.SequenceID = book.seqID.Add(1)
	log.Type = LogTypeReject
	log.MarketID = book.marketID
	log.Side = order.Side
	log.Price = order.Price
	log.Size = order.Size
	log.OrderID = order.ID
	log.UserID = order.UserID
	log.OrderType = order.Type
	log.RejectReason = RejectReasonWouldCrossSpread
	log.CreatedAt = now
	logs = append(logs, log)
	return logs
}

// handleMarketOrder handles Market orders. It matches against the best available prices until filled or liquidity is exhausted.
// If quoteSize is set (and Size is zero), the order is filled by quote currency amount.
// If Size is set (and quoteSize is zero), the order is filled by base currency quantity.
func (book *OrderBook) handleMarketOrder(order *Order, quoteSize decimal.Decimal) []*BookLog {
	targetQueue := book.bidQueue
	if order.Side == Buy {
		targetQueue = book.askQueue
	}

	// Pre-allocate slice and cache timestamp
	logs := make([]*BookLog, 0, 8)
	now := time.Now().UTC()

	// Determine if using quote size mode (amount in quote currency) or base size mode (quantity in base currency)
	useQuoteSize := quoteSize.GreaterThan(decimal.Zero) && order.Size.IsZero()
	remainingQuote := quoteSize
	remainingBase := order.Size

	for {
		tOrd := targetQueue.popHeadOrder()

		if tOrd == nil {
			// Market order ran out of liquidity - Reject does not change order book state
			log := acquireBookLog()
			log.SequenceID = book.seqID.Add(1)
			log.Type = LogTypeReject
			log.MarketID = book.marketID
			log.Side = order.Side
			log.Price = order.Price
			if useQuoteSize {
				log.Size = remainingQuote // Remaining quote amount
			} else {
				log.Size = remainingBase // Remaining base quantity
			}
			log.OrderID = order.ID
			log.UserID = order.UserID
			log.OrderType = order.Type
			log.RejectReason = RejectReasonNoLiquidity
			log.CreatedAt = now
			logs = append(logs, log)
			return logs
		}

		if useQuoteSize {
			// Quote size mode: order.QuoteSize is the total amount in quote currency (e.g., USDT)
			amount := tOrd.Price.Mul(tOrd.Size) // Quote amount for this maker order

			if remainingQuote.GreaterThanOrEqual(amount) {
				// Consume entire maker order
				log := acquireBookLog()
				log.SequenceID = book.seqID.Add(1)
				log.TradeID = book.tradeID.Add(1)
				log.Type = LogTypeMatch
				log.MarketID = book.marketID
				log.Side = order.Side
				log.Price = tOrd.Price
				log.Size = tOrd.Size
				log.Amount = amount
				log.OrderID = order.ID
				log.UserID = order.UserID
				log.OrderType = order.Type
				log.MakerOrderID = tOrd.ID
				log.MakerUserID = tOrd.UserID
				log.CreatedAt = now
				logs = append(logs, log)
				remainingQuote = remainingQuote.Sub(amount)
				if remainingQuote.Equal(decimal.Zero) {
					break
				}
			} else {
				// Partial fill of maker order
				tSize := remainingQuote.Div(tOrd.Price)

				log := acquireBookLog()
				log.SequenceID = book.seqID.Add(1)
				log.TradeID = book.tradeID.Add(1)
				log.Type = LogTypeMatch
				log.MarketID = book.marketID
				log.Side = order.Side
				log.Price = tOrd.Price
				log.Size = tSize
				log.Amount = remainingQuote
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
		} else {
			// Base size mode: order.Size is the quantity in base currency (e.g., BTC)
			if remainingBase.GreaterThanOrEqual(tOrd.Size) {
				// Consume entire maker order
				log := acquireBookLog()
				log.SequenceID = book.seqID.Add(1)
				log.TradeID = book.tradeID.Add(1)
				log.Type = LogTypeMatch
				log.MarketID = book.marketID
				log.Side = order.Side
				log.Price = tOrd.Price
				log.Size = tOrd.Size
				log.Amount = tOrd.Price.Mul(tOrd.Size)
				log.OrderID = order.ID
				log.UserID = order.UserID
				log.OrderType = order.Type
				log.MakerOrderID = tOrd.ID
				log.MakerUserID = tOrd.UserID
				log.CreatedAt = now
				logs = append(logs, log)
				remainingBase = remainingBase.Sub(tOrd.Size)
				if remainingBase.Equal(decimal.Zero) {
					break
				}
			} else {
				// Partial fill of maker order
				log := acquireBookLog()
				log.SequenceID = book.seqID.Add(1)
				log.TradeID = book.tradeID.Add(1)
				log.Type = LogTypeMatch
				log.MarketID = book.marketID
				log.Side = order.Side
				log.Price = tOrd.Price
				log.Size = remainingBase
				log.Amount = tOrd.Price.Mul(remainingBase)
				log.OrderID = order.ID
				log.UserID = order.UserID
				log.OrderType = order.Type
				log.MakerOrderID = tOrd.ID
				log.MakerUserID = tOrd.UserID
				log.CreatedAt = now
				logs = append(logs, log)

				tOrd.Size = tOrd.Size.Sub(remainingBase)
				targetQueue.insertOrder(tOrd, true)
				break
			}
		}
	}

	return logs
}

// createSnapshot creates a snapshot of the current order book state.
// This method is called from the order book loop (via CmdSnapshot), so it's thread-safe with respect to order processing.
func (book *OrderBook) createSnapshot() *OrderBookSnapshot {
	snap := &OrderBookSnapshot{
		MarketID:     book.marketID,
		SeqID:        book.seqID.Load(),
		LastCmdSeqID: book.lastCmdSeqID.Load(),
		TradeID:      book.tradeID.Load(),
		Bids:         make([]*Order, 0),
		Asks:         make([]*Order, 0),
	}

	// Capture bids
	bids := book.bidQueue.toSnapshot()
	for i := range bids {
		snap.Bids = append(snap.Bids, &bids[i])
	}

	// Capture asks
	asks := book.askQueue.toSnapshot()
	for i := range asks {
		snap.Asks = append(snap.Asks, &asks[i])
	}

	return snap
}

// Restore restores the order book state from a snapshot.
// It resets the current state and rebuilds the order book from the snapshot data.
func (book *OrderBook) Restore(snap *OrderBookSnapshot) {
	// 1. Reset counters
	book.seqID.Store(snap.SeqID)
	book.lastCmdSeqID.Store(snap.LastCmdSeqID)
	book.tradeID.Store(snap.TradeID)

	// 2. Clear current queues
	book.bidQueue = NewBuyerQueue()
	book.askQueue = NewSellerQueue()

	// 3. Helper to insert orders
	restoreOrders := func(orders []*Order, queue *queue) {
		for _, o := range orders {
			// Insert directly into queue, bypassing matching logic
			queue.insertOrder(o, false) // Insert at back to preserve priority if sorted by time
		}
	}

	restoreOrders(snap.Bids, book.bidQueue)
	restoreOrders(snap.Asks, book.askQueue)
}

// TakeSnapshot captures the current state of the order book.
// It is thread-safe and interacts with the order book loop via a channel.
func (book *OrderBook) TakeSnapshot() (*OrderBookSnapshot, error) {
	respChan := make(chan any, 1)
	cmd := Command{
		Type: CmdSnapshot,
		Resp: respChan,
	}

	select {
	case book.cmdChan <- cmd:
		select {
		case res := <-respChan:
			if snap, ok := res.(*OrderBookSnapshot); ok {
				return snap, nil
			}
			return nil, errors.New("unexpected response type for snapshot")
		case <-time.After(5 * time.Second): // Timeout for snapshot
			return nil, ErrTimeout
		}
	case <-book.done:
		return nil, ErrOrderBookClosed
	case <-time.After(1 * time.Second): // Fail fast if channel is full
		return nil, ErrTimeout
	}
}
