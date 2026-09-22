package match

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/quagmt/udecimal"

	"github.com/0x5487/matching-engine/protocol"
)

// orderPool is used to reduce Order allocations in the hot path.
var orderPool = sync.Pool{
	New: func() any {
		return &Order{}
	},
}

func acquireOrder() *Order {
	val := orderPool.Get()
	o, ok := val.(*Order)
	if !ok {
		return &Order{}
	}
	return o
}

func releaseOrder(o *Order) {
	*o = Order{}
	orderPool.Put(o)
}

const defaultLotSizePrecision = 8

// DefaultLotSize is the fallback minimum trade unit (1e-8).
// This prevents infinite loops when quoteSize/price produces very small values.
var DefaultLotSize = udecimal.MustFromInt64(1, defaultLotSizePrecision) // 0.00000001

// OrderBookOption configures an OrderBook.
type OrderBookOption func(*OrderBook)

// WithLotSize sets the minimum trade unit for the order book.
// When a Market order's calculated match size is less than this value,
// the order will be rejected with remaining funds returned.
// Default: 1e-8 (0.00000001) as a safety fallback.
func WithLotSize(size udecimal.Decimal) OrderBookOption {
	return func(book *OrderBook) {
		book.lotSize = size
	}
}

// WithEngineID sets the engine ID for the order book.
func WithEngineID(id string) OrderBookOption {
	return func(book *OrderBook) {
		book.engineID = id
	}
}

// WithPublisher sets the publisher for the order book.
func WithPublisher(publisher Publisher) OrderBookOption {
	return func(book *OrderBook) {
		book.publisher = publisher
	}
}

// WithSkiplistSeed sets the skiplist PRNG seed for deterministic queue ordering.
func WithSkiplistSeed(seed int64) OrderBookOption {
	return func(book *OrderBook) {
		book.skiplistSeed = seed
	}
}

// OrderBook is a pure logic object that maintains the state of an order book.
// It can be run directly (synchronously) or managed by a MatchingEngine event loop.
type OrderBook struct {
	engineID     string
	marketID     string
	lotSize      udecimal.Decimal // Minimum trade unit for Market orders
	skiplistSeed int64            // PRNG seed for price skiplist
	seqID        atomic.Uint64    // Globally increasing sequence ID
	lastCmdSeqID atomic.Uint64    // Last sequence ID of the command
	tradeID      atomic.Uint64    // Sequential trade ID counter
	bidQueue     *queue
	askQueue     *queue
	publisher    Publisher
	state        protocol.OrderBookState
}

// NewOrderBook creates a new pure OrderBook instance.
func NewOrderBook(
	marketID string,
	opts ...OrderBookOption,
) *OrderBook {
	book := &OrderBook{
		engineID:     "default",
		marketID:     marketID,
		lotSize:      DefaultLotSize,
		skiplistSeed: defaultSkiplistSeed,
		state:        protocol.OrderBookStateRunning,
	}

	for _, opt := range opts {
		opt(book)
	}

	book.bidQueue = newBuyerQueue(book.skiplistSeed)
	book.askQueue = newSellerQueue(book.skiplistSeed)

	return book
}

// newOrderBookWithPublisher creates a new OrderBook instance (internal backward-compatible constructor).
func newOrderBookWithPublisher(
	engineID string,
	marketID string,
	publishTrader Publisher,
	opts ...OrderBookOption,
) *OrderBook {
	allOpts := append([]OrderBookOption{
		WithEngineID(engineID),
		WithPublisher(publishTrader),
	}, opts...)
	return NewOrderBook(marketID, allOpts...)
}

// LastCmdSeqID returns the sequence ID of the last processed command.
func (book *OrderBook) LastCmdSeqID() uint64 {
	return book.lastCmdSeqID.Load()
}

// Restore restores the order book state from a snapshot.
func (book *OrderBook) Restore(snap *OrderBookSnapshot) {
	book.seqID.Store(snap.SeqID)
	// Check for nil decimal (zero value) in case of old snapshot or default
	if snap.MinLotSize.IsZero() {
		book.lotSize = DefaultLotSize
	} else {
		book.lotSize = snap.MinLotSize
	}
	book.state = snap.State
	book.lastCmdSeqID.Store(snap.LastCmdSeqID)
	book.tradeID.Store(snap.TradeID)

	for _, o := range snap.Bids {
		newOrder := acquireOrder()
		*newOrder = *o
		book.bidQueue.insertOrder(newOrder)
	}
	for _, o := range snap.Asks {
		newOrder := acquireOrder()
		*newOrder = *o
		book.askQueue.insertOrder(newOrder)
	}
}

