package match

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/0x5487/matching-engine/protocol"
	"github.com/quagmt/udecimal"
)

// orderPool is used to reduce Order allocations in the hot path.
var orderPool = sync.Pool{
	New: func() any {
		return &Order{}
	},
}

// Command payload pools to reduce allocations during Unmarshal
var placeOrderCmdPool = sync.Pool{
	New: func() any {
		return &protocol.PlaceOrderCommand{}
	},
}

var cancelOrderCmdPool = sync.Pool{
	New: func() any {
		return &protocol.CancelOrderCommand{}
	},
}

func acquireOrder() *Order {
	return orderPool.Get().(*Order)
}

func releaseOrder(o *Order) {
	*o = Order{}
	orderPool.Put(o)
}

// DefaultLotSize is the fallback minimum trade unit (1e-8).
// This prevents infinite loops when quoteSize/price produces very small values.
var DefaultLotSize = udecimal.MustFromInt64(1, 8) // 0.00000001

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

// OrderBook is a pure logic object that maintains the state of an order book.
// It must be managed by a MatchingEngine which provides the event loop.
type OrderBook struct {
	marketID      string
	lotSize       udecimal.Decimal // Minimum trade unit for Market orders
	seqID         atomic.Uint64    // Globally increasing sequence ID
	lastCmdSeqID  atomic.Uint64    // Last sequence ID of the command
	tradeID       atomic.Uint64    // Sequential trade ID counter
	bidQueue      *queue
	askQueue      *queue
	publishTrader PublishLog
	serializer    protocol.Serializer
	state         protocol.OrderBookState
}

// newOrderBook creates a new OrderBook instance.
// OrderBooks are managed by a MatchingEngine and do not have their own event loop.
func newOrderBook(marketID string, publishTrader PublishLog, opts ...OrderBookOption) *OrderBook {
	book := &OrderBook{
		marketID:      marketID,
		lotSize:       DefaultLotSize,
		bidQueue:      NewBuyerQueue(),
		askQueue:      NewSellerQueue(),
		publishTrader: publishTrader,
	}

	for _, opt := range opts {
		opt(book)
	}

	if book.serializer == nil {
		book.serializer = &protocol.DefaultJSONSerializer{}
	}

	// Explicitly set initial state (Review 8.4.3)
	book.state = protocol.OrderBookStateRunning

	return book
}

// LastCmdSeqID returns the sequence ID of the last processed command.
func (book *OrderBook) LastCmdSeqID() uint64 {
	return book.lastCmdSeqID.Load()
}

