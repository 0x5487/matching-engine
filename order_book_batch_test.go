package match

// The book owns its LogBatch. A call returns that batch and the logs in it; the
// next call on the same book reclaims them. These tests pin what a caller may
// rely on: each call's batch holds that call's logs and nothing else, books and
// the engine never share a batch, and there is no Release to call.

import (
	"reflect"
	"strconv"
	"sync"
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

const batchMarket = "BTC-USDT"

// batchPlace is a limit order of the given size at the given price. The
// command ID is "cmd-"+id, so a log can be traced to the call that made it.
func batchPlace(market, id string, side protocol.Side, price, seq int64) *protocol.PlaceOrderRequest {
	return &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			CommandID: "cmd-" + id,
			MarketID:  market,
			UserID:    1,
			Timestamp: seq,
		},
		OrderID:   id,
		OrderType: protocol.OrderTypeLimit,
		Side:      side,
		Price:     udecimal.MustFromInt64(price, 0),
		Size:      udecimal.MustFromInt64(1, 0),
	}
}

// batchCancel cancels the order id, with command ID "cmd-cancel-"+id.
func batchCancel(market, id string, seq int64) *protocol.CancelOrderRequest {
	return &protocol.CancelOrderRequest{
		BaseCommand: protocol.BaseCommand{
			CommandID: "cmd-cancel-" + id,
			MarketID:  market,
			UserID:    1,
			Timestamp: seq,
		},
		OrderID: id,
	}
}

// requireOnlyCommand asserts that logs holds exactly one entry, and that it was
// produced by commandID.
func requireOnlyCommand(tb testing.TB, logs []*OrderBookLog, commandID string) {
	tb.Helper()
	require.Len(tb, logs, 1)
	for _, l := range logs {
		require.Equal(tb, commandID, l.CommandID, "log %s of %s is from another call", l.Type, l.OrderID)
	}
}

// TestLogBatch_HasNoRelease fixes the API: the caller hands nothing back, so a
// Release method on LogBatch would bring back the double-release hazard.
func TestLogBatch_HasNoRelease(t *testing.T) {
	_, ok := reflect.TypeFor[*LogBatch]().MethodByName("Release")
	assert.False(t, ok, "LogBatch must not have Release: the book reclaims its batch when its next call starts")
}

// TestBookBatch_EachCallHoldsOnlyItsOwnLogs covers two calls in a row on one book.
// The second batch must hold only the second call's logs.
func TestBookBatch_EachCallHoldsOnlyItsOwnLogs(t *testing.T) {
	book := NewOrderBook(batchMarket)

	first, err := book.PlaceOrder(batchPlace(batchMarket, "s1", protocol.SideSell, 100, 1))
	require.NoError(t, err)
	requireOnlyCommand(t, first.Logs, "cmd-s1")
	require.Equal(t, protocol.LogTypeOpen, first.Logs[0].Type)

	second, err := book.PlaceOrder(batchPlace(batchMarket, "b1", protocol.SideBuy, 100, 2))
	require.NoError(t, err)
	requireOnlyCommand(t, second.Logs, "cmd-b1")
	require.Equal(t, protocol.LogTypeMatch, second.Logs[0].Type)
	require.Equal(t, "s1", second.Logs[0].MakerOrder.ID, "the match names the resting order it took")

	third, err := book.PlaceOrder(batchPlace(batchMarket, "r1", protocol.SideBuy, 99, 3))
	require.NoError(t, err)
	requireOnlyCommand(t, third.Logs, "cmd-r1")
	require.Equal(t, protocol.LogTypeOpen, third.Logs[0].Type)

	cancel, err := book.CancelOrder(batchCancel(batchMarket, "r1", 4))
	require.NoError(t, err)
	requireOnlyCommand(t, cancel.Logs, "cmd-cancel-r1")
	require.Equal(t, protocol.LogTypeCancel, cancel.Logs[0].Type)
}