// MarketID returns the market identifier for this order book.
func (book *OrderBook) MarketID() string {
	return book.marketID
}

// EngineID returns the engine identifier for this order book.
func (book *OrderBook) EngineID() string {
	return book.engineID
}

// State returns the current lifecycle state of the order book.
func (book *OrderBook) State() protocol.OrderBookState {
	return book.state
}

// SetState sets the lifecycle state of the order book.
func (book *OrderBook) SetState(state protocol.OrderBookState) {
	book.state = state
}

// SeqID returns the current sequence ID of the order book.
func (book *OrderBook) SeqID() uint64 {
	return book.seqID.Load()
}

// TradeID returns the current trade ID counter of the order book.
func (book *OrderBook) TradeID() uint64 {
	return book.tradeID.Load()
}

// Snapshot creates and returns an in-memory snapshot of the current OrderBook state.
func (book *OrderBook) Snapshot() *OrderBookSnapshot {
	return book.createSnapshot()
}

// GetDepth returns the aggregated market depth up to the specified limit.
func (book *OrderBook) GetDepth(limit uint32) *protocol.GetDepthResponse {
	return book.depth(limit)
}

// PlaceOrder processes an order synchronously and returns the resulting LogBatch.
// The caller is responsible for processing logs and calling batch.Release().
func (book *OrderBook) PlaceOrder(req *protocol.PlaceOrderRequest) (*LogBatch, error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	if req.Timestamp <= 0 {
		return book.createRejectBatch(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonInvalidPayload,
			req.Timestamp,
		), nil
	}

	if book.state == protocol.OrderBookStateHalted {
		return book.createRejectBatch(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonMarketHalted,
			req.Timestamp,
		), nil
	}
	if book.state == protocol.OrderBookStateSuspended {
		return book.createRejectBatch(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonMarketSuspended,
			req.Timestamp,
		), nil
	}

	// Basic validation
	isValid := true
	if req.OrderType == Market {
		if req.Size.IsZero() && req.QuoteSize.IsZero() {
			isValid = false
		}
	} else {
		if req.Price.IsZero() || req.Size.IsZero() {
			isValid = false
		}
	}

	if !isValid {
		return book.createRejectBatch(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonInvalidPayload,
			req.Timestamp,
		), nil
	}

	// Check for duplicate ID
	if book.bidQueue.order(req.OrderID) != nil || book.askQueue.order(req.OrderID) != nil {
		return book.createRejectBatch(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonDuplicateID,
			req.Timestamp,
		), nil
	}

	order := acquireOrder()
	order.ID = req.OrderID
	order.Side = req.Side
	order.Price = req.Price
	order.Size = req.Size
	order.Type = req.OrderType
	order.UserID = req.UserID
	order.Timestamp = req.Timestamp

	if req.VisibleSize.GreaterThan(udecimal.Zero) && req.VisibleSize.LessThan(req.Size) {
		order.VisibleLimit = req.VisibleSize
	}

	var batch *LogBatch
	switch order.Type {
	case Limit:
		batch = book.handleLimitOrder(req.CommandID, order, req.Timestamp)
	case FOK:
		batch = book.handleFOKOrder(req.CommandID, order, req.Timestamp)
	case IOC:
		batch = book.handleIOCOrder(req.CommandID, order, req.Timestamp)
	case PostOnly:
		batch = book.handlePostOnlyOrder(req.CommandID, order, req.Timestamp)
	case Market:
		batch = book.handleMarketOrder(req.CommandID, order, req.QuoteSize, req.Timestamp)
	default:
		releaseOrder(order)
		return nil, ErrInvalidParam
	}

	return batch, nil
}

// CancelOrder processes an order cancellation synchronously and returns the resulting LogBatch.
// The caller is responsible for processing logs and calling batch.Release().
func (book *OrderBook) CancelOrder(req *protocol.CancelOrderRequest) (*LogBatch, error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	if req.Timestamp <= 0 || req.OrderID == "" {
		return book.createRejectBatch(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonInvalidPayload,
			req.Timestamp,
		), nil
	}

	if book.state == protocol.OrderBookStateHalted {
		return book.createRejectBatch(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonMarketHalted,
			req.Timestamp,
		), nil
	}

	order, ok := book.findOrder(req.OrderID)
	if !ok || order.UserID != req.UserID {
		return book.createRejectBatch(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonOrderNotFound,
			req.Timestamp,
		), nil
	}

	myQueue := book.bidQueue
	if order.Side == Sell {
		myQueue = book.askQueue
	}

	myQueue.removeOrder(order.Price, order.ID)
	batch := acquireLogBatch()
	totalSize := order.Size.Add(order.HiddenSize)
	log := NewCancelLog(
		book.seqID.Add(1),
		req.CommandID,
		book.engineID,
		book.marketID,
		order.ID,
		order.UserID,
		order.Side,
		order.Price,
		totalSize,
		order.Type,
		req.Timestamp,
	)
	batch.Logs = append(batch.Logs, log)
	releaseOrder(order)

	return batch, nil
}

