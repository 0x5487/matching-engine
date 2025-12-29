package match

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quagmt/udecimal"
)

// orderPool is used to reduce Order allocations in the hot path.
var orderPool = sync.Pool{
	New: func() any {
		return &Order{}
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

// OrderBook type
type OrderBook struct {
	marketID         string
	lotSize          udecimal.Decimal // Minimum trade unit for Market orders
	seqID            atomic.Uint64    // Globally increasing sequence ID
	lastCmdSeqID     atomic.Uint64    // Last sequence ID of the command
	tradeID          atomic.Uint64    // Sequential trade ID counter
	isShutdown       atomic.Bool
	bidQueue         *queue
	askQueue         *queue
	cmdBuffer        *RingBuffer[Command]
	done             chan struct{}
	shutdownComplete chan struct{}
	publishTrader    PublishLog
}

// NewOrderBook creates a new order book instance.
func NewOrderBook(marketID string, publishTrader PublishLog, opts ...OrderBookOption) *OrderBook {
	book := &OrderBook{
		marketID:         marketID,
		lotSize:          DefaultLotSize,
		bidQueue:         NewBuyerQueue(),
		askQueue:         NewSellerQueue(),
		done:             make(chan struct{}),
		shutdownComplete: make(chan struct{}),
		publishTrader:    publishTrader,
	}

	for _, opt := range opts {
		opt(book)
	}

	book.cmdBuffer = NewRingBuffer(32768, book)
	return book
}

// AddOrder submits an order to the order book asynchronously.
func (book *OrderBook) AddOrder(ctx context.Context, cmd *PlaceOrderCommand) error {
	if book.isShutdown.Load() {
		return ErrShutdown
	}

	if len(cmd.Type) == 0 || len(cmd.ID) == 0 {
		return ErrInvalidParam
	}

	seq, event := book.cmdBuffer.Claim()
	if seq == -1 {
		return ErrShutdown
	}

	event.SeqID = cmd.SeqID
	event.Type = CmdPlaceOrder
	event.MarketID = cmd.MarketID
	event.OrderID = cmd.ID
	event.Side = cmd.Side
	event.OrderType = cmd.Type
	event.Price = cmd.Price
	event.Size = cmd.Size
	event.VisibleSize = cmd.VisibleSize
	event.QuoteSize = cmd.QuoteSize
	event.UserID = cmd.UserID
	event.Timestamp = cmd.Timestamp

	book.cmdBuffer.Commit(seq)
	return nil
}

// AmendOrder submits a request to modify an existing order asynchronously.
func (book *OrderBook) AmendOrder(ctx context.Context, cmd *AmendOrderCommand) error {
	if len(cmd.OrderID) == 0 || cmd.NewSize.LessThanOrEqual(udecimal.Zero) || cmd.NewPrice.LessThanOrEqual(udecimal.Zero) {
		return ErrInvalidParam
	}

	seq, event := book.cmdBuffer.Claim()
	if seq == -1 {
		return ErrShutdown
	}

	event.SeqID = cmd.SeqID
	event.Type = CmdAmendOrder
	event.OrderID = cmd.OrderID
	event.UserID = cmd.UserID
	event.NewPrice = cmd.NewPrice
	event.NewSize = cmd.NewSize
	event.Timestamp = cmd.Timestamp

	book.cmdBuffer.Commit(seq)
	return nil
}

// CancelOrder submits a cancellation request for an order asynchronously.
func (book *OrderBook) CancelOrder(ctx context.Context, cmd *CancelOrderCommand) error {
	if len(cmd.OrderID) == 0 {
		return nil
	}

	seq, event := book.cmdBuffer.Claim()
	if seq == -1 {
		return ErrShutdown
	}

	event.SeqID = cmd.SeqID
	event.Type = CmdCancelOrder
	event.OrderID = cmd.OrderID
	event.UserID = cmd.UserID
	event.Timestamp = cmd.Timestamp

	book.cmdBuffer.Commit(seq)
	return nil
}

// Depth returns the current depth of the order book up to the specified limit.
func (book *OrderBook) Depth(limit uint32) (*Depth, error) {
	if limit == 0 {
		return nil, ErrInvalidParam
	}

	respChan := make(chan any, 1)
	book.cmdBuffer.Publish(Command{
		Type:       CmdDepth,
		DepthLimit: limit,
		Resp:       respChan,
	})

	select {
	case res := <-respChan:
		if result, ok := res.(*Depth); ok {
			return result, nil
		}
		return nil, errors.New("unexpected response type")
	case <-time.After(time.Second):
		return nil, ErrTimeout
	}
}

// GetStats returns usage statistics for the order book.
func (book *OrderBook) GetStats() (*BookStats, error) {
	respChan := make(chan any, 1)
	book.cmdBuffer.Publish(Command{
		Type: CmdGetStats,
		Resp: respChan,
	})

	select {
	case res := <-respChan:
		if result, ok := res.(*BookStats); ok {
			return result, nil
		}
		return nil, errors.New("unexpected response type")
	case <-time.After(time.Second):
		return nil, ErrTimeout
	}
}

// LastCmdSeqID returns the sequence ID of the last processed command.
func (book *OrderBook) LastCmdSeqID() uint64 {
	return book.lastCmdSeqID.Load()
}

// Start starts the order book loop.
func (book *OrderBook) Start() error {
	book.cmdBuffer.Start()
	<-book.done
	return nil
}

// OnEvent processes commands from the RingBuffer.
func (book *OrderBook) OnEvent(cmd *Command) {
	switch cmd.Type {
	case CmdPlaceOrder:
		book.addOrder(cmd)
	case CmdAmendOrder:
		book.amendOrder(cmd)
	case CmdCancelOrder:
		book.cancelOrder(cmd)
	case CmdDepth:
		result := book.depth(cmd.DepthLimit)
		if cmd.Resp != nil {
			select {
			case cmd.Resp <- result:
			default:
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

	if cmd.SeqID > 0 {
		book.lastCmdSeqID.Store(cmd.SeqID)
	}
}

// Shutdown signals the order book to stop.
func (book *OrderBook) Shutdown(ctx context.Context) error {
	if book.isShutdown.CompareAndSwap(false, true) {
		close(book.done)
	}
	return book.cmdBuffer.Shutdown(ctx)
}

// addOrder processes the addition of an order.
func (book *OrderBook) addOrder(cmd *Command) {
	if book.bidQueue.order(cmd.OrderID) != nil || book.askQueue.order(cmd.OrderID) != nil {
		logsPtr := acquireLogSlice()
		log := NewRejectLog(book.seqID.Add(1), book.marketID, cmd.OrderID, cmd.UserID, RejectReasonDuplicateID, cmd.Timestamp)
		*logsPtr = append(*logsPtr, log)
		book.publishTrader.Publish(*logsPtr)
		releaseBookLog(log)
		releaseLogSlice(logsPtr)
		return
	}

	order := acquireOrder()
	order.ID = cmd.OrderID
	order.Side = cmd.Side
	order.Price = cmd.Price
	order.Size = cmd.Size
	order.Type = cmd.OrderType
	order.UserID = cmd.UserID
	order.Timestamp = cmd.Timestamp

	// For Iceberg orders, store VisibleLimit but DON'T split Size yet.
	// The split happens when the order enters the book (Maker/Passive phase),
	// not during aggressive matching (Taker phase).
	// This follows spec 3.2.A: "Use full Size for aggressive matching."
	if cmd.VisibleSize.GreaterThan(udecimal.Zero) && cmd.VisibleSize.LessThan(cmd.Size) {
		order.VisibleLimit = cmd.VisibleSize
		// HiddenSize and Size adjustment will happen in handleLimitOrder when resting
	}

	var logsPtr *[]*OrderBookLog
	switch order.Type {
	case Limit:
		logsPtr = book.handleLimitOrder(order, cmd.Timestamp)
	case FOK:
		logsPtr = book.handleFOKOrder(order, cmd.Timestamp)
	case IOC:
		logsPtr = book.handleIOCOrder(order, cmd.Timestamp)
	case PostOnly:
		logsPtr = book.handlePostOnlyOrder(order, cmd.Timestamp)
	case Market:
		logsPtr = book.handleMarketOrder(order, cmd.QuoteSize, cmd.Timestamp)
	case Cancel:
		// Cancel type is not expected here, handled by cancelOrder
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
func (book *OrderBook) amendOrder(cmd *Command) {
	order, ok := book.findOrder(cmd.OrderID)
	if !ok || order.UserID != cmd.UserID {
		logsPtr := acquireLogSlice()
		log := NewRejectLog(book.seqID.Add(1), book.marketID, cmd.OrderID, cmd.UserID, RejectReasonOrderNotFound, cmd.Timestamp)
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
	newTotalSize := cmd.NewSize

	isPriceChange := !oldPrice.Equal(cmd.NewPrice)
	isSizeIncrease := newTotalSize.GreaterThan(oldTotalSize)

	if isPriceChange || isSizeIncrease {
		// Path 1: Priority Loss (Re-match)
		myQueue.removeOrder(oldPrice, order.ID)
		order.Price = cmd.NewPrice
		order.Timestamp = cmd.Timestamp

		// Recalculate Iceberg fields for the new total size
		if order.VisibleLimit.IsZero() {
			order.Size = newTotalSize
			order.HiddenSize = udecimal.Zero
		} else {
			if newTotalSize.GreaterThan(order.VisibleLimit) {
				order.Size = order.VisibleLimit
				order.HiddenSize = newTotalSize.Sub(order.VisibleLimit)
			} else {
				order.Size = newTotalSize
				order.HiddenSize = udecimal.Zero
			}
		}

		log := NewAmendLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Price, newTotalSize, oldPrice, oldTotalSize, order.Type, cmd.Timestamp)
		amendLogsPtr := acquireLogSlice()
		*amendLogsPtr = append(*amendLogsPtr, log)
		book.publishTrader.Publish(*amendLogsPtr)
		releaseBookLog(log)
		releaseLogSlice(amendLogsPtr)

		logsPtr := book.handleLimitOrder(order, cmd.Timestamp)
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
		if newTotalSize.LessThan(oldTotalSize) {
			delta := oldTotalSize.Sub(newTotalSize)
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

		log := NewAmendLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Price, newTotalSize, oldPrice, oldTotalSize, order.Type, cmd.Timestamp)
		amendLogsPtr := acquireLogSlice()
		*amendLogsPtr = append(*amendLogsPtr, log)
		book.publishTrader.Publish(*amendLogsPtr)
		releaseBookLog(log)
		releaseLogSlice(amendLogsPtr)
	}
}

// cancelOrder processes an order cancellation.
func (book *OrderBook) cancelOrder(cmd *Command) {
	order, ok := book.findOrder(cmd.OrderID)
	if !ok || order.UserID != cmd.UserID {
		logsPtr := acquireLogSlice()
		log := NewRejectLog(book.seqID.Add(1), book.marketID, cmd.OrderID, cmd.UserID, RejectReasonOrderNotFound, cmd.Timestamp)
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
	log := NewCancelLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Price, totalSize, order.Type, cmd.Timestamp)
	*logsPtr = append(*logsPtr, log)
	book.publishTrader.Publish(*logsPtr)
	releaseBookLog(log)
	releaseLogSlice(logsPtr)
	releaseOrder(order)
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
func (book *OrderBook) depth(limit uint32) *Depth {
	return &Depth{
		UpdateID: book.seqID.Load(),
		Asks:     book.askQueue.depth(limit),
		Bids:     book.bidQueue.depth(limit),
	}
}

// TakeSnapshot is used by the Engine to request a snapshot.
func (book *OrderBook) TakeSnapshot() (*OrderBookSnapshot, error) {
	respChan := make(chan any, 1)
	book.cmdBuffer.Publish(Command{
		Type: CmdSnapshot,
		Resp: respChan,
	})

	select {
	case res := <-respChan:
		if snap, ok := res.(*OrderBookSnapshot); ok {
			return snap, nil
		}
		return nil, errors.New("unexpected snapshot response")
	case <-time.After(5 * time.Second):
		return nil, ErrTimeout
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

// Restore restores the order book state from a snapshot.
func (book *OrderBook) Restore(snap *OrderBookSnapshot) {
	book.seqID.Store(snap.SeqID)
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
			log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, RejectReasonNoLiquidity, timestamp)
			log.Side, log.Price, log.Size, log.OrderType = order.Side, order.Price, order.Size, order.Type
			*logsPtr = append(*logsPtr, log)
			releaseOrder(order)
			return logsPtr
		}

		if (order.Side == Buy && order.Price.LessThan(tOrd.Price)) || (order.Side == Sell && order.Price.GreaterThan(tOrd.Price)) {
			log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, RejectReasonPriceMismatch, timestamp)
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
		reason := RejectReasonInsufficientSize
		if !hasLiquidityAtPrice {
			// If we didn't find ANY order matching the price, it might be PriceMismatch or NoLiquidity
			if targetQueue.peekHeadOrder() == nil {
				reason = RejectReasonNoLiquidity
			} else {
				reason = RejectReasonPriceMismatch
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
			log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, RejectReasonPostOnlyMatch, timestamp)
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
			log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, RejectReasonNoLiquidity, timestamp)
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
			log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, RejectReasonNoLiquidity, timestamp)
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
