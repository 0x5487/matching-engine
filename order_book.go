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

// OrderBookOption configures an OrderBook.
type OrderBookOption func(*OrderBook)

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
	skiplistSeed int64 // PRNG seed for price skiplist
	// seqID is this book's depth version. One SDK call that moves visible depth
	// advances it by exactly one; the logs of that call all carry the new value.
	// Per book, never global.
	seqID        atomic.Uint64
	lastCmdSeqID atomic.Uint64 // Last sequence ID of the command
	tradeID      atomic.Uint64 // Sequential trade ID counter
	// callSeq is the version the call in progress produces: seqID+1 while the call
	// runs. callMoved records whether a log of the call has taken it.
	callSeq   uint64
	callMoved bool
	// batch holds the logs of the call in progress and of the last one. The book
	// reclaims them when its next call starts; see LogBatch.
	batch     LogBatch
	bidQueue  *queue
	askQueue  *queue
	publisher Publisher
}

// NewOrderBook creates a new pure OrderBook instance.
func NewOrderBook(
	marketID string,
	opts ...OrderBookOption,
) *OrderBook {
	book := &OrderBook{
		engineID:     "default",
		marketID:     marketID,
		skiplistSeed: defaultSkiplistSeed,
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
) *OrderBook {
	return NewOrderBook(marketID,
		WithEngineID(engineID),
		WithPublisher(publishTrader),
	)
}

// LastCmdSeqID returns the sequence ID of the last processed command.
func (book *OrderBook) LastCmdSeqID() uint64 {
	return book.lastCmdSeqID.Load()
}

// Restore restores the order book state from a snapshot.
func (book *OrderBook) Restore(snap *OrderBookSnapshot) {
	book.seqID.Store(snap.SeqID)
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

// SeqID returns the current book depth version: how many SDK calls have moved
// visible depth. A call that moves none, such as a refusal or an admin command,
// does not advance it.
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
// The batch belongs to the book: it and its logs are valid until the next call on
// this book, which reclaims them.
func (book *OrderBook) PlaceOrder(req *protocol.PlaceOrderRequest) (*LogBatch, error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	book.beginCall()
	defer book.endCall()
	book.batch.reclaim()
	if req.Timestamp <= 0 {
		return book.rejectCall(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonInvalidPayload,
			req.Timestamp,
		)
	}

	// Basic validation
	isValid := true
	if req.OrderType == Market {
		if req.Size.IsZero() && req.QuoteSize.IsZero() {
			isValid = false
		}
		// A quote-sized order converts by dividing by each level's price, and the
		// result has to be floored to the step size to stay expressible as a base
		// balance. Without a step size there is nothing to floor to and no unit to
		// hold the balance in, so the order cannot be executed as described. This is
		// rejected rather than defaulted: a default unit would silently write
		// balances the ledger cannot represent.
		if req.Size.IsZero() && req.StepSize.IsZero() {
			isValid = false
		}
		// A market order's Price is an optional protection price, not a limit: zero
		// means none, and a negative bound has no meaning on either side.
		if req.Price.LessThan(udecimal.Zero) {
			isValid = false
		}
	} else if req.Price.IsZero() || req.Size.IsZero() {
		isValid = false
	}

	// A quantity-sized order carrying a step size must be a whole number of
	// steps, measured from zero. The book never floors the quantity path, so
	// whatever arrives here is what the fills will carry: refusing dust at
	// the door is the only place the book can keep it out.
	//
	// Measured from zero, not from a minimum: the book holds no market rules
	// and never sees minQuantity, so it cannot anchor anywhere else. Callers
	// that own the rules (exchange APIs) check against the minimum; the two
	// agree wherever the minimum is itself a whole number of steps.
	//
	// Skipped when no step travels with the order. A zero step means "no
	// unit to measure against", and older callers that never send one keep
	// the old behavior rather than failing every order overnight.
	//
	// AmendOrder applies the same check to its new size, against the step it
	// carries.
	if isValid && !req.Size.IsZero() && !req.StepSize.IsZero() &&
		!isWholeSteps(req.Size, req.StepSize) {
		isValid = false
	}
	// The visible part of an iceberg rests on the book, so it answers the
	// same question as the size itself.
	if isValid && !req.VisibleSize.IsZero() && !req.StepSize.IsZero() &&
		!isWholeSteps(req.VisibleSize, req.StepSize) {
		isValid = false
	}

	if !isValid {
		return book.rejectCall(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonInvalidPayload,
			req.Timestamp,
		)
	}

	// Check for duplicate ID
	if book.bidQueue.order(req.OrderID) != nil || book.askQueue.order(req.OrderID) != nil {
		return book.rejectCall(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonDuplicateID,
			req.Timestamp,
		)
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

	switch order.Type {
	case Limit:
		book.handleLimitOrder(req.CommandID, order, req.Timestamp)
	case FOK:
		book.handleFOKOrder(req.CommandID, order, req.Timestamp)
	case IOC:
		book.handleIOCOrder(req.CommandID, order, req.Timestamp)
	case PostOnly:
		book.handlePostOnlyOrder(req.CommandID, order, req.Timestamp)
	case Market:
		book.handleMarketOrder(req.CommandID, order, req.QuoteSize, req.StepSize, req.Timestamp)
	default:
		releaseOrder(order)
		return nil, ErrInvalidParam
	}

	return &book.batch, nil
}

// isWholeSteps reports whether size is a whole number of step.
//
// A Mod error is unreachable for a non-zero step, and a size that cannot be
// divided is not a whole number of anything, so both report false and the
// order is refused. Failing closed is the point: an order the book cannot
// measure is not an order the book should fill.
func isWholeSteps(size, step udecimal.Decimal) bool {
	rem, err := size.Mod(step)
	if err != nil {
		return false
	}
	return rem.IsZero()
}

// CancelOrder processes an order cancellation synchronously and returns the resulting LogBatch.
// The batch belongs to the book: it and its logs are valid until the next call on
// this book, which reclaims them.
func (book *OrderBook) CancelOrder(req *protocol.CancelOrderRequest) (*LogBatch, error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	book.beginCall()
	defer book.endCall()
	book.batch.reclaim()
	if req.Timestamp <= 0 || req.OrderID == "" {
		return book.rejectCall(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonInvalidPayload,
			req.Timestamp,
		)
	}

	order, ok := book.findOrder(req.OrderID)
	if !ok || order.UserID != req.UserID {
		return book.rejectCall(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonOrderNotFound,
			req.Timestamp,
		)
	}

	myQueue := book.bidQueue
	if order.Side == Sell {
		myQueue = book.askQueue
	}

	myQueue.removeOrder(order.Price, order.ID)
	totalSize := order.Size.Add(order.HiddenSize)
	log := NewCancelLog(
		book.depthSeq(),
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
	// The whole visible part leaves the book at its price. The hidden part was
	// never depth, so it does not appear here even though Size carries it.
	log.VisibleRemoved = order.Size
	book.batch.Logs = append(book.batch.Logs, log)
	releaseOrder(order)

	return &book.batch, nil
}

// amendChangesDepth reports whether an amend alters the book's visible depth.
//
// An amend moves visible size whenever it relocates the order - a price change
// emits an amend plus an open at the new price - or grows it. A shrink that stays
// inside the hidden part moves no visible size: the order keeps its place, its
// visible part is unchanged, and the book is the same as before. Such an amend
// reuses the current version, exactly like a no-op.
//
// A no-op amend is the one command that reaches the amend log without changing
// the book: the new price and size both resolve to the current ones, so none of
// the three conditions hold and neither AmendOrder branch is taken.
func amendChangesDepth(isPriceChange, isSizeIncrease bool, visibleRemoved udecimal.Decimal) bool {
	return isPriceChange || isSizeIncrease || visibleRemoved.GreaterThan(udecimal.Zero)
}

// AmendOrder processes an order amendment synchronously and returns the resulting LogBatch.
// The batch belongs to the book: it and its logs are valid until the next call on
// this book, which reclaims them.
func (book *OrderBook) AmendOrder(req *protocol.AmendOrderRequest) (*LogBatch, error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	book.beginCall()
	defer book.endCall()
	book.batch.reclaim()
	// A new size must be a whole number of steps when the amend names a step. Zero
	// on either side means no size or no unit is given, and nothing is checked.
	offStep := !req.NewSize.IsZero() && !req.StepSize.IsZero() && !isWholeSteps(req.NewSize, req.StepSize)
	if req.Timestamp <= 0 || req.OrderID == "" || offStep {
		return book.rejectCall(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonInvalidPayload,
			req.Timestamp,
		)
	}

	order, ok := book.findOrder(req.OrderID)
	if !ok || order.UserID != req.UserID {
		return book.rejectCall(
			req.CommandID,
			req.OrderID,
			req.UserID,
			protocol.RejectReasonOrderNotFound,
			req.Timestamp,
		)
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

	// The visible size this amend takes off the book. Losing priority removes the
	// whole visible part at the old price. An in-place shrink removes only the part
	// of the decrease that reaches past the hidden size; a shrink that stays inside
	// the hidden size removes nothing from depth.
	var visibleRemoved udecimal.Decimal
	switch {
	case isPriceChange || isSizeIncrease:
		visibleRemoved = order.Size
	case newSize.LessThan(oldTotalSize):
		if delta := oldTotalSize.Sub(newSize); delta.GreaterThan(order.HiddenSize) {
			visibleRemoved = delta.Sub(order.HiddenSize)
		}
	default:
		visibleRemoved = udecimal.Zero
	}

	// Computed once and reused for both the version and the branch below, so the
	// two cannot disagree about whether the book changed.
	depthChanged := amendChangesDepth(isPriceChange, isSizeIncrease, visibleRemoved)
	// An amend that changes no visible depth (a no-op, or a shrink inside the
	// hidden size) must not advance the version. Emitting the log is still right -
	// the command happened and an operator wants to see it - but the version
	// belongs to visible depth only.
	amendSeqID := book.seqID.Load()
	if depthChanged {
		amendSeqID = book.depthSeq()
	}
	log := NewAmendLog(
		amendSeqID,
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
	log.VisibleRemoved = visibleRemoved
	book.batch.Logs = append(book.batch.Logs, log)

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

		book.handleLimitOrder(req.CommandID, order, req.Timestamp)
	} else if newSize.LessThan(oldTotalSize) {
		// Path 2: Priority Retention (In-place update)
		delta := oldTotalSize.Sub(newSize)
		if delta.LessThanOrEqual(order.HiddenSize) {
			myQueue.resizeOrder(order.ID, order.Size, order.HiddenSize.Sub(delta))
		} else {
			remainingDelta := delta.Sub(order.HiddenSize)
			myQueue.resizeOrder(order.ID, order.Size.Sub(remainingDelta), udecimal.Zero)
		}
	}

	return &book.batch, nil
}

// LevelSize returns the visible size resting at price on side: the depth of that
// level, not counting hidden iceberg size. It returns zero when no order rests
// there. It does not allocate.
func (book *OrderBook) LevelSize(side Side, price udecimal.Decimal) udecimal.Decimal {
	if side == Sell {
		return book.askQueue.levelSize(price)
	}
	return book.bidQueue.levelSize(price)
}

// BestPrice returns the best price on side: the highest bid or the lowest ask. The
// second result is false when that side is empty. It does not allocate.
func (book *OrderBook) BestPrice(side Side) (udecimal.Decimal, bool) {
	if side == Sell {
		return book.askQueue.bestPrice()
	}
	return book.bidQueue.bestPrice()
}

// beginCall starts an SDK call. The call's depth logs take the next version, and
// the book advances to it only if the call moves visible depth (see endCall).
func (book *OrderBook) beginCall() {
	book.callSeq = book.seqID.Load() + 1
	book.callMoved = false
}

// depthSeq returns the version for a log that moves visible depth, and marks the
// call as having moved depth.
func (book *OrderBook) depthSeq() uint64 {
	book.callMoved = true
	return book.callSeq
}

// endCall finishes an SDK call. The version advances by one when the call moved
// visible depth and not otherwise, so a refusal, a no-op amend or a shrink inside
// the hidden size leaves the version where it was.
func (book *OrderBook) endCall() {
	if book.callMoved {
		book.seqID.Store(book.callSeq)
	}
}

func (book *OrderBook) processCommand(ev *InputEvent) {
	req := ev.Request
	switch request := req.(type) {
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

// rejectCall ends a call with one reject log and returns the book's batch.
func (book *OrderBook) rejectCall(
	commandID string,
	orderID string,
	userID uint64,
	reason protocol.RejectReason,
	timestamp int64,
) (*LogBatch, error) {
	log := NewRejectLog(
		0,
		commandID,
		book.engineID,
		book.marketID,
		orderID,
		userID,
		reason,
		timestamp,
	)
	book.batch.Logs = append(book.batch.Logs, log)
	return &book.batch, nil
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
					return
				}
			}
		}
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
					return
				}
			}
		}
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
					return
				}
			}
		}
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
	book.batch.reclaim()
	log := NewRejectLog(
		0,
		commandID,
		book.engineID,
		marketID,
		orderID,
		userID,
		reason,
		timestamp,
	)
	book.batch.Logs = append(book.batch.Logs, log)
	book.publisher.Publish(book.batch.Logs)
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
	}
}