// AmendOrder processes an order amendment synchronously and returns the resulting LogBatch.
// The caller is responsible for processing logs and calling batch.Release().
func (book *OrderBook) AmendOrder(req *protocol.AmendOrderRequest) (*LogBatch, error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	if req.Timestamp <= 0 || req.OrderID == "" {
		return book.createRejectBatch(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonInvalidPayload,
			req.Timestamp,
		), nil
	}

	if book.state == protocol.OrderBookStateHalted {
		return book.createRejectBatch(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonMarketHalted,
			req.Timestamp,
		), nil
	}
	if book.state == protocol.OrderBookStateSuspended {
		return book.createRejectBatch(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonMarketSuspended,
			req.Timestamp,
		), nil
	}

	order, ok := book.findOrder(req.OrderID)
	if !ok || order.UserID != req.UserID {
		return book.createRejectBatch(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonOrderNotFound,
			req.Timestamp,
		), nil
	}

	newPrice := req.NewPrice
	newSize := req.NewSize

	// 0 means no change
	if newPrice.IsZero() {
		newPrice = order.Price
	}
	if newSize.IsZero() {
		newSize = order.Size.Add(order.HiddenSize)
	}

	myQueue := book.bidQueue
	if order.Side == Sell {
		myQueue = book.askQueue
	}

	oldPrice := order.Price
	oldTotalSize := order.Size.Add(order.HiddenSize)

	isPriceChange := !oldPrice.Equal(newPrice)
	isSizeIncrease := newSize.GreaterThan(oldTotalSize)

	amendBatch := acquireLogBatch()
	log := NewAmendLog(
		book.seqID.Add(1),
		req.CommandID,
		book.engineID,
		book.marketID,
		order.ID,
		order.UserID,
		order.Side,
		newPrice,
		newSize,
		oldPrice,
		oldTotalSize,
		order.Type,
		req.Timestamp,
	)
	amendBatch.Logs = append(amendBatch.Logs, log)

	if isPriceChange || isSizeIncrease {
		// Path 1: Priority Loss (Re-match)
		myQueue.removeOrder(oldPrice, order.ID)
		order.Price = newPrice
		order.Timestamp = req.Timestamp

		// Recalculate Iceberg fields for the new total size
		if order.VisibleLimit.IsZero() {
			order.Size = newSize
			order.HiddenSize = udecimal.Zero
		} else {
			if newSize.GreaterThan(order.VisibleLimit) {
				order.Size = order.VisibleLimit
				order.HiddenSize = newSize.Sub(order.VisibleLimit)
			} else {
				order.Size = newSize
				order.HiddenSize = udecimal.Zero
			}
		}

		batch := book.handleLimitOrder(req.CommandID, order, req.Timestamp)
		if batch != nil {
			amendBatch.Logs = append(amendBatch.Logs, batch.Logs...)
			batch.Release()
		}
	} else if newSize.LessThan(oldTotalSize) {
		// Path 2: Priority Retention (In-place update)
		delta := oldTotalSize.Sub(newSize)
		if delta.LessThanOrEqual(order.HiddenSize) {
			order.HiddenSize = order.HiddenSize.Sub(delta)
		} else {
			remainingDelta := delta.Sub(order.HiddenSize)
			order.HiddenSize = udecimal.Zero
			newVisibleSize := order.Size.Sub(remainingDelta)
			myQueue.updateOrderSize(order.ID, newVisibleSize)
		}
	}

	return amendBatch, nil
}

func (book *OrderBook) processCommand(ev *InputEvent) {
	req := ev.Request
	switch request := req.(type) {
	case *protocol.SuspendMarketRequest:
		book.handleSuspendMarket(ev, request)
	case *protocol.ResumeMarketRequest:
		book.handleResumeMarket(ev, request)
	case *protocol.UpdateConfigRequest:
		book.handleUpdateConfig(ev, request)
	case *protocol.PlaceOrderRequest:
		book.handlePlaceOrder(ev, request)
	case *protocol.CancelOrderRequest:
		book.handleCancelOrder(ev, request)
	case *protocol.AmendOrderRequest:
		book.handleAmendOrder(ev, request)
	default:
		base, _ := protocol.GetRequestBase(req)
		book.rejectInvalidPayload(
			base.CommandID,
			book.marketID,
			"unknown",
			base.UserID,
			protocol.RejectReasonUnknownCommand,
			base.Timestamp,
		)
		book.sendResponse(ev.Resp, ErrUnknownCommand)
	}

	if base, ok := protocol.GetRequestBase(req); ok && base.SeqID > 0 {
		book.lastCmdSeqID.Store(base.SeqID)
	}
}