func (book *OrderBook) processCommand(cmd *protocol.Command) {
	switch cmd.Type {
	case protocol.CmdSuspendMarket:
		payload := &protocol.SuspendMarketCommand{}
		if err := book.serializer.Unmarshal(cmd.Payload, payload); err != nil {
			book.rejectInvalidPayload("unknown", 0, protocol.RejectReasonInvalidPayload, cmd.Metadata)
			return
		}
		book.handleSuspendMarket(payload)

	case protocol.CmdResumeMarket:
		payload := &protocol.ResumeMarketCommand{}
		if err := book.serializer.Unmarshal(cmd.Payload, payload); err != nil {
			book.rejectInvalidPayload("unknown", 0, protocol.RejectReasonInvalidPayload, cmd.Metadata)
			return
		}
		book.handleResumeMarket(payload)

	case protocol.CmdUpdateConfig:
		payload := &protocol.UpdateConfigCommand{}
		if err := book.serializer.Unmarshal(cmd.Payload, payload); err != nil {
			book.rejectInvalidPayload("unknown", 0, protocol.RejectReasonInvalidPayload, cmd.Metadata)
			return
		}
		book.handleUpdateConfig(payload)

	case protocol.CmdPlaceOrder:
		payload := placeOrderCmdPool.Get().(*protocol.PlaceOrderCommand)
		*payload = protocol.PlaceOrderCommand{} // Reset before use
		if err := book.serializer.Unmarshal(cmd.Payload, payload); err != nil {
			placeOrderCmdPool.Put(payload)
			book.rejectInvalidPayload("unknown", 0, protocol.RejectReasonInvalidPayload, cmd.Metadata)
			return
		}

		if book.state == protocol.OrderBookStateHalted {
			book.rejectInvalidPayload(payload.OrderID, payload.UserID, protocol.RejectReasonMarketHalted, cmd.Metadata)
			placeOrderCmdPool.Put(payload)
			return
		}
		if book.state == protocol.OrderBookStateSuspended {
			book.rejectInvalidPayload(payload.OrderID, payload.UserID, protocol.RejectReasonMarketSuspended, cmd.Metadata)
			placeOrderCmdPool.Put(payload)
			return
		}

		book.handlePlaceOrder(payload)
		placeOrderCmdPool.Put(payload)
	case protocol.CmdCancelOrder:
		payload := cancelOrderCmdPool.Get().(*protocol.CancelOrderCommand)
		*payload = protocol.CancelOrderCommand{} // Reset before use
		if err := book.serializer.Unmarshal(cmd.Payload, payload); err != nil {
			cancelOrderCmdPool.Put(payload)
			book.rejectInvalidPayload("unknown", 0, protocol.RejectReasonInvalidPayload, cmd.Metadata)
			return
		}

		// Cancel is allowed in Suspended state, but not Halted
		if book.state == protocol.OrderBookStateHalted {
			book.rejectInvalidPayload(payload.OrderID, payload.UserID, protocol.RejectReasonMarketHalted, cmd.Metadata)
			cancelOrderCmdPool.Put(payload)
			return
		}

		book.handleCancelOrder(payload)
		cancelOrderCmdPool.Put(payload)
	case protocol.CmdAmendOrder:
		var payload protocol.AmendOrderCommand
		if err := book.serializer.Unmarshal(cmd.Payload, &payload); err != nil {
			book.rejectInvalidPayload("unknown", 0, protocol.RejectReasonInvalidPayload, cmd.Metadata)
			return
		}

		if book.state == protocol.OrderBookStateHalted {
			book.rejectInvalidPayload(payload.OrderID, payload.UserID, protocol.RejectReasonMarketHalted, cmd.Metadata)
			return
		}
		if book.state == protocol.OrderBookStateSuspended {
			book.rejectInvalidPayload(payload.OrderID, payload.UserID, protocol.RejectReasonMarketSuspended, cmd.Metadata)
			return
		}

		book.handleAmendOrder(&payload)
	default:
		// Ignore unknown command types
	}

	if cmd.SeqID > 0 {
		book.lastCmdSeqID.Store(cmd.SeqID)
	}
}

func (book *OrderBook) processQuery(ev *InputEvent) {
	switch q := ev.Query.(type) {
	case *protocol.GetDepthRequest:
		result := book.depth(q.Limit)
		if ev.Resp != nil {
			select {
			case ev.Resp <- result:
			default:
			}
		}
	case *protocol.GetStatsRequest:
		stats := &protocol.GetStatsResponse{
			AskDepthCount: book.askQueue.depthCount(),
			AskOrderCount: book.askQueue.orderCount(),
			BidDepthCount: book.bidQueue.depthCount(),
			BidOrderCount: book.bidQueue.orderCount(),
		}
		if ev.Resp != nil {
			select {
			case ev.Resp <- stats:
			default:
			}
		}
	case *OrderBookSnapshot: // Snapshot query
		snap := book.createSnapshot()
		if ev.Resp != nil {
			select {
			case ev.Resp <- snap:
			default:
			}
		}
	}
}

func (book *OrderBook) rejectInvalidPayload(orderID string, userID uint64, reason protocol.RejectReason, _ map[string]string) {
	logsPtr := acquireLogSlice()
	log := NewRejectLog(book.seqID.Add(1), book.marketID, orderID, userID, reason, time.Now().UnixNano())
	*logsPtr = append(*logsPtr, log)
	book.publishTrader.Publish(*logsPtr)
	releaseBookLog(log)
	releaseLogSlice(logsPtr)
}

