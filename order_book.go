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

// Command payload pools to reduce allocations during Unmarshal.
var placeOrderCmdPool = sync.Pool{
	New: func() any {
		return &protocol.PlaceOrderParams{}
	},
}

var cancelOrderCmdPool = sync.Pool{
	New: func() any {
		return &protocol.CancelOrderParams{}
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

// OrderBook is a pure logic object that maintains the state of an order book.
// It must be managed by a MatchingEngine which provides the event loop.
type OrderBook struct {
	engineID     string
	marketID     string
	lotSize      udecimal.Decimal // Minimum trade unit for Market orders
	seqID        atomic.Uint64    // Globally increasing sequence ID
	lastCmdSeqID atomic.Uint64    // Last sequence ID of the command
	tradeID      atomic.Uint64    // Sequential trade ID counter
	bidQueue     *queue
	askQueue     *queue
	publisher    Publisher
	state        protocol.OrderBookState
}

// newOrderBook creates a new OrderBook instance.
// OrderBooks are managed by a MatchingEngine and do not have their own event loop.
func newOrderBook(
	engineID string,
	marketID string,
	publishTrader Publisher,
	opts ...OrderBookOption,
) *OrderBook {
	book := &OrderBook{
		engineID:  engineID,
		marketID:  marketID,
		lotSize:   DefaultLotSize,
		bidQueue:  newBuyerQueue(),
		askQueue:  newSellerQueue(),
		publisher: publishTrader,
		state:     protocol.OrderBookStateRunning,
	}

	for _, opt := range opts {
		opt(book)
	}

	return book
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

func (book *OrderBook) processCommand(ev *InputEvent) {
	cmd := ev.Cmd
	switch cmd.Type {
	case protocol.CmdSuspendMarket:
		payload := &protocol.SuspendMarketParams{}
		if err := payload.UnmarshalBinary(cmd.Payload); err != nil {
			book.rejectInvalidPayload(
				cmd.CommandID,
				book.marketID,
				"",
				cmd.UserID,
				protocol.RejectReasonInvalidPayload,
				cmd.Timestamp,
			)
			book.sendResponse(ev.Resp, err)
			return
		}
		book.handleSuspendMarket(cmd.CommandID, cmd.Timestamp, cmd.UserID, payload, ev.Resp)

	case protocol.CmdResumeMarket:
		payload := &protocol.ResumeMarketParams{}
		if err := payload.UnmarshalBinary(cmd.Payload); err != nil {
			book.rejectInvalidPayload(
				cmd.CommandID,
				book.marketID,
				"",
				cmd.UserID,
				protocol.RejectReasonInvalidPayload,
				cmd.Timestamp,
			)
			book.sendResponse(ev.Resp, err)
			return
		}
		book.handleResumeMarket(cmd.CommandID, cmd.Timestamp, cmd.UserID, payload, ev.Resp)

	case protocol.CmdUpdateConfig:
		payload := &protocol.UpdateConfigParams{}
		if err := payload.UnmarshalBinary(cmd.Payload); err != nil {
			book.rejectInvalidPayload(
				cmd.CommandID,
				book.marketID,
				"",
				cmd.UserID,
				protocol.RejectReasonInvalidPayload,
				cmd.Timestamp,
			)
			book.sendResponse(ev.Resp, err)
			return
		}
		book.handleUpdateConfig(cmd.CommandID, cmd.Timestamp, cmd.UserID, payload, ev.Resp)

	case protocol.CmdPlaceOrder:
		val := placeOrderCmdPool.Get()
		payload, ok := val.(*protocol.PlaceOrderParams)
		if !ok {
			book.rejectInvalidPayload(
				cmd.CommandID,
				book.marketID,
				"unknown",
				0,
				protocol.RejectReasonInvalidPayload,
				cmd.Timestamp,
			)
			book.sendResponse(ev.Resp, errors.New("failed to acquire place order params from pool"))
			return
		}
		*payload = protocol.PlaceOrderParams{} // Reset before use
		if err := payload.UnmarshalBinary(cmd.Payload); err != nil {
			placeOrderCmdPool.Put(payload)
			book.rejectInvalidPayload(
				cmd.CommandID,
				book.marketID,
				"unknown",
				cmd.UserID,
				protocol.RejectReasonInvalidPayload,
				cmd.Timestamp,
			)
			book.sendResponse(ev.Resp, err)
			return
		}
		book.handlePlaceOrder(cmd.CommandID, cmd.Timestamp, cmd.UserID, payload, ev.Resp)
		placeOrderCmdPool.Put(payload)
	case protocol.CmdCancelOrder:
		val := cancelOrderCmdPool.Get()
		payload, ok := val.(*protocol.CancelOrderParams)
		if !ok {
			book.rejectInvalidPayload(
				cmd.CommandID,
				book.marketID,
				"unknown",
				0,
				protocol.RejectReasonInvalidPayload,
				cmd.Timestamp,
			)
			book.sendResponse(
				ev.Resp,
				errors.New("failed to acquire cancel order params from pool"),
			)
			return
		}
		*payload = protocol.CancelOrderParams{} // Reset before use
		if err := payload.UnmarshalBinary(cmd.Payload); err != nil {
			cancelOrderCmdPool.Put(payload)
			book.rejectInvalidPayload(
				cmd.CommandID,
				book.marketID,
				"unknown",
				cmd.UserID,
				protocol.RejectReasonInvalidPayload,
				cmd.Timestamp,
			)
			book.sendResponse(ev.Resp, err)
			return
		}
		book.handleCancelOrder(cmd.CommandID, cmd.Timestamp, cmd.UserID, payload, ev.Resp)
		cancelOrderCmdPool.Put(payload)
	case protocol.CmdAmendOrder:
		var payload protocol.AmendOrderParams
		if err := payload.UnmarshalBinary(cmd.Payload); err != nil {
			book.rejectInvalidPayload(
				cmd.CommandID,
				book.marketID,
				"unknown",
				cmd.UserID,
				protocol.RejectReasonInvalidPayload,
				cmd.Timestamp,
			)
			book.sendResponse(ev.Resp, err)
			return
		}
		book.handleAmendOrder(cmd.CommandID, cmd.Timestamp, cmd.UserID, &payload, ev.Resp)
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
		book.sendResponse(ev.Resp, result)
	case *protocol.GetStatsRequest:
		stats := &protocol.GetStatsResponse{
			AskDepthCount: book.askQueue.depthCount(),
			AskOrderCount: book.askQueue.orderCount(),
			BidDepthCount: book.bidQueue.depthCount(),
			BidOrderCount: book.bidQueue.orderCount(),
		}
		book.sendResponse(ev.Resp, stats)
	case *OrderBookSnapshot: // Snapshot query
		snap := book.createSnapshot()
		book.sendResponse(ev.Resp, snap)
	default:
		// Unsupported query type
		book.sendResponse(ev.Resp, ErrInvalidParam)
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

func (book *OrderBook) handlePlaceOrder(
	commandID string,
	timestamp int64,
	userID uint64,
	cmd *protocol.PlaceOrderParams,
	resp chan<- any,
) {
	if timestamp <= 0 {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			cmd.OrderID,
			userID,
			protocol.RejectReasonInvalidPayload,
			timestamp,
		)
		book.sendResponse(resp, errors.New(string(protocol.RejectReasonInvalidPayload)))
		return
	}

	if book.state == protocol.OrderBookStateHalted {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			cmd.OrderID,
			userID,
			protocol.RejectReasonMarketHalted,
			timestamp,
		)
		book.sendResponse(resp, errors.New(string(protocol.RejectReasonMarketHalted)))
		return
	}
	if book.state == protocol.OrderBookStateSuspended {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			cmd.OrderID,
			userID,
			protocol.RejectReasonMarketSuspended,
			timestamp,
		)
		book.sendResponse(resp, errors.New(string(protocol.RejectReasonMarketSuspended)))
		return
	}

	// Parse strings to decimals
	price, err := udecimal.Parse(cmd.Price)
	if err != nil {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			cmd.OrderID,
			userID,
			protocol.RejectReasonInvalidPayload,
			timestamp,
		)
		book.sendResponse(resp, err)
		return
	}
	size, err := udecimal.Parse(cmd.Size)
	if err != nil {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			cmd.OrderID,
			userID,
			protocol.RejectReasonInvalidPayload,
			timestamp,
		)
		book.sendResponse(resp, err)
		return
	}

	visibleSize := udecimal.Zero
	if cmd.VisibleSize != "" {
		visibleSize, err = udecimal.Parse(cmd.VisibleSize)
		if err != nil {
			book.rejectInvalidPayload(
				commandID,
				book.marketID,
				cmd.OrderID,
				userID,
				protocol.RejectReasonInvalidPayload,
				timestamp,
			)
			book.sendResponse(resp, err)
			return
		}
	}
	quoteSize := udecimal.Zero
	if cmd.QuoteSize != "" {
		quoteSize, err = udecimal.Parse(cmd.QuoteSize)
		if err != nil {
			book.rejectInvalidPayload(
				commandID,
				book.marketID,
				cmd.OrderID,
				userID,
				protocol.RejectReasonInvalidPayload,
				timestamp,
			)
			book.sendResponse(resp, err)
			return
		}
	}

	result, err := book.placeOrder(&placeOrderParams{
		commandID:   commandID,
		orderID:     cmd.OrderID,
		side:        cmd.Side,
		price:       price,
		size:        size,
		visibleSize: visibleSize,
		quoteSize:   quoteSize,
		orderType:   cmd.OrderType,
		userID:      userID,
		timestamp:   timestamp,
	})
	if err != nil {
		book.sendResponse(resp, err)
	} else {
		book.sendResponse(resp, result)
	}
}