func (book *OrderBook) handleSuspendMarket(ev *InputEvent, req *protocol.SuspendMarketRequest) {
	if !book.validateBasic(req.CommandID, req.UserID, "", req.Timestamp) {
		book.sendResponse(ev.Resp, errors.New(string(protocol.RejectReasonInvalidPayload)))
		return
	}

	book.state = protocol.OrderBookStateSuspended
	batch := acquireLogBatch()
	log := NewAdminLog(
		book.seqID.Add(1),
		req.CommandID,
		book.engineID,
		book.marketID,
		req.UserID,
		req.Reason,
		req.Timestamp,
	)
	batch.Logs = append(batch.Logs, log)
	book.publisher.Publish(batch.Logs)
	releaseBookLog(log)
	batch.Release()

	book.sendResponse(ev.Resp, true)
}

func (book *OrderBook) handleResumeMarket(ev *InputEvent, req *protocol.ResumeMarketRequest) {
	if !book.validateBasic(req.CommandID, req.UserID, "", req.Timestamp) {
		book.sendResponse(ev.Resp, errors.New(string(protocol.RejectReasonInvalidPayload)))
		return
	}

	book.state = protocol.OrderBookStateRunning
	batch := acquireLogBatch()
	log := NewAdminLog(
		book.seqID.Add(1),
		req.CommandID,
		book.engineID,
		book.marketID,
		req.UserID,
		"market_resumed",
		req.Timestamp,
	)
	batch.Logs = append(batch.Logs, log)
	book.publisher.Publish(batch.Logs)
	releaseBookLog(log)
	batch.Release()

	book.sendResponse(ev.Resp, true)
}

func (book *OrderBook) handleUpdateConfig(ev *InputEvent, req *protocol.UpdateConfigRequest) {
	if !book.validateBasic(req.CommandID, req.UserID, "", req.Timestamp) {
		book.sendResponse(ev.Resp, errors.New(string(protocol.RejectReasonInvalidPayload)))
		return
	}

	if !req.MinLotSize.IsZero() {
		book.lotSize = req.MinLotSize
		batch := acquireLogBatch()
		log := NewAdminLog(
			book.seqID.Add(1),
			req.CommandID,
			book.engineID,
			book.marketID,
			req.UserID,
			"market_config_updated",
			req.Timestamp,
		)
		batch.Logs = append(batch.Logs, log)
		book.publisher.Publish(batch.Logs)
		releaseBookLog(log)
		batch.Release()
		book.sendResponse(ev.Resp, true)
	} else {
		// No config update requested, but still respond success if we reached here
		book.sendResponse(ev.Resp, true)
	}
}

func (book *OrderBook) createRejectBatch(
	commandID string,
	orderID string,
	userID uint64,
	reason protocol.RejectReason,
	timestamp int64,
) *LogBatch {
	batch := acquireLogBatch()
	log := NewRejectLog(
		book.seqID.Add(1),
		commandID,
		book.engineID,
		book.marketID,
		orderID,
		userID,
		reason,
		timestamp,
	)
	batch.Logs = append(batch.Logs, log)
	return batch
}

func (book *OrderBook) handlePlaceOrder(ev *InputEvent, req *protocol.PlaceOrderRequest) {
	batch, err := book.PlaceOrder(req)
	if err != nil {
		book.sendResponse(ev.Resp, err)
		return
	}
	if batch != nil {
		if len(batch.Logs) > 0 {
			if book.publisher != nil {
				book.publisher.Publish(batch.Logs)
			}
			for _, log := range batch.Logs {
				if log.Type == protocol.LogTypeReject {
					book.sendResponse(ev.Resp, errors.New(string(log.RejectReason)))
					releaseBookLog(log)
					batch.Release()
					return
				}
				releaseBookLog(log)
			}
		}
		batch.Release()
	}
	book.sendResponse(ev.Resp, true)
}