// handlePlaceOrder matches protocol Payloads to internal logic.
func (book *OrderBook) handlePlaceOrder(cmd *protocol.PlaceOrderCommand) {
	// Parse strings to decimals
	price, err := udecimal.Parse(cmd.Price)
	if err != nil {
		book.rejectInvalidPayload(cmd.OrderID, cmd.UserID, protocol.RejectReasonInvalidPayload, nil)
		return
	}
	size, err := udecimal.Parse(cmd.Size)
	if err != nil {
		book.rejectInvalidPayload(cmd.OrderID, cmd.UserID, protocol.RejectReasonInvalidPayload, nil)
		return
	}

	visibleSize, _ := udecimal.Parse(cmd.VisibleSize)
	quoteSize, _ := udecimal.Parse(cmd.QuoteSize)

	book.placeOrder(cmd.OrderID, cmd.Side, price, size, visibleSize, quoteSize, cmd.OrderType, cmd.UserID, cmd.Timestamp)
}

func (book *OrderBook) handleCancelOrder(cmd *protocol.CancelOrderCommand) {
	book.cancelOrder(cmd.OrderID, cmd.UserID, cmd.Timestamp)
}

func (book *OrderBook) handleAmendOrder(cmd *protocol.AmendOrderCommand) {
	newPrice, _ := udecimal.Parse(cmd.NewPrice)
	newSize, _ := udecimal.Parse(cmd.NewSize)
	book.amendOrder(cmd.OrderID, cmd.UserID, newPrice, newSize, cmd.Timestamp)
}

// placeOrder processes the addition of an order.
func (book *OrderBook) placeOrder(orderID string, side Side, price, size, visibleSize, quoteSize udecimal.Decimal, orderType OrderType, userID uint64, timestamp int64) {
	if book.bidQueue.order(orderID) != nil || book.askQueue.order(orderID) != nil {
		logsPtr := acquireLogSlice()
		log := NewRejectLog(book.seqID.Add(1), book.marketID, orderID, userID, protocol.RejectReasonDuplicateID, timestamp)
		*logsPtr = append(*logsPtr, log)
		book.publishTrader.Publish(*logsPtr)
		releaseBookLog(log)
		releaseLogSlice(logsPtr)
		return
	}

	order := acquireOrder()
	order.ID = orderID
	order.Side = side
	order.Price = price
	order.Size = size
	order.Type = orderType
	order.UserID = userID
	order.Timestamp = timestamp

	// For Iceberg orders, store VisibleLimit but DON'T split Size yet.
	// The split happens when the order enters the book (Maker/Passive phase),
	// not during aggressive matching (Taker phase).
	// This follows spec 3.2.A: "Use full Size for aggressive matching."
	if visibleSize.GreaterThan(udecimal.Zero) && visibleSize.LessThan(size) {
		order.VisibleLimit = visibleSize
		// HiddenSize and Size adjustment will happen in handleLimitOrder when resting
	}

	var logsPtr *[]*OrderBookLog
	switch order.Type {
	case Limit:
		logsPtr = book.handleLimitOrder(order, timestamp)
	case FOK:
		logsPtr = book.handleFOKOrder(order, timestamp)
	case IOC:
		logsPtr = book.handleIOCOrder(order, timestamp)
	case PostOnly:
		logsPtr = book.handlePostOnlyOrder(order, timestamp)
	case Market:
		logsPtr = book.handleMarketOrder(order, quoteSize, timestamp)
	default:
		// Ignore or handle unknown types
	}

	if logsPtr != nil {
		if len(*logsPtr) > 0 {
			book.publishTrader.Publish(*logsPtr)
			for _, log := range *logsPtr {
				releaseBookLog(log)
			}
		}
		releaseLogSlice(logsPtr)
	}
}