// TestBookBatch_TwoBooksKeepTheirOwnBatches interleaves calls on two books. A call
// on one book must not reclaim or overwrite what the other book returned.
func TestBookBatch_TwoBooksKeepTheirOwnBatches(t *testing.T) {
	const marketA, marketB = "A-USDT", "B-USDT"
	a := NewOrderBook(marketA)
	b := NewOrderBook(marketB)

	aRest, err := a.PlaceOrder(batchPlace(marketA, "a1", protocol.SideSell, 100, 1))
	require.NoError(t, err)
	requireOnlyCommand(t, aRest.Logs, "cmd-a1")

	bRest, err := b.PlaceOrder(batchPlace(marketB, "b1", protocol.SideSell, 200, 1))
	require.NoError(t, err)
	requireOnlyCommand(t, bRest.Logs, "cmd-b1")

	aFill, err := a.PlaceOrder(batchPlace(marketA, "a2", protocol.SideBuy, 100, 2))
	require.NoError(t, err)
	requireOnlyCommand(t, aFill.Logs, "cmd-a2")
	require.Equal(t, protocol.LogTypeMatch, aFill.Logs[0].Type)

	// b's batch was returned before a's last two calls; a's calls must not have touched it.
	requireOnlyCommand(t, bRest.Logs, "cmd-b1")
	require.Equal(t, "b1", bRest.Logs[0].OrderID)

	bCancel, err := b.CancelOrder(batchCancel(marketB, "b1", 3))
	require.NoError(t, err)
	requireOnlyCommand(t, bCancel.Logs, "cmd-cancel-b1")
}

// TestBookBatch_AmendLogsStayInTheBooksBatch covers an amend that moves logs
// between the amend and the re-match. The result must match the batch the amend
// returned before the batch became the book's own: the amend log, then the
// three matches it produced, all with the same depth version.
func TestBookBatch_AmendLogsStayInTheBooksBatch(t *testing.T) {
	book := NewOrderBook(amendMarket)

	for i, price := range []int64{100, 101, 102} {
		_, err := book.PlaceOrder(amendPlace(t, "ask-"+string(rune('a'+i)), protocol.SideSell, price, int64(10+i)))
		require.NoError(t, err)
	}
	_, err := book.PlaceOrder(amendPlace(t, "bid", protocol.SideBuy, 99, 20))
	require.NoError(t, err)

	batch, err := book.AmendOrder(amendOrder(t, "bid", 103, 3, 21))
	require.NoError(t, err)
	require.Len(t, batch.Logs, 4)

	want := []struct {
		typ          protocol.LogType
		orderID      string
		makerOrderID string
		price, size  string
		tradeID      uint64
	}{
		{protocol.LogTypeAmend, "bid", "", "103", "3", 0},
		{protocol.LogTypeMatch, "bid", "ask-a", "100", "1", 1},
		{protocol.LogTypeMatch, "bid", "ask-b", "101", "1", 2},
		{protocol.LogTypeMatch, "bid", "ask-c", "102", "1", 3},
	}
	for i, w := range want {
		l := batch.Logs[i]
		assert.Equal(t, w.typ, l.Type, "log %d type", i)
		assert.Equal(t, w.orderID, l.OrderID, "log %d order", i)
		assert.Equal(t, w.makerOrderID, l.MakerOrder.ID, "log %d maker", i)
		assert.Equal(t, w.price, l.Price.String(), "log %d price", i)
		assert.Equal(t, w.size, quantityOf(l).String(), "log %d size", i)
		assert.Equal(t, w.tradeID, l.TradeID, "log %d trade", i)
		assert.Equal(t, uint64(5), l.SeqID, "log %d depth version", i)
	}
	amendLog := batch.Logs[0]
	assert.Equal(t, "99", amendLog.OldPrice.String(), "amend keeps the pre-amend price")
	assert.Equal(t, "1", amendLog.OldRemaining.String(), "amend keeps the pre-amend size")
	assert.Equal(t, "1", amendLog.VisibleRemoved.String(), "amend reports the visible size it took off")
}