func (book *OrderBook) handleCancelOrder(ev *InputEvent, req *protocol.CancelOrderRequest) {
	batch, err := book.CancelOrder(req)
	if err != nil {
		book.sendResponse(ev.Resp, err)
		return
	}
	if batch != nil {
		if len(batch.Logs) > 0 {
			if book.publisher != nil {
				book.publisher.Publish(batch.Logs)
			}
			for _, log := range batch.Logs {
				if log.Type == protocol.LogTypeReject {
					book.sendResponse(ev.Resp, errors.New(string(log.RejectReason)))
					releaseBookLog(log)
					batch.Release()
					return
				}
				releaseBookLog(log)
			}
		}
		batch.Release()
	}
	book.sendResponse(ev.Resp, true)
}

func (book *OrderBook) handleAmendOrder(ev *InputEvent, req *protocol.AmendOrderRequest) {
	batch, err := book.AmendOrder(req)
	if err != nil {
		book.sendResponse(ev.Resp, err)
		return
	}
	if batch != nil {
		if len(batch.Logs) > 0 {
			if book.publisher != nil {
				book.publisher.Publish(batch.Logs)
			}
			for _, log := range batch.Logs {
				if log.Type == protocol.LogTypeReject {
					book.sendResponse(ev.Resp, errors.New(string(log.RejectReason)))
					releaseBookLog(log)
					batch.Release()
					return
				}
				releaseBookLog(log)
			}
		}
		batch.Release()
	}
	book.sendResponse(ev.Resp, true)
}

func (book *OrderBook) processQuery(ev *InputEvent) {
	switch q := ev.Query.(type) {
	case *protocol.GetDepthQuery:
		book.sendResponse(ev.Resp, book.depth(q.Limit))
	case *protocol.GetStatsQuery:
		book.sendResponse(ev.Resp, book.stats())
	case *snapshotQuery:
		book.sendResponse(ev.Resp, book.createSnapshot())
	default:
		book.sendResponse(ev.Resp, ErrUnknownQuery)
	}
}

func (book *OrderBook) stats() *protocol.GetStatsResponse {
	return &protocol.GetStatsResponse{
		AskDepthCount: book.askQueue.depthCount(),
		AskOrderCount: book.askQueue.orderCount(),
		BidDepthCount: book.bidQueue.depthCount(),
		BidOrderCount: book.bidQueue.orderCount(),
	}
}

// sendResponse sends a value to the response channel if it is not nil.
func (book *OrderBook) sendResponse(resp chan<- any, val any) {
	if resp != nil {
		select {
		case resp <- val:
		default:
		}
	}
}

func (book *OrderBook) rejectInvalidPayload(
	commandID string,
	marketID string,
	orderID string,
	userID uint64,
	reason protocol.RejectReason,
	timestamp int64,
) {
	batch := acquireLogBatch()
	log := NewRejectLog(
		book.seqID.Add(1),
		commandID,
		book.engineID,
		marketID,
		orderID,
		userID,
		reason,
		timestamp,
	)
	batch.Logs = append(batch.Logs, log)
	book.publisher.Publish(batch.Logs)
	releaseBookLog(log)
	batch.Release()
}

func (book *OrderBook) validateBasic(
	commandID string,
	userID uint64,
	orderID string,
	timestamp int64,
) bool {
	if timestamp <= 0 {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			orderID,
			userID,
			protocol.RejectReasonInvalidPayload,
			timestamp,
		)
		return false
	}

	if book.state == protocol.OrderBookStateHalted {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			orderID,
			userID,
			protocol.RejectReasonMarketHalted,
			timestamp,
		)
		return false
	}

	return true
}

func (book *OrderBook) findOrder(orderID string) (*Order, bool) {
	order := book.bidQueue.order(orderID)
	if order != nil {
		return order, true
	}
	order = book.askQueue.order(orderID)
	if order != nil {
		return order, true
	}
	return nil, false
}

// depth returns the snapshot of the order book depth.
func (book *OrderBook) depth(limit uint32) *protocol.GetDepthResponse {
	return &protocol.GetDepthResponse{
		UpdateID: book.seqID.Load(),
		Asks:     book.askQueue.depth(limit),
		Bids:     book.bidQueue.depth(limit),
	}
}

// createSnapshot internal logic to build the snapshot.
func (book *OrderBook) createSnapshot() *OrderBookSnapshot {
	return &OrderBookSnapshot{
		MarketID:     book.marketID,
		SeqID:        book.seqID.Load(),
		LastCmdSeqID: book.lastCmdSeqID.Load(),
		TradeID:      book.tradeID.Load(),
		Bids:         book.bidQueue.toSnapshot(),
		Asks:         book.askQueue.toSnapshot(),
		State:        book.state,
		MinLotSize:   book.lotSize,
	}
}