// amendOrder processes the modification of an order.
func (book *OrderBook) amendOrder(orderID string, userID uint64, newPrice, newSize udecimal.Decimal, timestamp int64) {
	order, ok := book.findOrder(orderID)
	if !ok || order.UserID != userID {
		logsPtr := acquireLogSlice()
		log := NewRejectLog(book.seqID.Add(1), book.marketID, orderID, userID, protocol.RejectReasonOrderNotFound, timestamp)
		*logsPtr = append(*logsPtr, log)
		book.publishTrader.Publish(*logsPtr)
		releaseBookLog(log)
		releaseLogSlice(logsPtr)
		return
	}

	myQueue := book.bidQueue
	if order.Side == Sell {
		myQueue = book.askQueue
	}

	oldPrice := order.Price
	oldTotalSize := order.Size.Add(order.HiddenSize)

	isPriceChange := !oldPrice.Equal(newPrice)
	isSizeIncrease := newSize.GreaterThan(oldTotalSize)

	if isPriceChange || isSizeIncrease {
		// Path 1: Priority Loss (Re-match)
		myQueue.removeOrder(oldPrice, order.ID)
		order.Price = newPrice
		order.Timestamp = timestamp

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

		log := NewAmendLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Price, newSize, oldPrice, oldTotalSize, order.Type, timestamp)
		amendLogsPtr := acquireLogSlice()
		*amendLogsPtr = append(*amendLogsPtr, log)
		book.publishTrader.Publish(*amendLogsPtr)
		releaseBookLog(log)
		releaseLogSlice(amendLogsPtr)

		logsPtr := book.handleLimitOrder(order, timestamp)
		if logsPtr != nil {
			if len(*logsPtr) > 0 {
				book.publishTrader.Publish(*logsPtr)
				for _, log := range *logsPtr {
					releaseBookLog(log)
				}
			}
			releaseLogSlice(logsPtr)
		}
	} else {
		// Path 2: Priority Retention (In-place update)
		if newSize.LessThan(oldTotalSize) {
			delta := oldTotalSize.Sub(newSize)
			// Prioritize deducting from HiddenSize
			if delta.LessThanOrEqual(order.HiddenSize) {
				order.HiddenSize = order.HiddenSize.Sub(delta)
			} else {
				remainingDelta := delta.Sub(order.HiddenSize)
				order.HiddenSize = udecimal.Zero
				newVisibleSize := order.Size.Sub(remainingDelta)
				// Update the queue with the new visible size (this also updates order.Size)
				myQueue.updateOrderSize(order.ID, newVisibleSize)
			}
		}

		log := NewAmendLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Price, newSize, oldPrice, oldTotalSize, order.Type, timestamp)
		amendLogsPtr := acquireLogSlice()
		*amendLogsPtr = append(*amendLogsPtr, log)
		book.publishTrader.Publish(*amendLogsPtr)
		releaseBookLog(log)
		releaseLogSlice(amendLogsPtr)
	}
}

// cancelOrder processes an order cancellation.
func (book *OrderBook) cancelOrder(orderID string, userID uint64, timestamp int64) {
	order, ok := book.findOrder(orderID)
	if !ok || order.UserID != userID {
		logsPtr := acquireLogSlice()
		log := NewRejectLog(book.seqID.Add(1), book.marketID, orderID, userID, protocol.RejectReasonOrderNotFound, timestamp)
		*logsPtr = append(*logsPtr, log)
		book.publishTrader.Publish(*logsPtr)
		releaseBookLog(log)
		releaseLogSlice(logsPtr)
		return
	}

	myQueue := book.bidQueue
	if order.Side == Sell {
		myQueue = book.askQueue
	}

	myQueue.removeOrder(order.Price, order.ID)
	logsPtr := acquireLogSlice()
	totalSize := order.Size.Add(order.HiddenSize)
	log := NewCancelLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Price, totalSize, order.Type, timestamp)
	*logsPtr = append(*logsPtr, log)
	book.publishTrader.Publish(*logsPtr)
	releaseBookLog(log)
	releaseLogSlice(logsPtr)
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
		book.bidQueue.insertOrder(newOrder, false)
	}
	for _, o := range snap.Asks {
		newOrder := acquireOrder()
		*newOrder = *o
		book.askQueue.insertOrder(newOrder, false)
	}
}