// handleLimitOrder handles Limit orders.
func (book *OrderBook) handleLimitOrder(commandID string, order *Order, timestamp int64) {
	var myQueue, targetQueue *queue
	if order.Side == Buy {
		myQueue = book.bidQueue
		targetQueue = book.askQueue
	} else {
		myQueue = book.askQueue
		targetQueue = book.bidQueue
	}

	for {
		tOrd := targetQueue.peekHeadOrder()
		if tOrd == nil {
			book.prepareIcebergForResting(order)
			myQueue.insertOrder(order)
			log := NewOpenLog(
				book.depthSeq(),
				commandID,
				book.engineID,
				book.marketID,
				order.ID,
				order.UserID,
				order.Side,
				order.Price,
				order.Size,
				order.Size.Add(order.HiddenSize),
				order.Type,
				timestamp,
			)
			book.batch.Logs = append(book.batch.Logs, log)
			return
		}

		if (order.Side == Buy && order.Price.LessThan(tOrd.Price)) ||
			(order.Side == Sell && order.Price.GreaterThan(tOrd.Price)) {
			book.prepareIcebergForResting(order)
			myQueue.insertOrder(order)
			log := NewOpenLog(
				book.depthSeq(),
				commandID,
				book.engineID,
				book.marketID,
				order.ID,
				order.UserID,
				order.Side,
				order.Price,
				order.Size,
				order.Size.Add(order.HiddenSize),
				order.Type,
				timestamp,
			)
			book.batch.Logs = append(book.batch.Logs, log)
			return
		}

		tOrd = targetQueue.popHeadOrder()
		if order.Size.LessThan(tOrd.Size) {
			log := NewMatchLog(
				book.depthSeq(),
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
			book.batch.Logs = append(book.batch.Logs, log)
			tOrd.Size = tOrd.Size.Sub(order.Size)
			targetQueue.pushFront(tOrd)
			releaseOrder(order)
			break
		}

		log := NewMatchLog(
			book.depthSeq(),
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
		book.batch.Logs = append(book.batch.Logs, log)
		order.Size = order.Size.Sub(tOrd.Size)

		if !book.checkReplenish(commandID, tOrd, targetQueue, timestamp) {
			releaseOrder(tOrd)
		}

		if order.Size.Equal(udecimal.Zero) {
			releaseOrder(order)
			break
		}
	}
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
//
// An IOC is a taker walk whose price is a required bound: Price is the limit the
// order will pay or accept, and a Price of zero is a bound of zero, not "no
// bound". The remainder blocked by that bound is a price mismatch. An IOC is
// always sized in base.
func (book *OrderBook) handleIOCOrder(commandID string, order *Order, timestamp int64) {
	book.takeLiquidity(commandID, order, takerWalk{
		priceRequired: true,
		blockedReason: protocol.RejectReasonPriceMismatch,
	}, timestamp)
}

// handleFOKOrder handles Fill Or Kill orders.
func (book *OrderBook) handleFOKOrder(commandID string, order *Order, timestamp int64) {
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
		// The level can give its visible size and the iceberg size behind it: a
		// taker that eats the visible part reloads the hidden part at the same price.
		levelTotal := unit.totalSize.Add(unit.hiddenSize)
		if remainingSize.LessThanOrEqual(levelTotal) {
			canFill = true
			break
		}
		remainingSize = remainingSize.Sub(levelTotal)
		it.Next()
	}

	if !canFill {
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
			0,
			commandID,
			book.engineID,
			book.marketID,
			order.ID,
			order.UserID,
			reason,
			timestamp,
		)
		log.Side, log.Price, log.Remaining, log.OrderType = order.Side, order.Price, order.Size, order.Type
		book.batch.Logs = append(book.batch.Logs, log)
		releaseOrder(order)
		return
	}

	// Phase 2: Execute match (same as IOC/Limit logic but guaranteed to finish)
	book.handleIOCOrder(commandID, order, timestamp)
}

// handlePostOnlyOrder handles Post-Only orders.
func (book *OrderBook) handlePostOnlyOrder(
	commandID string,
	order *Order,
	timestamp int64,
) {
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
			log := NewRejectLog(
				0,
				commandID,
				book.engineID,
				book.marketID,
				order.ID,
				order.UserID,
				protocol.RejectReasonPostOnlyMatch,
				timestamp,
			)
			log.Side, log.Price, log.Remaining, log.OrderType = order.Side, order.Price, order.Size, order.Type
			book.batch.Logs = append(book.batch.Logs, log)
			releaseOrder(order)
			return
		}
	}

	book.handleLimitOrder(commandID, order, timestamp)
}