// handleLimitOrder handles Limit orders.
func (book *OrderBook) handleLimitOrder(commandID string, order *Order, timestamp int64) *LogBatch {
	var myQueue, targetQueue *queue
	if order.Side == Buy {
		myQueue = book.bidQueue
		targetQueue = book.askQueue
	} else {
		myQueue = book.askQueue
		targetQueue = book.bidQueue
	}

	batch := acquireLogBatch()

	for {
		tOrd := targetQueue.peekHeadOrder()
		if tOrd == nil {
			book.prepareIcebergForResting(order)
			myQueue.insertOrder(order)
			log := NewOpenLog(
				book.seqID.Add(1),
				commandID,
				book.engineID,
				book.marketID,
				order.ID,
				order.UserID,
				order.Side,
				order.Price,
				order.Size,
				order.Type,
				timestamp,
			)
			batch.Logs = append(batch.Logs, log)
			return batch
		}

		if (order.Side == Buy && order.Price.LessThan(tOrd.Price)) ||
			(order.Side == Sell && order.Price.GreaterThan(tOrd.Price)) {
			book.prepareIcebergForResting(order)
			myQueue.insertOrder(order)
			log := NewOpenLog(
				book.seqID.Add(1),
				commandID,
				book.engineID,
				book.marketID,
				order.ID,
				order.UserID,
				order.Side,
				order.Price,
				order.Size,
				order.Type,
				timestamp,
			)
			batch.Logs = append(batch.Logs, log)
			return batch
		}

		tOrd = targetQueue.popHeadOrder()
		if order.Size.LessThan(tOrd.Size) {
			log := NewMatchLog(
				book.seqID.Add(1),
				commandID,
				book.engineID,
				book.tradeID.Add(1),
				book.marketID,
				order.ID,
				order.UserID,
				order.Side,
				order.Type,
				tOrd.ID,
				tOrd.UserID,
				tOrd.Price,
				order.Size,
				timestamp,
			)
			batch.Logs = append(batch.Logs, log)
			tOrd.Size = tOrd.Size.Sub(order.Size)
			targetQueue.pushFront(tOrd)
			releaseOrder(order)
			break
		}

		log := NewMatchLog(
			book.seqID.Add(1),
			commandID,
			book.engineID,
			book.tradeID.Add(1),
			book.marketID,
			order.ID,
			order.UserID,
			order.Side,
			order.Type,
			tOrd.ID,
			tOrd.UserID,
			tOrd.Price,
			tOrd.Size,
			timestamp,
		)
		batch.Logs = append(batch.Logs, log)
		order.Size = order.Size.Sub(tOrd.Size)

		if !book.checkReplenish(commandID, tOrd, targetQueue, batch, timestamp) {
			releaseOrder(tOrd)
		}

		if order.Size.Equal(udecimal.Zero) {
			releaseOrder(order)
			break
		}
	}
	return batch
}

// prepareIcebergForResting splits an Iceberg order's size into visible and hidden parts
// when the order is about to rest in the order book.
func (book *OrderBook) prepareIcebergForResting(order *Order) {
	if order.VisibleLimit.GreaterThan(udecimal.Zero) && order.HiddenSize.IsZero() &&
		order.Size.GreaterThan(order.VisibleLimit) {
		order.HiddenSize = order.Size.Sub(order.VisibleLimit)
		order.Size = order.VisibleLimit
	}
}