func (book *OrderBook) handleCancelOrder(
	commandID string,
	timestamp int64,
	userID uint64,
	cmd *protocol.CancelOrderParams,
	resp chan<- any,
) {
	if timestamp <= 0 {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			cmd.OrderID,
			userID,
			protocol.RejectReasonInvalidPayload,
			timestamp,
		)
		book.sendResponse(resp, errors.New(string(protocol.RejectReasonInvalidPayload)))
		return
	}

	// Cancel is allowed in Suspended state, but not Halted
	if book.state == protocol.OrderBookStateHalted {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			cmd.OrderID,
			userID,
			protocol.RejectReasonMarketHalted,
			timestamp,
		)
		book.sendResponse(resp, errors.New(string(protocol.RejectReasonMarketHalted)))
		return
	}

	err := book.cancelOrder(commandID, cmd.OrderID, userID, timestamp)
	if err != nil {
		book.sendResponse(resp, err)
	} else {
		book.sendResponse(resp, true)
	}
}

func (book *OrderBook) handleAmendOrder(
	commandID string,
	timestamp int64,
	userID uint64,
	cmd *protocol.AmendOrderParams,
	resp chan<- any,
) {
	if timestamp <= 0 {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			cmd.OrderID,
			userID,
			protocol.RejectReasonInvalidPayload,
			timestamp,
		)
		book.sendResponse(resp, errors.New(string(protocol.RejectReasonInvalidPayload)))
		return
	}

	if book.state == protocol.OrderBookStateHalted {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			cmd.OrderID,
			userID,
			protocol.RejectReasonMarketHalted,
			timestamp,
		)
		book.sendResponse(resp, errors.New(string(protocol.RejectReasonMarketHalted)))
		return
	}
	if book.state == protocol.OrderBookStateSuspended {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			cmd.OrderID,
			userID,
			protocol.RejectReasonMarketSuspended,
			timestamp,
		)
		book.sendResponse(resp, errors.New(string(protocol.RejectReasonMarketSuspended)))
		return
	}

	newPrice, err := udecimal.Parse(cmd.NewPrice)
	if err != nil {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			cmd.OrderID,
			userID,
			protocol.RejectReasonInvalidPayload,
			timestamp,
		)
		book.sendResponse(resp, err)
		return
	}
	newSize, err := udecimal.Parse(cmd.NewSize)
	if err != nil {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			cmd.OrderID,
			userID,
			protocol.RejectReasonInvalidPayload,
			timestamp,
		)
		book.sendResponse(resp, err)
		return
	}
	err = book.amendOrder(commandID, cmd.OrderID, userID, newPrice, newSize, timestamp)
	if err != nil {
		book.sendResponse(resp, err)
	} else {
		book.sendResponse(resp, true)
	}
}