// handleLimitOrder handles Limit orders.
func (book *OrderBook) handleLimitOrder(order *Order, timestamp int64) *[]*OrderBookLog {
	var myQueue, targetQueue *queue
	if order.Side == Buy {
		myQueue = book.bidQueue
		targetQueue = book.askQueue
	} else {
		myQueue = book.askQueue
		targetQueue = book.bidQueue
	}

	logsPtr := acquireLogSlice()

	for {
		tOrd := targetQueue.peekHeadOrder()
		if tOrd == nil {
			book.prepareIcebergForResting(order)
			myQueue.insertOrder(order, false)
			log := NewOpenLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Price, order.Size, order.Type, timestamp)
			*logsPtr = append(*logsPtr, log)
			return logsPtr
		}

		if (order.Side == Buy && order.Price.LessThan(tOrd.Price)) ||
			(order.Side == Sell && order.Price.GreaterThan(tOrd.Price)) {
			book.prepareIcebergForResting(order)
			myQueue.insertOrder(order, false)
			log := NewOpenLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Price, order.Size, order.Type, timestamp)
			*logsPtr = append(*logsPtr, log)
			return logsPtr
		}

		tOrd = targetQueue.popHeadOrder()
		if order.Size.GreaterThanOrEqual(tOrd.Size) {
			log := NewMatchLog(book.seqID.Add(1), book.tradeID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Type, tOrd.ID, tOrd.UserID, tOrd.Price, tOrd.Size, timestamp)
			*logsPtr = append(*logsPtr, log)
			order.Size = order.Size.Sub(tOrd.Size)

			if !book.checkReplenish(tOrd, targetQueue, logsPtr, timestamp) {
				releaseOrder(tOrd)
			}

			if order.Size.Equal(udecimal.Zero) {
				releaseOrder(order)
				break
			}
		} else {
			log := NewMatchLog(book.seqID.Add(1), book.tradeID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Type, tOrd.ID, tOrd.UserID, tOrd.Price, order.Size, timestamp)
			*logsPtr = append(*logsPtr, log)
			tOrd.Size = tOrd.Size.Sub(order.Size)
			targetQueue.insertOrder(tOrd, true)
			releaseOrder(order)
			break
		}
	}
	return logsPtr
}

// prepareIcebergForResting splits an Iceberg order's size into visible and hidden parts
// when the order is about to rest in the order book.
func (book *OrderBook) prepareIcebergForResting(order *Order) {
	if order.VisibleLimit.GreaterThan(udecimal.Zero) && order.HiddenSize.IsZero() && order.Size.GreaterThan(order.VisibleLimit) {
		order.HiddenSize = order.Size.Sub(order.VisibleLimit)
		order.Size = order.VisibleLimit
	}
}