// handleIOCOrder handles Immediate Or Cancel orders.
func (book *OrderBook) handleIOCOrder(commandID string, order *Order, timestamp int64) *LogBatch {
	var targetQueue *queue
	if order.Side == Buy {
		targetQueue = book.askQueue
	} else {
		targetQueue = book.bidQueue
	}

	batch := acquireLogBatch()
	for {
		tOrd := targetQueue.peekHeadOrder()
		if tOrd == nil {
			log := NewRejectLog(
				book.seqID.Add(1),
				commandID,
				book.engineID,
				book.marketID,
				order.ID,
				order.UserID,
				protocol.RejectReasonNoLiquidity,
				timestamp,
			)
			log.Side, log.Price, log.Size, log.OrderType = order.Side, order.Price, order.Size, order.Type
			batch.Logs = append(batch.Logs, log)
			releaseOrder(order)
			return batch
		}

		if (order.Side == Buy && order.Price.LessThan(tOrd.Price)) ||
			(order.Side == Sell && order.Price.GreaterThan(tOrd.Price)) {
			log := NewRejectLog(
				book.seqID.Add(1),
				commandID,
				book.engineID,
				book.marketID,
				order.ID,
				order.UserID,
				protocol.RejectReasonPriceMismatch,
				timestamp,
			)
			log.Side, log.Price, log.Size, log.OrderType = order.Side, order.Price, order.Size, order.Type
			batch.Logs = append(batch.Logs, log)
			releaseOrder(order)
			return batch
		}

		tOrd = targetQueue.popHeadOrder()
		if order.Size.LessThan(tOrd.Size) {
			log := NewMatchLog(
				book.seqID.Add(1),
				commandID,
				book.engineID,
				book.tradeID.Add(1),
				book.marketID,
				order.ID,
				order.UserID,
				order.Side,
				order.Type,
				tOrd.ID,
				tOrd.UserID,
				tOrd.Price,
				order.Size,
				timestamp,
			)
			batch.Logs = append(batch.Logs, log)
			tOrd.Size = tOrd.Size.Sub(order.Size)
			targetQueue.pushFront(tOrd)
			releaseOrder(order)
			break
		}

		log := NewMatchLog(
			book.seqID.Add(1),
			commandID,
			book.engineID,
			book.tradeID.Add(1),
			book.marketID,
			order.ID,
			order.UserID,
			order.Side,
			order.Type,
			tOrd.ID,
			tOrd.UserID,
			tOrd.Price,
			tOrd.Size,
			timestamp,
		)
		batch.Logs = append(batch.Logs, log)
		order.Size = order.Size.Sub(tOrd.Size)

		if !book.checkReplenish(commandID, tOrd, targetQueue, batch, timestamp) {
			releaseOrder(tOrd)
		}

		if order.Size.Equal(udecimal.Zero) {
			releaseOrder(order)
			break
		}
	}
	return batch
}

// handleFOKOrder handles Fill Or Kill orders.
func (book *OrderBook) handleFOKOrder(commandID string, order *Order, timestamp int64) *LogBatch {
	var targetQueue *queue
	if order.Side == Buy {
		targetQueue = book.askQueue
	} else {
		targetQueue = book.bidQueue
	}

	// Phase 1: Check if can be fully filled
	remainingSize := order.Size
	canFill := false
	hasLiquidityAtPrice := false
	it := targetQueue.priceIterator()
	for it.Valid() {
		price, unit := it.PriceUnit()
		if (order.Side == Buy && order.Price.LessThan(price)) ||
			(order.Side == Sell && order.Price.GreaterThan(price)) {
			break
		}
		hasLiquidityAtPrice = true
		if remainingSize.LessThanOrEqual(unit.totalSize) {
			canFill = true
			break
		}
		remainingSize = remainingSize.Sub(unit.totalSize)
		it.Next()
	}

	if !canFill {
		batch := acquireLogBatch()
		reason := protocol.RejectReasonInsufficientSize
		if !hasLiquidityAtPrice {
			// If we didn't find ANY order matching the price, it might be PriceMismatch or NoLiquidity
			if targetQueue.peekHeadOrder() == nil {
				reason = protocol.RejectReasonNoLiquidity
			} else {
				reason = protocol.RejectReasonPriceMismatch
			}
		}
		log := NewRejectLog(
			book.seqID.Add(1),
			commandID,
			book.engineID,
			book.marketID,
			order.ID,
			order.UserID,
			reason,
			timestamp,
		)
		log.Side, log.Price, log.Size, log.OrderType = order.Side, order.Price, order.Size, order.Type
		batch.Logs = append(batch.Logs, log)
		releaseOrder(order)
		return batch
	}

	// Phase 2: Execute match (same as IOC/Limit logic but guaranteed to finish)
	return book.handleIOCOrder(commandID, order, timestamp)
}

// handlePostOnlyOrder handles Post-Only orders.
func (book *OrderBook) handlePostOnlyOrder(
	commandID string,
	order *Order,
	timestamp int64,
) *LogBatch {
	var targetQueue *queue
	if order.Side == Buy {
		targetQueue = book.askQueue
	} else {
		targetQueue = book.bidQueue
	}

	tOrd := targetQueue.peekHeadOrder()
	if tOrd != nil {
		if (order.Side == Buy && order.Price.GreaterThanOrEqual(tOrd.Price)) ||
			(order.Side == Sell && order.Price.LessThanOrEqual(tOrd.Price)) {
			batch := acquireLogBatch()
			log := NewRejectLog(
				book.seqID.Add(1),
				commandID,
				book.engineID,
				book.marketID,
				order.ID,
				order.UserID,
				protocol.RejectReasonPostOnlyMatch,
				timestamp,
			)
			log.Side, log.Price, log.Size, log.OrderType = order.Side, order.Price, order.Size, order.Type
			batch.Logs = append(batch.Logs, log)
			releaseOrder(order)
			return batch
		}
	}

	return book.handleLimitOrder(commandID, order, timestamp)
}