// handleMarketOrder handles Market orders.
//
// A Market order is a taker walk whose price is an optional protection price:
// a Price of zero means no bound. The remainder blocked by that bound is a price
// protection reject. A Market order may also be sized in quote, in which case
// quoteSize and stepSize drive the walk.
func (book *OrderBook) handleMarketOrder(
	commandID string,
	order *Order,
	quoteSize udecimal.Decimal,
	stepSize udecimal.Decimal,
	timestamp int64,
) {
	book.takeLiquidity(commandID, order, takerWalk{
		blockedReason: protocol.RejectReasonPriceProtection,
		quoteSize:     quoteSize,
		stepSize:      stepSize,
	}, timestamp)
}

// takerWalk carries what differs between the orders that take liquidity. Each
// handler decides these; takeLiquidity never looks at the order's type to decide
// them.
type takerWalk struct {
	// priceRequired makes the order's Price a bound even when it is zero. Without
	// it a zero Price means the walk has no bound.
	priceRequired bool
	// blockedReason is the reject reason when the next level lies beyond the bound.
	blockedReason protocol.RejectReason
	// quoteSize, when non-zero on an order with no base Size, is the quote the walk
	// spends instead of a quantity, and what is left of it is the reject's Size.
	quoteSize udecimal.Decimal
	// stepSize is the unit the quote conversion is floored to.
	stepSize udecimal.Decimal
}