// handleIOCOrder handles Immediate Or Cancel orders.
func (book *OrderBook) handleIOCOrder(order *Order, timestamp int64) *[]*OrderBookLog {
	var targetQueue *queue
	if order.Side == Buy {
		targetQueue = book.askQueue
	} else {
		targetQueue = book.bidQueue
	}

	logsPtr := acquireLogSlice()
	for {
		tOrd := targetQueue.peekHeadOrder()
		if tOrd == nil {
			log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, protocol.RejectReasonNoLiquidity, timestamp)
			log.Side, log.Price, log.Size, log.OrderType = order.Side, order.Price, order.Size, order.Type
			*logsPtr = append(*logsPtr, log)
			releaseOrder(order)
			return logsPtr
		}

		if (order.Side == Buy && order.Price.LessThan(tOrd.Price)) || (order.Side == Sell && order.Price.GreaterThan(tOrd.Price)) {
			log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, protocol.RejectReasonPriceMismatch, timestamp)
			log.Side, log.Price, log.Size, log.OrderType = order.Side, order.Price, order.Size, order.Type
			*logsPtr = append(*logsPtr, log)
			releaseOrder(order)
			return logsPtr
		}

		tOrd = targetQueue.popHeadOrder()
		if order.Size.GreaterThanOrEqual(tOrd.Size) {
			log := NewMatchLog(book.seqID.Add(1), book.tradeID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Type, tOrd.ID, tOrd.UserID, tOrd.Price, tOrd.Size, timestamp)
			*logsPtr = append(*logsPtr, log)
			order.Size = order.Size.Sub(tOrd.Size)

			if !book.checkReplenish(tOrd, targetQueue, logsPtr, timestamp) {
				releaseOrder(tOrd)
			}

			if order.Size.Equal(udecimal.Zero) {
				releaseOrder(order)
				break
			}
		} else {
			log := NewMatchLog(book.seqID.Add(1), book.tradeID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Type, tOrd.ID, tOrd.UserID, tOrd.Price, order.Size, timestamp)
			*logsPtr = append(*logsPtr, log)
			tOrd.Size = tOrd.Size.Sub(order.Size)
			targetQueue.insertOrder(tOrd, true)
			releaseOrder(order)
			break
		}
	}
	return logsPtr
}

// handleFOKOrder handles Fill Or Kill orders.
func (book *OrderBook) handleFOKOrder(order *Order, timestamp int64) *[]*OrderBookLog {
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
		if (order.Side == Buy && order.Price.LessThan(price)) || (order.Side == Sell && order.Price.GreaterThan(price)) {
			break
		}
		hasLiquidityAtPrice = true
		if remainingSize.GreaterThan(unit.totalSize) {
			remainingSize = remainingSize.Sub(unit.totalSize)
		} else {
			canFill = true
			break
		}
		it.Next()
	}

	if !canFill {
		logsPtr := acquireLogSlice()
		reason := protocol.RejectReasonInsufficientSize
		if !hasLiquidityAtPrice {
			// If we didn't find ANY order matching the price, it might be PriceMismatch or NoLiquidity
			if targetQueue.peekHeadOrder() == nil {
				reason = protocol.RejectReasonNoLiquidity
			} else {
				reason = protocol.RejectReasonPriceMismatch
			}
		}
		log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, reason, timestamp)
		log.Side, log.Price, log.Size, log.OrderType = order.Side, order.Price, order.Size, order.Type
		*logsPtr = append(*logsPtr, log)
		releaseOrder(order)
		return logsPtr
	}

	// Phase 2: Execute match (same as IOC/Limit logic but guaranteed to finish)
	return book.handleIOCOrder(order, timestamp)
}

// handlePostOnlyOrder handles Post-Only orders.
func (book *OrderBook) handlePostOnlyOrder(order *Order, timestamp int64) *[]*OrderBookLog {
	var targetQueue *queue
	if order.Side == Buy {
		targetQueue = book.askQueue
	} else {
		targetQueue = book.bidQueue
	}

	tOrd := targetQueue.peekHeadOrder()
	if tOrd != nil {
		if (order.Side == Buy && order.Price.GreaterThanOrEqual(tOrd.Price)) || (order.Side == Sell && order.Price.LessThanOrEqual(tOrd.Price)) {
			logsPtr := acquireLogSlice()
			log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, protocol.RejectReasonPostOnlyMatch, timestamp)
			log.Side, log.Price, log.Size, log.OrderType = order.Side, order.Price, order.Size, order.Type
			*logsPtr = append(*logsPtr, log)
			releaseOrder(order)
			return logsPtr
		}
	}

	return book.handleLimitOrder(order, timestamp)
}