type placeOrderParams struct {
	commandID   string
	orderID     string
	side        Side
	price       udecimal.Decimal
	size        udecimal.Decimal
	visibleSize udecimal.Decimal
	quoteSize   udecimal.Decimal
	orderType   OrderType
	userID      uint64
	timestamp   int64
}

// placeOrder processes the addition of an order.
func (book *OrderBook) placeOrder(params *placeOrderParams) (*Order, error) {
	if book.bidQueue.order(params.orderID) != nil || book.askQueue.order(params.orderID) != nil {
		batch := acquireLogBatch()
		log := NewRejectLog(
			book.seqID.Add(1),
			params.commandID,
			book.engineID,
			book.marketID,
			params.orderID,
			params.userID,
			protocol.RejectReasonDuplicateID,
			params.timestamp,
		)
		batch.Logs = append(batch.Logs, log)
		book.publisher.Publish(batch.Logs)
		releaseBookLog(log)
		batch.Release()
		return nil, errors.New(string(protocol.RejectReasonDuplicateID))
	}

	order := acquireOrder()
	order.ID = params.orderID
	order.Side = params.side
	order.Price = params.price
	order.Size = params.size
	order.Type = params.orderType
	order.UserID = params.userID
	order.Timestamp = params.timestamp

	// For Iceberg orders, store VisibleLimit but DON'T split Size yet.
	// The split happens when the order enters the book (Maker/Passive phase),
	// not during aggressive matching (Taker phase).
	// This follows spec 3.2.A: "Use full Size for aggressive matching."
	if params.visibleSize.GreaterThan(udecimal.Zero) && params.visibleSize.LessThan(params.size) {
		order.VisibleLimit = params.visibleSize
		// HiddenSize and Size adjustment will happen in handleLimitOrder when resting
	}

	// Capture a copy of the order before it might be released by handlers
	orderCopy := *order

	var batch *LogBatch
	switch order.Type {
	case Limit:
		batch = book.handleLimitOrder(params.commandID, order, params.timestamp)
	case FOK:
		batch = book.handleFOKOrder(params.commandID, order, params.timestamp)
	case IOC:
		batch = book.handleIOCOrder(params.commandID, order, params.timestamp)
	case PostOnly:
		batch = book.handlePostOnlyOrder(params.commandID, order, params.timestamp)
	case Market:
		batch = book.handleMarketOrder(params.commandID, order, params.quoteSize, params.timestamp)
	default:
		// Ignore or handle unknown types
	}

	if batch != nil {
		if len(batch.Logs) > 0 {
			book.publisher.Publish(batch.Logs)
			for _, log := range batch.Logs {
				if log.Type == protocol.LogTypeReject {
					// If any log is a reject, we should return an error
					// This is a bit simplified, usually there's only one log in batch for single order placement
					// but handleLimitOrder might produce multiple matches.
					// Actually, for single order placement, if it's rejected, it's the only log.
					releaseBookLog(log)
					batch.Release()
					return nil, errors.New(string(log.RejectReason))
				}
				releaseBookLog(log)
			}
		}
		batch.Release()
	}

	return &orderCopy, nil
}