// handleMarketOrder handles Market orders.
func (book *OrderBook) handleMarketOrder(
	commandID string,
	order *Order,
	quoteSize udecimal.Decimal,
	timestamp int64,
) *LogBatch {
	var targetQueue *queue
	if order.Side == Buy {
		targetQueue = book.askQueue
	} else {
		targetQueue = book.bidQueue
	}

	batch := acquireLogBatch()
	for {
		tOrd := targetQueue.peekHeadOrder()
		if tOrd == nil {
			log := NewRejectLog(
				book.seqID.Add(1),
				commandID,
				book.engineID,
				book.marketID,
				order.ID,
				order.UserID,
				protocol.RejectReasonNoLiquidity,
				timestamp,
			)
			log.Side, log.Price, log.OrderType = order.Side, order.Price, order.Type
			if order.Type == Market && !quoteSize.IsZero() {
				log.Size = quoteSize
			} else {
				log.Size = order.Size
			}
			batch.Logs = append(batch.Logs, log)
			releaseOrder(order)
			break
		}

		matchSize := order.Size
		useQuote := matchSize.IsZero() && order.Type == Market && !quoteSize.IsZero()
		if useQuote {
			maxSize, _ := quoteSize.Div(tOrd.Price)
			matchSize = maxSize
		}

		if matchSize.GreaterThan(tOrd.Size) {
			matchSize = tOrd.Size
		}

		// Check if match size is below minimum trade unit (LotSize)
		// This prevents infinite loops when quoteSize/price produces very small values.
		if matchSize.LessThan(book.lotSize) {
			// Cannot match anymore (remaining quantity below minimum trade unit)
			// Produce Reject Log so OMS can unfreeze the remaining funds.
			log := NewRejectLog(
				book.seqID.Add(1),
				commandID,
				book.engineID,
				book.marketID,
				order.ID,
				order.UserID,
				protocol.RejectReasonNoLiquidity,
				timestamp,
			)
			log.Side, log.Price, log.OrderType = order.Side, order.Price, order.Type
			if useQuote {
				log.Size = quoteSize // Remaining quote size that couldn't be matched
			} else {
				log.Size = order.Size
			}
			batch.Logs = append(batch.Logs, log)
			releaseOrder(order)
			break
		}

		log := NewMatchLog(
			book.seqID.Add(1),
			commandID,
			book.engineID,
			book.tradeID.Add(1),
			book.marketID,
			order.ID,
			order.UserID,
			order.Side,
			order.Type,
			tOrd.ID,
			tOrd.UserID,
			tOrd.Price,
			matchSize,
			timestamp,
		)
		batch.Logs = append(batch.Logs, log)

		if useQuote {
			quoteSize = quoteSize.Sub(matchSize.Mul(tOrd.Price))
		} else {
			order.Size = order.Size.Sub(matchSize)
		}

		tOrd = targetQueue.popHeadOrder()
		if matchSize.Equal(tOrd.Size) {
			if !book.checkReplenish(commandID, tOrd, targetQueue, batch, timestamp) {
				releaseOrder(tOrd)
			}
		} else {
			tOrd.Size = tOrd.Size.Sub(matchSize)
			targetQueue.pushFront(tOrd)
		}

		// Termination condition
		if (useQuote && quoteSize.IsZero()) || (!useQuote && order.Size.IsZero()) {
			releaseOrder(order)
			break
		}
	}
	return batch
}

func (book *OrderBook) checkReplenish(
	commandID string,
	order *Order,
	q *queue,
	batch *LogBatch,
	timestamp int64,
) bool {
	if order.HiddenSize.GreaterThan(udecimal.Zero) {
		reloadQty := order.VisibleLimit
		if order.HiddenSize.LessThan(reloadQty) {
			reloadQty = order.HiddenSize
		}
		order.Size = reloadQty
		order.HiddenSize = order.HiddenSize.Sub(reloadQty)
		order.Timestamp = timestamp
		q.insertOrder(order) // Insert at end (Priority Loss)

		log := NewOpenLog(
			book.seqID.Add(1),
			commandID,
			book.engineID,
			book.marketID,
			order.ID,
			order.UserID,
			order.Side,
			order.Price,
			order.Size,
			order.Type,
			timestamp,
		)
		batch.Logs = append(batch.Logs, log)
		return true
	}
	return false
}
