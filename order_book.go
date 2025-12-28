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

// OrderBook type
type OrderBook struct {
	marketID         string
	seqID            atomic.Uint64 // Globally increasing sequence ID
	lastCmdSeqID     atomic.Uint64 // Last sequence ID of the command
	tradeID          atomic.Uint64 // Sequential trade ID counter
	isShutdown       atomic.Bool
	bidQueue         *queue
	askQueue         *queue
	cmdBuffer        *RingBuffer[Command]
	done             chan struct{}
	shutdownComplete chan struct{}
	publishTrader    PublishLog
}

// NewOrderBook creates a new order book instance.
func NewOrderBook(marketID string, publishTrader PublishLog) *OrderBook {
	book := &OrderBook{
		marketID:         marketID,
		bidQueue:         NewBuyerQueue(),
		askQueue:         NewSellerQueue(),
		done:             make(chan struct{}),
		shutdownComplete: make(chan struct{}),
		publishTrader:    publishTrader,
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
	event.QuoteSize = cmd.QuoteSize
	event.UserID = cmd.UserID

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
		log := NewRejectLog(book.seqID.Add(1), book.marketID, cmd.OrderID, cmd.UserID, RejectReasonDuplicateID)
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
	order.Timestamp = time.Now().UnixNano()

	var logsPtr *[]*OrderBookLog
	switch order.Type {
	case Limit:
		logsPtr = book.handleLimitOrder(order)
	case FOK:
		logsPtr = book.handleFOKOrder(order)
	case IOC:
		logsPtr = book.handleIOCOrder(order)
	case PostOnly:
		logsPtr = book.handlePostOnlyOrder(order)
	case Market:
		logsPtr = book.handleMarketOrder(order, cmd.QuoteSize)
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
		log := NewRejectLog(book.seqID.Add(1), book.marketID, cmd.OrderID, cmd.UserID, RejectReasonOrderNotFound)
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
	oldSize := order.Size

	if !oldPrice.Equal(cmd.NewPrice) || cmd.NewSize.GreaterThan(oldSize) {
		myQueue.removeOrder(oldPrice, order.ID)
		order.Price = cmd.NewPrice
		order.Size = cmd.NewSize
		log := NewAmendLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Price, order.Size, oldPrice, oldSize, order.Type)
		amendLogsPtr := acquireLogSlice()
		*amendLogsPtr = append(*amendLogsPtr, log)
		book.publishTrader.Publish(*amendLogsPtr)
		releaseBookLog(log)
		releaseLogSlice(amendLogsPtr)

		logsPtr := book.handleLimitOrder(order)
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
		if cmd.NewSize.LessThan(oldSize) {
			myQueue.updateOrderSize(order.ID, cmd.NewSize)
		}
		log := NewAmendLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Price, order.Size, oldPrice, oldSize, order.Type)
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
		log := NewRejectLog(book.seqID.Add(1), book.marketID, cmd.OrderID, cmd.UserID, RejectReasonOrderNotFound)
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
	log := NewCancelLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Price, order.Size, order.Type)
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
func (book *OrderBook) handleLimitOrder(order *Order) *[]*OrderBookLog {
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
			myQueue.insertOrder(order, false)
			log := NewOpenLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Price, order.Size, order.Type)
			*logsPtr = append(*logsPtr, log)
			return logsPtr
		}

		if (order.Side == Buy && order.Price.LessThan(tOrd.Price)) ||
			(order.Side == Sell && order.Price.GreaterThan(tOrd.Price)) {
			myQueue.insertOrder(order, false)
			log := NewOpenLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Price, order.Size, order.Type)
			*logsPtr = append(*logsPtr, log)
			return logsPtr
		}

		tOrd = targetQueue.popHeadOrder()
		if order.Size.GreaterThanOrEqual(tOrd.Size) {
			log := NewMatchLog(book.seqID.Add(1), book.tradeID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Type, tOrd.ID, tOrd.UserID, tOrd.Price, tOrd.Size)
			*logsPtr = append(*logsPtr, log)
			order.Size = order.Size.Sub(tOrd.Size)
			releaseOrder(tOrd)
			if order.Size.Equal(udecimal.Zero) {
				releaseOrder(order)
				break
			}
		} else {
			log := NewMatchLog(book.seqID.Add(1), book.tradeID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Type, tOrd.ID, tOrd.UserID, tOrd.Price, order.Size)
			*logsPtr = append(*logsPtr, log)
			tOrd.Size = tOrd.Size.Sub(order.Size)
			targetQueue.insertOrder(tOrd, true)
			releaseOrder(order)
			break
		}
	}
	return logsPtr
}