// amendOrder processes the modification of an order.
func (book *OrderBook) amendOrder(
	commandID string,
	orderID string,
	userID uint64,
	newPrice, newSize udecimal.Decimal,
	timestamp int64,
) error {
	order, ok := book.findOrder(orderID)
	if !ok || order.UserID != userID {
		batch := acquireLogBatch()
		log := NewRejectLog(
			book.seqID.Add(1),
			commandID,
			book.engineID,
			book.marketID,
			orderID,
			userID,
			protocol.RejectReasonOrderNotFound,
			timestamp,
		)
		batch.Logs = append(batch.Logs, log)
		book.publisher.Publish(batch.Logs)
		releaseBookLog(log)
		batch.Release()
		return errors.New(string(protocol.RejectReasonOrderNotFound))
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

		log := NewAmendLog(
			book.seqID.Add(1),
			commandID,
			book.engineID,
			book.marketID,
			order.ID,
			order.UserID,
			order.Side,
			order.Price,
			newSize,
			oldPrice,
			oldTotalSize,
			order.Type,
			timestamp,
		)
		amendBatch := acquireLogBatch()
		amendBatch.Logs = append(amendBatch.Logs, log)
		book.publisher.Publish(amendBatch.Logs)
		releaseBookLog(log)
		amendBatch.Release()

		batch := book.handleLimitOrder(commandID, order, timestamp)
		if batch != nil {
			if len(batch.Logs) > 0 {
				book.publisher.Publish(batch.Logs)
				for _, log := range batch.Logs {
					releaseBookLog(log)
				}
			}
			batch.Release()
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

		log := NewAmendLog(
			book.seqID.Add(1),
			commandID,
			book.engineID,
			book.marketID,
			order.ID,
			order.UserID,
			order.Side,
			order.Price,
			newSize,
			oldPrice,
			oldTotalSize,
			order.Type,
			timestamp,
		)
		amendBatch := acquireLogBatch()
		amendBatch.Logs = append(amendBatch.Logs, log)
		book.publisher.Publish(amendBatch.Logs)
		releaseBookLog(log)
		amendBatch.Release()
	}
	return nil
}

// cancelOrder processes an order cancellation.
func (book *OrderBook) cancelOrder(
	commandID string,
	orderID string,
	userID uint64,
	timestamp int64,
) error {
	order, ok := book.findOrder(orderID)
	if !ok || order.UserID != userID {
		batch := acquireLogBatch()
		log := NewRejectLog(
			book.seqID.Add(1),
			commandID,
			book.engineID,
			book.marketID,
			orderID,
			userID,
			protocol.RejectReasonOrderNotFound,
			timestamp,
		)
		batch.Logs = append(batch.Logs, log)
		book.publisher.Publish(batch.Logs)
		releaseBookLog(log)
		batch.Release()
		return errors.New(string(protocol.RejectReasonOrderNotFound))
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
		commandID,
		book.engineID,
		book.marketID,
		order.ID,
		order.UserID,
		order.Side,
		order.Price,
		totalSize,
		order.Type,
		timestamp,
	)
	batch.Logs = append(batch.Logs, log)
	book.publisher.Publish(batch.Logs)
	releaseBookLog(log)
	batch.Release()
	return nil
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

// handleSuspendMarket updates the order book state to Suspended.
func (book *OrderBook) handleSuspendMarket(
	commandID string,
	timestamp int64,
	userID uint64,
	payload *protocol.SuspendMarketParams,
	resp chan<- any,
) {
	if timestamp <= 0 {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			"",
			userID,
			protocol.RejectReasonInvalidPayload,
			timestamp,
		)
		book.sendResponse(resp, errors.New(string(protocol.RejectReasonInvalidPayload)))
		return
	}

	// If already Halted, cannot Suspend (Halted is terminal state for now)
	if book.state == protocol.OrderBookStateHalted {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			"",
			userID,
			protocol.RejectReasonMarketHalted,
			timestamp,
		)
		book.sendResponse(resp, errors.New(string(protocol.RejectReasonMarketHalted)))
		return
	}
	book.state = protocol.OrderBookStateSuspended
	batch := acquireLogBatch()
	log := NewAdminLog(
		book.seqID.Add(1),
		commandID,
		book.engineID,
		book.marketID,
		userID,
		payload.Reason,
		timestamp,
	)
	batch.Logs = append(batch.Logs, log)
	book.publisher.Publish(batch.Logs)
	releaseBookLog(log)
	batch.Release()

	book.sendResponse(resp, true)
}