// TestBookBatch_RejectDoesNotLeakIntoNextCall covers a call that returns a reject
// batch, followed by a normal call. The normal call's batch must hold its own log only.
func TestBookBatch_RejectDoesNotLeakIntoNextCall(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		reject func(book *OrderBook) (*LogBatch, error)
	}{
		{"invalid place", func(book *OrderBook) (*LogBatch, error) {
			return book.PlaceOrder(batchPlace(batchMarket, "bad", protocol.SideBuy, 0, 1))
		}},
		{"cancel not found", func(book *OrderBook) (*LogBatch, error) {
			return book.CancelOrder(batchCancel(batchMarket, "nope", 1))
		}},
		{"amend not found", func(book *OrderBook) (*LogBatch, error) {
			return book.AmendOrder(&protocol.AmendOrderRequest{
				BaseCommand: protocol.BaseCommand{
					CommandID: "cmd-nope-amend",
					MarketID:  batchMarket,
					UserID:    1,
					Timestamp: 1,
				},
				OrderID:  "nope",
				NewPrice: udecimal.MustFromInt64(101, 0),
				NewSize:  udecimal.MustFromInt64(1, 0),
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			book := NewOrderBook(batchMarket)

			rejected, err := tc.reject(book)
			require.NoError(t, err)
			require.Len(t, rejected.Logs, 1)
			require.Equal(t, protocol.LogTypeReject, rejected.Logs[0].Type)

			next, err := book.PlaceOrder(batchPlace(batchMarket, "ok", protocol.SideSell, 100, 9))
			require.NoError(t, err)
			requireOnlyCommand(t, next.Logs, "cmd-ok")
			require.Equal(t, protocol.LogTypeOpen, next.Logs[0].Type)
		})
	}
}

// TestEngineBatch_DoesNotTouchBookBatch interleaves engine-level logs (a reject and
// a user event, which the engine publishes itself) with calls on a book the engine
// owns. A book batch returned before the engine's events must still hold exactly
// what that call put in it, and the engine's logs must come out in order.
func TestEngineBatch_DoesNotTouchBookBatch(t *testing.T) {
	pub := NewMemoryPublishLog()
	engine := NewMatchingEngine("engine-batch", pub)
	book := newOrderBookWithPublisher("engine-batch", batchMarket, pub)
	engine.orderbooks[batchMarket] = book

	held, err := book.PlaceOrder(batchPlace(batchMarket, "s1", protocol.SideSell, 100, 1))
	require.NoError(t, err)
	requireOnlyCommand(t, held.Logs, "cmd-s1")

	engine.processCommand(&InputEvent{Request: &protocol.CancelOrderRequest{
		BaseCommand: protocol.BaseCommand{CommandID: "cmd-nomarket", MarketID: "NOPE", UserID: 1, Timestamp: 2},
		OrderID:     "x",
	}})
	engine.processCommand(&InputEvent{Request: &protocol.UserEventRequest{
		BaseCommand: protocol.BaseCommand{CommandID: "cmd-user-bad", MarketID: batchMarket, UserID: 9, Timestamp: 0},
		EventType:   "EndOfBlock",
		Key:         "k",
	}})
	engine.processCommand(&InputEvent{Request: &protocol.UserEventRequest{
		BaseCommand: protocol.BaseCommand{CommandID: "cmd-user", MarketID: batchMarket, UserID: 9, Timestamp: 3},
		EventType:   "EndOfBlock",
		Key:         "k",
		Data:        []byte("d"),
	}})

	// The batch the book returned before the engine's events is still intact.
	requireOnlyCommand(t, held.Logs, "cmd-s1")
	require.Equal(t, protocol.LogTypeOpen, held.Logs[0].Type)
	require.Equal(t, "s1", held.Logs[0].OrderID)

	// A book call publishes through the same Publisher; the engine's logs keep their order.
	engine.processCommand(&InputEvent{Request: batchCancel(batchMarket, "s1", 4)})

	logs := pub.Logs()
	require.Len(t, logs, 4)
	wantCmd := []string{"cmd-nomarket", "cmd-user-bad", "cmd-user", "cmd-cancel-s1"}
	wantType := []protocol.LogType{
		protocol.LogTypeReject, protocol.LogTypeReject, protocol.LogTypeUser, protocol.LogTypeCancel,
	}
	for i := range logs {
		assert.Equal(t, wantCmd[i], logs[i].CommandID, "published log %d", i)
		assert.Equal(t, wantType[i], logs[i].Type, "published log %d", i)
	}
	assert.Equal(t, protocol.RejectReasonMarketNotFound, logs[0].RejectReason)
	assert.Equal(t, protocol.RejectReasonInvalidPayload, logs[1].RejectReason)
	assert.Equal(t, []byte("d"), logs[2].Data)
}

// TestBookPublisher_KeptLogsSurviveLaterCalls checks the Publisher contract.
// MemoryPublishLog copies inside Publish, so the logs it kept are still right after
// the book has run more calls and reclaimed its batch.
func TestBookPublisher_KeptLogsSurviveLaterCalls(t *testing.T) {
	pub := NewMemoryPublishLog()
	book := NewOrderBook(batchMarket, WithPublisher(pub))

	book.processCommand(&InputEvent{Request: batchPlace(batchMarket, "s1", protocol.SideSell, 100, 1)})
	book.processCommand(&InputEvent{Request: batchPlace(batchMarket, "b1", protocol.SideBuy, 100, 2)})
	book.processCommand(&InputEvent{Request: batchPlace(batchMarket, "r1", protocol.SideBuy, 99, 3)})
	book.processCommand(&InputEvent{Request: batchPlace(batchMarket, "r2", protocol.SideBuy, 98, 4)})

	logs := pub.Logs()
	require.Len(t, logs, 4)
	assert.Equal(t, protocol.LogTypeOpen, logs[0].Type)
	assert.Equal(t, "s1", logs[0].OrderID)
	assert.Equal(t, "100", logs[0].Price.String())
	assert.Equal(t, protocol.LogTypeMatch, logs[1].Type)
	assert.Equal(t, "b1", logs[1].OrderID)
	assert.Equal(t, "s1", logs[1].MakerOrder.ID)
	assert.Equal(t, uint64(1), logs[1].TradeID)
	assert.Equal(t, protocol.LogTypeOpen, logs[2].Type)
	assert.Equal(t, "r1", logs[2].OrderID)
	assert.Equal(t, "99", logs[2].Price.String())
	assert.Equal(t, "r2", logs[3].OrderID)
	assert.Equal(t, "98", logs[3].Price.String())
}

// TestBooksOwnTheirBatchesAcrossGoroutines runs several books, each on its own
// goroutine, with a place and a cancel per round. Under -race this shows that a
// book's batch is touched only by that book's caller.
func TestBooksOwnTheirBatchesAcrossGoroutines(t *testing.T) {
	const books, rounds = 8, 200
	var wg sync.WaitGroup
	for b := range books {
		wg.Go(func() {
			market := "M" + strconv.Itoa(b)
			book := NewOrderBook(market)
			for r := range rounds {
				id := "s" + strconv.Itoa(r)
				placed, err := book.PlaceOrder(batchPlace(market, id, protocol.SideSell, 100, int64(r+1)))
				if err != nil {
					t.Errorf("place %s on %s: %v", id, market, err)
					return
				}
				if !assert.Len(t, placed.Logs, 1) || !assert.Equal(t, "cmd-"+id, placed.Logs[0].CommandID) {
					return
				}
				canceled, err := book.CancelOrder(batchCancel(market, id, int64(r+1)))
				if err != nil {
					t.Errorf("cancel %s on %s: %v", id, market, err)
					return
				}
				if !assert.Len(t, canceled.Logs, 1) || !assert.Equal(t, "cmd-cancel-"+id, canceled.Logs[0].CommandID) {
					return
				}
			}
		})
	}
	wg.Wait()
}

// TestSyncCalls_AllocsDoNotGrow pins the steady-state allocations per call,
// measured with testing.AllocsPerRun over 1000 calls (identical with and without
// -race). Before the book owned its batch: rest 4, fill 3, cancel 3. After: rest 1,
// fill 0, cancel 0. The bounds are the after numbers, so a regression that
// allocates on the hot path fails here.
func TestSyncCalls_AllocsDoNotGrow(t *testing.T) {
	const runs = 1000 // AllocsPerRun makes runs+1 calls, so each case prepares runs+1 requests

	t.Run("rest", func(t *testing.T) {
		book := NewOrderBook(batchMarket)
		reqs := make([]*protocol.PlaceOrderRequest, runs+1)
		for i := range reqs {
			reqs[i] = batchPlace(batchMarket, "r"+strconv.Itoa(i), protocol.SideSell, 100, 1)
		}
		i := 0
		allocs := testing.AllocsPerRun(runs, func() {
			_, _ = book.PlaceOrder(reqs[i])
			i++
		})
		require.LessOrEqual(t, allocs, float64(1), "PlaceOrder that rests (before: 4)")
	})

	t.Run("fill", func(t *testing.T) {
		book := NewOrderBook(batchMarket)
		for i := range runs + 1 {
			_, err := book.PlaceOrder(batchPlace(batchMarket, "s"+strconv.Itoa(i), protocol.SideSell, 100, 1))
			require.NoError(t, err)
		}
		buys := make([]*protocol.PlaceOrderRequest, runs+1)
		for i := range buys {
			buys[i] = batchPlace(batchMarket, "b"+strconv.Itoa(i), protocol.SideBuy, 100, 2)
		}
		i := 0
		allocs := testing.AllocsPerRun(runs, func() {
			_, _ = book.PlaceOrder(buys[i])
			i++
		})
		require.Zero(t, allocs, "PlaceOrder with a fill (before: 3)")
	})

	t.Run("cancel", func(t *testing.T) {
		book := NewOrderBook(batchMarket)
		cancels := make([]*protocol.CancelOrderRequest, runs+1)
		for i := range cancels {
			id := "c" + strconv.Itoa(i)
			_, err := book.PlaceOrder(batchPlace(batchMarket, id, protocol.SideSell, 100, 1))
			require.NoError(t, err)
			cancels[i] = batchCancel(batchMarket, id, 2)
		}
		i := 0
		allocs := testing.AllocsPerRun(runs, func() {
			_, _ = book.CancelOrder(cancels[i])
			i++
		})
		require.Zero(t, allocs, "CancelOrder (before: 3)")
	})
}