// handleIOCOrder handles Immediate Or Cancel orders.
func (book *OrderBook) handleIOCOrder(order *Order) *[]*OrderBookLog {
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
			log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, RejectReasonNoLiquidity)
			log.Side, log.Price, log.Size, log.OrderType = order.Side, order.Price, order.Size, order.Type
			*logsPtr = append(*logsPtr, log)
			releaseOrder(order)
			return logsPtr
		}

		if (order.Side == Buy && order.Price.LessThan(tOrd.Price)) || (order.Side == Sell && order.Price.GreaterThan(tOrd.Price)) {
			log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, RejectReasonPriceMismatch)
			log.Side, log.Price, log.Size, log.OrderType = order.Side, order.Price, order.Size, order.Type
			*logsPtr = append(*logsPtr, log)
			releaseOrder(order)
			return logsPtr
		}

		tOrd = targetQueue.popHeadOrder()
		if order.Size.GreaterThanOrEqual(tOrd.Size) {
			log := NewMatchLog(book.seqID.Add(1), book.tradeID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Type, tOrd.ID, tOrd.UserID, tOrd.Price, tOrd.Size)
			*logsPtr = append(*logsPtr, log)
			order.Size = order.Size.Sub(tOrd.Size)
			releaseOrder(tOrd)
			if order.Size.Equal(udecimal.Zero) {
				releaseOrder(order)
				break
			}
		} else {
			log := NewMatchLog(book.seqID.Add(1), book.tradeID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Type, tOrd.ID, tOrd.UserID, tOrd.Price, order.Size)
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
func (book *OrderBook) handleFOKOrder(order *Order) *[]*OrderBookLog {
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
		log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, reason)
		log.Side, log.Price, log.Size, log.OrderType = order.Side, order.Price, order.Size, order.Type
		*logsPtr = append(*logsPtr, log)
		releaseOrder(order)
		return logsPtr
	}

	// Phase 2: Execute match (same as IOC/Limit logic but guaranteed to finish)
	return book.handleIOCOrder(order)
}

// handlePostOnlyOrder handles Post-Only orders.
func (book *OrderBook) handlePostOnlyOrder(order *Order) *[]*OrderBookLog {
	var targetQueue *queue
	if order.Side == Buy {
		targetQueue = book.askQueue
	} else {
		targetQueue = book.bidQueue
	}

	tOrd := targetQueue.peekHeadOrder()
	if tOrd != nil && ((order.Side == Buy && order.Price.GreaterThanOrEqual(tOrd.Price)) || (order.Side == Sell && order.Price.LessThanOrEqual(tOrd.Price))) {
		logsPtr := acquireLogSlice()
		log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, RejectReasonWouldCrossSpread)
		log.Side, log.Price, log.Size, log.OrderType = order.Side, order.Price, order.Size, order.Type
		*logsPtr = append(*logsPtr, log)
		releaseOrder(order)
		return logsPtr
	}

	logsPtr := acquireLogSlice()
	myQueue := book.bidQueue
	if order.Side == Sell {
		myQueue = book.askQueue
	}
	myQueue.insertOrder(order, false)
	log := NewOpenLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Price, order.Size, order.Type)
	*logsPtr = append(*logsPtr, log)
	return logsPtr
}

// handleMarketOrder handles Market orders.
func (book *OrderBook) handleMarketOrder(order *Order, quoteSize udecimal.Decimal) *[]*OrderBookLog {
	var targetQueue *queue
	if order.Side == Buy {
		targetQueue = book.askQueue
	} else {
		targetQueue = book.bidQueue
	}

	logsPtr := acquireLogSlice()
	useQuote := order.Size.Equal(udecimal.Zero) && quoteSize.GreaterThan(udecimal.Zero)
	remainingQuote := quoteSize

	for {
		tOrd := targetQueue.popHeadOrder()
		if tOrd == nil {
			log := NewRejectLog(book.seqID.Add(1), book.marketID, order.ID, order.UserID, RejectReasonNoLiquidity)
			log.Side, log.Price, log.OrderType = order.Side, order.Price, order.Type
			if useQuote {
				log.Size = remainingQuote
			} else {
				log.Size = order.Size
			}
			*logsPtr = append(*logsPtr, log)
			releaseOrder(order)
			return logsPtr
		}

		var matchSize udecimal.Decimal
		if useQuote {
			potentialSize, _ := remainingQuote.Div(tOrd.Price)
			if potentialSize.GreaterThanOrEqual(tOrd.Size) {
				matchSize = tOrd.Size
			} else {
				matchSize = potentialSize
			}
		} else {
			if order.Size.GreaterThanOrEqual(tOrd.Size) {
				matchSize = tOrd.Size
			} else {
				matchSize = order.Size
			}
		}

		if matchSize.Equal(udecimal.Zero) {
			targetQueue.insertOrder(tOrd, true)
			break
		}

		log := NewMatchLog(book.seqID.Add(1), book.tradeID.Add(1), book.marketID, order.ID, order.UserID, order.Side, order.Type, tOrd.ID, tOrd.UserID, tOrd.Price, matchSize)
		*logsPtr = append(*logsPtr, log)

		if useQuote {
			remainingQuote = remainingQuote.Sub(matchSize.Mul(tOrd.Price))
		} else {
			order.Size = order.Size.Sub(matchSize)
		}

		if matchSize.Equal(tOrd.Size) {
			releaseOrder(tOrd)
		} else {
			tOrd.Size = tOrd.Size.Sub(matchSize)
			targetQueue.insertOrder(tOrd, true)
		}

		if (useQuote && remainingQuote.LessThanOrEqual(udecimal.Zero)) || (!useQuote && order.Size.Equal(udecimal.Zero)) {
			releaseOrder(order)
			break
		}
	}
	return logsPtr
}