// handleResumeMarket updates the order book state to Running.
func (book *OrderBook) handleResumeMarket(
	commandID string,
	timestamp int64,
	userID uint64,
	_ *protocol.ResumeMarketParams,
	resp chan<- any,
) {
	if timestamp <= 0 {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			"",
			userID,
			protocol.RejectReasonInvalidPayload,
			timestamp,
		)
		book.sendResponse(resp, errors.New(string(protocol.RejectReasonInvalidPayload)))
		return
	}

	if book.state == protocol.OrderBookStateHalted {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			"",
			userID,
			protocol.RejectReasonMarketHalted,
			timestamp,
		)
		book.sendResponse(resp, errors.New(string(protocol.RejectReasonMarketHalted)))
		return
	}
	book.state = protocol.OrderBookStateRunning
	batch := acquireLogBatch()
	log := NewAdminLog(
		book.seqID.Add(1),
		commandID,
		book.engineID,
		book.marketID,
		userID,
		"market_resumed",
		timestamp,
	)
	batch.Logs = append(batch.Logs, log)
	book.publisher.Publish(batch.Logs)
	releaseBookLog(log)
	batch.Release()

	book.sendResponse(resp, true)
}

// handleUpdateConfig updates order book configuration.
func (book *OrderBook) handleUpdateConfig(
	commandID string,
	timestamp int64,
	userID uint64,
	payload *protocol.UpdateConfigParams,
	resp chan<- any,
) {
	if timestamp <= 0 {
		book.rejectInvalidPayload(
			commandID,
			book.marketID,
			"",
			userID,
			protocol.RejectReasonInvalidPayload,
			timestamp,
		)
		book.sendResponse(resp, errors.New(string(protocol.RejectReasonInvalidPayload)))
		return
	}

	if payload.MinLotSize != "" {
		size, err := udecimal.Parse(payload.MinLotSize)
		if err == nil {
			book.lotSize = size
			batch := acquireLogBatch()
			log := NewAdminLog(
				book.seqID.Add(1),
				commandID,
				book.engineID,
				book.marketID,
				userID,
				"market_config_updated",
				timestamp,
			)
			batch.Logs = append(batch.Logs, log)
			book.publisher.Publish(batch.Logs)
			releaseBookLog(log)
			batch.Release()
			book.sendResponse(resp, true)
		} else {
			book.rejectInvalidPayload(
				commandID,
				book.marketID,
				"",
				userID,
				protocol.RejectReasonInvalidPayload,
				timestamp,
			)
			book.sendResponse(resp, err)
		}
	} else {
		// No config update requested, but still respond success if we reached here
		book.sendResponse(resp, true)
	}
}