// takeLiquidity walks the side opposite the taker level by level, filling until the
// order is done, the next level lies beyond the price bound, or the book has no
// more liquidity. It is the one place a taker order matches.
func (book *OrderBook) takeLiquidity(
	commandID string,
	order *Order,
	walk takerWalk,
	timestamp int64,
) {
	quoteSize := walk.quoteSize
	var targetQueue *queue
	if order.Side == Buy {
		targetQueue = book.askQueue
	} else {
		targetQueue = book.bidQueue
	}

	for {
		tOrd := targetQueue.peekHeadOrder()
		if tOrd == nil {
			log := NewRejectLog(
				0,
				commandID,
				book.engineID,
				book.marketID,
				order.ID,
				order.UserID,
				protocol.RejectReasonNoLiquidity,
				timestamp,
			)
			log.Side, log.Price, log.OrderType = order.Side, order.Price, order.Type
			if !quoteSize.IsZero() {
				log.QuoteRemaining = quoteSize
			} else {
				log.Remaining = order.Size
			}
			book.batch.Logs = append(book.batch.Logs, log)
			releaseOrder(order)
			break
		}

		useQuote := order.Size.IsZero() && !quoteSize.IsZero()
		// The price bound stops the walk at the first level beyond it, and the
		// remainder goes back unfrozen. The bound is inclusive.
		if (walk.priceRequired || order.Price.GreaterThan(udecimal.Zero)) &&
			beyondProtectionPrice(order.Side, tOrd.Price, order.Price) {
			log := NewRejectLog(
				0,
				commandID,
				book.engineID,
				book.marketID,
				order.ID,
				order.UserID,
				walk.blockedReason,
				timestamp,
			)
			log.Side, log.Price, log.OrderType = order.Side, order.Price, order.Type
			if useQuote {
				log.QuoteRemaining = quoteSize // Remaining quote size the price bound left unmatched
			} else {
				log.Remaining = order.Size
			}
			book.batch.Logs = append(book.batch.Logs, log)
			releaseOrder(order)
			break
		}

		matchSize := order.Size
		if useQuote {
			// Floor first, then take the minimum with the resting order. The order
			// of those two operations is what keeps an offer placed under an older,
			// finer step reachable: flooring the minimum would send 0.0003 to zero at
			// a 0.001 step, reject an order the book could have filled, and let one
			// holder's dust stop every order behind it.
			//
			// Only the quote path needs flooring. When both sizes are whole steps the
			// result already is, so flooring a quantity-based order would change
			// nothing and cost a division on the hot path.
			maxSize, _ := quoteSize.Div(tOrd.Price)
			steps, _ := maxSize.Div(walk.stepSize)
			matchSize = steps.Floor().Mul(walk.stepSize)
		}

		if matchSize.GreaterThan(tOrd.Size) {
			matchSize = tOrd.Size
		}

		// A quote conversion can floor to zero when the remaining quote no longer
		// buys one step. Matching on would either log a zero-size fill or leave the
		// loop spinning, since the quote would not shrink. Zero is therefore also the
		// loop guard the old lot-size check provided, and a stricter one: after this
		// point a non-zero matchSize is always at least one whole step.
		if matchSize.IsZero() {
			// Produce Reject Log so OMS can unfreeze the remaining funds.
			log := NewRejectLog(
				0,
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
				log.QuoteRemaining = quoteSize // Remaining quote size that couldn't be matched
			} else {
				log.Remaining = order.Size
			}
			book.batch.Logs = append(book.batch.Logs, log)
			releaseOrder(order)
			break
		}

		log := NewMatchLog(
			book.depthSeq(),
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
		book.batch.Logs = append(book.batch.Logs, log)

		if useQuote {
			quoteSize = quoteSize.Sub(matchSize.Mul(tOrd.Price))
		} else {
			order.Size = order.Size.Sub(matchSize)
		}

		tOrd = targetQueue.popHeadOrder()
		if matchSize.Equal(tOrd.Size) {
			if !book.checkReplenish(commandID, tOrd, targetQueue, timestamp) {
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
}

// beyondProtectionPrice reports whether a resting level at makerPrice lies outside
// the protection price for a taker on side. A buyer will not pay above protection
// and a seller will not accept below it; a level exactly at protection is takeable.
func beyondProtectionPrice(side Side, makerPrice, protection udecimal.Decimal) bool {
	if side == Buy {
		return makerPrice.GreaterThan(protection)
	}
	return makerPrice.LessThan(protection)
}

func (book *OrderBook) checkReplenish(
	commandID string,
	order *Order,
	q *queue,
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
			book.depthSeq(),
			commandID,
			book.engineID,
			book.marketID,
			order.ID,
			order.UserID,
			order.Side,
			order.Price,
			order.Size,
			order.Size.Add(order.HiddenSize),
			order.Type,
			timestamp,
		)
		book.batch.Logs = append(book.batch.Logs, log)
		return true
	}
	return false
}