// handleMarketOrder handles Market orders.
func (book *OrderBook) handleMarketOrder(order *Order, quoteSize udecimal.Decimal, timestamp int64) *[]*OrderBookLog {
	var targetQueue *queue
	if order.Side == Buy {
		targetQueue = book.askQueue
	} else {
		targetQueue = book.bidQueue
	}

	logsPtr := acquireLogSlice()
	for {
		tOrd := targetQueue.peekHeadOrder()
		if tOrd == nil {
			log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, protocol.RejectReasonNoLiquidity, timestamp)
			log.Side, log.Price, log.OrderType = order.Side, order.Price, order.Type
			if order.Type == Market && !quoteSize.IsZero() {
				log.Size = quoteSize
			} else {
				log.Size = order.Size
			}
			*logsPtr = append(*logsPtr, log)
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
			log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, protocol.RejectReasonNoLiquidity, timestamp)
			log.Side, log.Price, log.OrderType = order.Side, order.Price, order.Type
			if useQuote {
				log.Size = quoteSize // Remaining quote size that couldn't be matched
			} else {
				log.Size = order.Size
			}
			*logsPtr = append(*logsPtr, log)
			releaseOrder(order)
			break
		}

		log := NewMatchLog(book.seqID.Add(1), book.tradeID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Type, tOrd.ID, tOrd.UserID, tOrd.Price, matchSize, timestamp)
		*logsPtr = append(*logsPtr, log)

		if useQuote {
			quoteSize = quoteSize.Sub(matchSize.Mul(tOrd.Price))
		} else {
			order.Size = order.Size.Sub(matchSize)
		}

		tOrd = targetQueue.popHeadOrder()
		if matchSize.Equal(tOrd.Size) {
			if !book.checkReplenish(tOrd, targetQueue, logsPtr, timestamp) {
				releaseOrder(tOrd)
			}
		} else {
			tOrd.Size = tOrd.Size.Sub(matchSize)
			targetQueue.insertOrder(tOrd, true)
		}

		// Termination condition
		if (useQuote && quoteSize.IsZero()) || (!useQuote && order.Size.IsZero()) {
			releaseOrder(order)
			break
		}
	}
	return logsPtr
}

func (book *OrderBook) checkReplenish(order *Order, q *queue, logsPtr *[]*OrderBookLog, timestamp int64) bool {
	if order.HiddenSize.GreaterThan(udecimal.Zero) {
		reloadQty := order.VisibleLimit
		if order.HiddenSize.LessThan(reloadQty) {
			reloadQty = order.HiddenSize
		}
		order.Size = reloadQty
		order.HiddenSize = order.HiddenSize.Sub(reloadQty)
		order.Timestamp = timestamp
		q.insertOrder(order, false) // Insert at end (Priority Loss)

		log := NewOpenLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Price, order.Size, order.Type, timestamp)
		*logsPtr = append(*logsPtr, log)
		return true
	}
	return false
}

// handleSuspendMarket updates the order book state to Suspended.
func (book *OrderBook) handleSuspendMarket(cmd *protocol.SuspendMarketCommand) {
	// If already Halted, cannot Suspend (Halted is terminal state for now)
	if book.state == protocol.OrderBookStateHalted {
		book.rejectInvalidPayload("unknown", 0, protocol.RejectReasonMarketHalted, nil)
		return
	}
	book.state = protocol.OrderBookStateSuspended
	// Note: We don't generate a specific log for state change yet,
	// relying on the upstream Command Log for audit.
}

// handleResumeMarket updates the order book state to Running.
func (book *OrderBook) handleResumeMarket(cmd *protocol.ResumeMarketCommand) {
	if book.state == protocol.OrderBookStateHalted {
		book.rejectInvalidPayload("unknown", 0, protocol.RejectReasonMarketHalted, nil)
		return
	}
	book.state = protocol.OrderBookStateRunning
}

// handleUpdateConfig updates order book configuration.
func (book *OrderBook) handleUpdateConfig(cmd *protocol.UpdateConfigCommand) {
	if cmd.MinLotSize != "" {
		size, err := udecimal.Parse(cmd.MinLotSize)
		if err != nil {
			book.rejectInvalidPayload("unknown", 0, protocol.RejectReasonInvalidPayload, nil)
			return
		}
		book.lotSize = size
	}
}
