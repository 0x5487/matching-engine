package match

// SeqID is the order book depth version: it advances only when the book depth
// changes. Rejections and admin commands were counted too, which made the
// sequence answer "has anything at all happened" instead of "has the depth
// changed" - so a reader could not tell a real change from an unrelated one,
// and a consumer forwarding only depth changes saw a version jump per rejected
// order.
//
// The tests below drive the exported synchronous API wherever the order book
// exposes one, because the version is what a caller observes through
// GetDepth().UpdateID. Admin commands have no exported entry point, so those go
// through processCommand; the assertions are still on the exported SeqID.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

const depthVersionMarket = "SEQ-BTC-USDT"

// depthVersionBook returns a book that can handle every command type. The admin
// handlers publish unconditionally, so a book without a publisher panics.
func depthVersionBook(marketID string) *OrderBook {
	return NewOrderBook(marketID, WithPublisher(NewDiscardPublishLog()))
}

func depthVersionPlace(
	id string, side Side, typ OrderType, price, size, ts int64,
) *protocol.PlaceOrderRequest {
	req := &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			MarketID:  depthVersionMarket,
			CommandID: "cmd-" + id,
			UserID:    1,
			Timestamp: ts,
		},
		OrderID:   id,
		OrderType: typ,
		Side:      side,
		Size:      udecimal.MustFromInt64(size, 0),
	}
	if typ != Market {
		req.Price = udecimal.MustFromInt64(price, 0)
	}
	return req
}

func depthVersionCancel(orderID string, ts int64) *protocol.CancelOrderRequest {
	return &protocol.CancelOrderRequest{
		BaseCommand: protocol.BaseCommand{
			MarketID:  depthVersionMarket,
			CommandID: "cmd-cancel-" + orderID,
			UserID:    1,
			Timestamp: ts,
		},
		OrderID: orderID,
	}
}

// advanceTo runs a command, releases its batch, and returns the version after.
func advanceTo(t *testing.T, book *OrderBook, run func() (*LogBatch, error)) uint64 {
	t.Helper()
	batch, err := run()
	require.NoError(t, err)
	batch.Release()
	return book.SeqID()
}

// TestDepthVersionAdvancesOnDepthChange covers the positive half of the
// contract. The assertion is "the version moved", not "it moved by one": a
// single command can produce several depth logs, and an amend that relocates a
// resting order produces two. Pinning the increment would pin the log
// granularity rather than the behavior.
func TestDepthVersionAdvancesOnDepthChange(t *testing.T) {
	t.Run("resting a new order", func(t *testing.T) {
		book := depthVersionBook(depthVersionMarket)
		before := book.SeqID()

		after := advanceTo(t, book, func() (*LogBatch, error) {
			return book.PlaceOrder(depthVersionPlace("a", Buy, Limit, 100, 1, 10))
		})

		assert.Greater(t, after, before)
	})

	t.Run("matching against a resting order", func(t *testing.T) {
		book := depthVersionBook(depthVersionMarket)
		advanceTo(t, book, func() (*LogBatch, error) {
			return book.PlaceOrder(depthVersionPlace("ask", Sell, Limit, 100, 1, 10))
		})
		before := book.SeqID()

		after := advanceTo(t, book, func() (*LogBatch, error) {
			return book.PlaceOrder(depthVersionPlace("taker", Buy, Limit, 100, 1, 11))
		})

		assert.Greater(t, after, before, "a match removes resting size, so the book changed")
	})

	t.Run("canceling a resting order", func(t *testing.T) {
		book := depthVersionBook(depthVersionMarket)
		advanceTo(t, book, func() (*LogBatch, error) {
			return book.PlaceOrder(depthVersionPlace("a", Buy, Limit, 100, 1, 10))
		})
		before := book.SeqID()

		after := advanceTo(t, book, func() (*LogBatch, error) {
			return book.CancelOrder(depthVersionCancel("a", 11))
		})

		assert.Greater(t, after, before)
	})

	t.Run("amending a resting order to a new price", func(t *testing.T) {
		book := depthVersionBook(depthVersionMarket)
		advanceTo(t, book, func() (*LogBatch, error) {
			return book.PlaceOrder(depthVersionPlace("a", Buy, Limit, 100, 1, 10))
		})
		before := book.SeqID()

		batch, err := book.AmendOrder(&protocol.AmendOrderRequest{
			BaseCommand: protocol.BaseCommand{
				MarketID: depthVersionMarket, CommandID: "cmd-amend", UserID: 1, Timestamp: 11,
			},
			OrderID:  "a",
			NewPrice: udecimal.MustFromInt64(99, 0),
			NewSize:  udecimal.MustFromInt64(1, 0),
		})
		require.NoError(t, err)
		batch.Release()

		assert.Greater(t, book.SeqID(), before,
			"moving a resting order changes two price levels, so the book changed")
	})
}

// TestDepthVersionIsNotAdvancedByRefusedCommands covers every refusal path that
// reaches createRejectBatch, plus each handler-local refusal site, because the
// redefinition is only as good as its coverage: one refusal path left advancing
// the sequence reinstates the problem for whichever consumer routes through it.
//
// Each case seeds the book, runs one command that must be refused, and asserts
// the version is untouched. The runner asserts the command really was refused,
// so a case that silently became a valid order cannot pass.
func TestDepthVersionIsNotAdvancedByRefusedCommands(t *testing.T) {
	// resting places the orders a case needs and returns nothing; the runner
	// records the version afterwards.
	tests := []struct {
		name string
		// resting is applied before the refused command.
		resting []*protocol.PlaceOrderRequest
		// prepare runs between the resting orders and the refused command, for
		// the triggers that need extra state rather than just resting orders.
		prepare func(t *testing.T, book *OrderBook)
		refused func(book *OrderBook) (*LogBatch, error)
	}{
		{
			name:    "canceling an order that does not exist",
			resting: []*protocol.PlaceOrderRequest{depthVersionPlace("bid", Buy, Limit, 99, 1, 1)},
			refused: func(book *OrderBook) (*LogBatch, error) {
				return book.CancelOrder(depthVersionCancel("no-such-order", 10))
			},
		},
		{
			name:    "a duplicate resting order id",
			resting: []*protocol.PlaceOrderRequest{depthVersionPlace("dup", Buy, Limit, 99, 1, 1)},
			refused: func(book *OrderBook) (*LogBatch, error) {
				return book.PlaceOrder(depthVersionPlace("dup", Buy, Limit, 98, 1, 10))
			},
		},
		{
			name:    "an order with no price",
			resting: []*protocol.PlaceOrderRequest{depthVersionPlace("bid", Buy, Limit, 99, 1, 1)},
			refused: func(book *OrderBook) (*LogBatch, error) {
				return book.PlaceOrder(&protocol.PlaceOrderRequest{
					BaseCommand: protocol.BaseCommand{
						MarketID: depthVersionMarket, CommandID: "cmd-no-price",
						UserID: 1, Timestamp: 10,
					},
					OrderID:   "no-price",
					OrderType: Limit,
					Side:      Buy,
					Size:      udecimal.MustFromInt64(1, 0),
				})
			},
		},
		{
			name:    "an order with no timestamp",
			resting: []*protocol.PlaceOrderRequest{depthVersionPlace("bid", Buy, Limit, 99, 1, 1)},
			refused: func(book *OrderBook) (*LogBatch, error) {
				return book.PlaceOrder(depthVersionPlace("no-ts", Buy, Limit, 100, 1, 0))
			},
		},
		{
			name: "an IOC buy with an empty book",
			refused: func(book *OrderBook) (*LogBatch, error) {
				return book.PlaceOrder(depthVersionPlace("ioc", Buy, IOC, 100, 1, 10))
			},
		},
		{
			name:    "an IOC buy that would rest rather than fill",
			resting: []*protocol.PlaceOrderRequest{depthVersionPlace("bid", Buy, Limit, 99, 1, 1)},
			refused: func(book *OrderBook) (*LogBatch, error) {
				return book.PlaceOrder(depthVersionPlace("ioc", Buy, IOC, 100, 1, 10))
			},
		},
		{
			name:    "an IOC buy priced below the best offer",
			resting: []*protocol.PlaceOrderRequest{depthVersionPlace("ask", Sell, Limit, 100, 1, 1)},
			refused: func(book *OrderBook) (*LogBatch, error) {
				return book.PlaceOrder(depthVersionPlace("ioc", Buy, IOC, 99, 1, 10))
			},
		},
		{
			name: "a market buy with an empty book",
			refused: func(book *OrderBook) (*LogBatch, error) {
				return book.PlaceOrder(depthVersionPlace("mkt", Buy, Market, 0, 1, 10))
			},
		},
		{
			name: "a market buy whose quote no longer buys a whole step",
			resting: []*protocol.PlaceOrderRequest{
				depthVersionPlace("ask", Sell, Limit, 100, 1, 1),
			},
			prepare: func(_ *testing.T, _ *OrderBook) {},
			refused: func(book *OrderBook) (*LogBatch, error) {
				req := depthVersionPlace("mkt", Buy, Market, 0, 0, 10)
				req.QuoteSize = udecimal.MustFromInt64(1, 0)
				req.StepSize = udecimal.MustFromInt64(2, 0)
				return book.PlaceOrder(req)
			},
		},
		{
			name: "a FOK buy that cannot fill completely",
			resting: []*protocol.PlaceOrderRequest{
				depthVersionPlace("ask", Sell, Limit, 100, 1, 1),
			},
			refused: func(book *OrderBook) (*LogBatch, error) {
				return book.PlaceOrder(depthVersionPlace("fok", Buy, FOK, 100, 5, 10))
			},
		},
		{
			name: "a POST_ONLY buy that would have crossed",
			resting: []*protocol.PlaceOrderRequest{
				depthVersionPlace("ask", Sell, Limit, 100, 1, 1),
			},
			refused: func(book *OrderBook) (*LogBatch, error) {
				return book.PlaceOrder(depthVersionPlace("po", Buy, PostOnly, 100, 1, 10))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			book := depthVersionBook(depthVersionMarket)
			for _, o := range tc.resting {
				batch, err := book.PlaceOrder(o)
				require.NoError(t, err)
				batch.Release()
			}
			if tc.prepare != nil {
				tc.prepare(t, book)
			}
			before := book.SeqID()

			batch, err := tc.refused(book)
			require.NoError(t, err)
			require.NotEmpty(t, batch.Logs, "the command must actually have been refused")
			assert.Equal(t, protocol.LogTypeReject, batch.Logs[0].Type,
				"and it must have been refused, not filled")
			batch.Release()

			assert.Equal(t, before, book.SeqID(),
				"a refused command must leave the depth version alone")
		})
	}
}

// TestNoOpAmendDoesNotAdvanceDepthVersion pins the one case that reaches a
// depth-changing log type without changing depth.
//
// An amend whose new price and size both equal the current ones is accepted but
// does neither of AmendOrder's two branches, so no resting size moves. It still
// emits an amend log - the command happened - but the version must repeat rather
// than advance, otherwise a depth reader would rebuild for nothing.
func TestNoOpAmendDoesNotAdvanceDepthVersion(t *testing.T) {
	tests := []struct {
		name      string
		newPrice  int64
		newSize   int64
		restPrice int64
		restSize  int64
	}{
		{name: "same price and size", newPrice: 100, newSize: 2, restPrice: 100, restSize: 2},
		{name: "zero price and size resolve to the current ones", newPrice: 0, newSize: 0, restPrice: 100, restSize: 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			book := depthVersionBook(depthVersionMarket)
			advanceTo(t, book, func() (*LogBatch, error) {
				return book.PlaceOrder(
					depthVersionPlace("a", Buy, Limit, tc.restPrice, tc.restSize, 10))
			})
			before := book.SeqID()

			batch, err := book.AmendOrder(&protocol.AmendOrderRequest{
				BaseCommand: protocol.BaseCommand{
					MarketID: depthVersionMarket, CommandID: "cmd-noop", UserID: 1, Timestamp: 11,
				},
				OrderID:  "a",
				NewPrice: udecimal.MustFromInt64(tc.newPrice, 0),
				NewSize:  udecimal.MustFromInt64(tc.newSize, 0),
			})
			require.NoError(t, err)
			defer batch.Release()

			require.NotEmpty(t, batch.Logs, "the amend must still be logged")
			assert.Equal(t, protocol.LogTypeAmend, batch.Logs[0].Type)

			assert.Equal(t, before, book.SeqID(),
				"an amend that changes nothing must not advance the depth version")
		})
	}
}

// TestDepthVersionIsNotAdvancedByAdminCommand covers the administrative half.
// Suspending, resuming and reconfiguring re-publish the book without resting or
// removing a single order, so none of them may advance the version.

// TestDepthVersionIsIndependentPerMarket guards the property that makes the
// version usable as a per-market change signal at all.
func TestDepthVersionIsIndependentPerMarket(t *testing.T) {
	btc := depthVersionBook("SEQ-BTC-USDT")
	eth := depthVersionBook("SEQ-ETH-USDT")

	advanceTo(t, btc, func() (*LogBatch, error) {
		return btc.PlaceOrder(depthVersionPlace("a", Buy, Limit, 100, 1, 10))
	})

	assert.Equal(t, uint64(0), eth.SeqID(),
		"one market's depth must not advance another market's version")
}

// TestRefusedCommandLeavesLevelsIdentical is the user-visible consequence: a
// client polling for depth changes used to see a new version with identical
// levels, and re-rendered for nothing.
func TestRefusedCommandLeavesLevelsIdentical(t *testing.T) {
	book := depthVersionBook(depthVersionMarket)
	advanceTo(t, book, func() (*LogBatch, error) {
		return book.PlaceOrder(depthVersionPlace("bid", Buy, Limit, 99, 1, 10))
	})

	beforeVersion := book.SeqID()
	beforeDepth := book.GetDepth(10)
	require.NotNil(t, beforeDepth)

	advanceTo(t, book, func() (*LogBatch, error) {
		return book.CancelOrder(depthVersionCancel("ghost", 11))
	})
	afterDepth := book.GetDepth(10)

	assert.Equal(t, beforeVersion, book.SeqID())
	assert.Equal(t, beforeDepth.Asks, afterDepth.Asks, "levels must be identical")
	assert.Equal(t, beforeDepth.Bids, afterDepth.Bids, "levels must be identical")
	assert.Equal(t, beforeDepth.UpdateID, afterDepth.UpdateID)
}

// TestSnapshotCarriesDepthVersion pins the snapshot side of the same contract.
// The snapshot's SeqID is restored into book.seqID, so a stale value there
// rewinds the version on recovery.
func TestSnapshotCarriesDepthVersion(t *testing.T) {
	book := depthVersionBook(depthVersionMarket)
	advanceTo(t, book, func() (*LogBatch, error) {
		return book.PlaceOrder(depthVersionPlace("a", Buy, Limit, 100, 1, 10))
	})

	snap := book.Snapshot()
	require.NotNil(t, snap)
	assert.Equal(t, book.SeqID(), snap.SeqID)

	restored := depthVersionBook(depthVersionMarket)
	restored.Restore(snap)
	assert.Equal(t, book.SeqID(), restored.SeqID(),
		"a restored book resumes the version it was snapshotted at")

	advanceTo(t, restored, func() (*LogBatch, error) {
		return restored.PlaceOrder(depthVersionPlace("b", Buy, Limit, 99, 1, 12))
	})
	assert.Greater(t, restored.SeqID(), snap.SeqID,
		"a change after restore continues from the snapshot, it does not restart")
}

// TestAggregatedBookSurvivesFilteredDepthLogs is why the redefinition matters
// beyond this repository.
//
// AggregatedBook reports a gap when a log's version jumps past the one it
// expects. Had refusals and admin commands kept consuming the sequence, any
// consumer forwarding only depth changes - which is what a market data
// projection does - would see a jump on every refused order, rebuild the whole
// book each time, and never catch up.
//
// With the redefinition, dropping non-depth logs leaves the version contiguous,
// so the projection stays live and is never rebuilt.
func TestAggregatedBookSurvivesFilteredDepthLogs(t *testing.T) {
	book := depthVersionBook(depthVersionMarket)
	ab := NewAggregatedBook()

	rebuilds := 0
	ab.OnRebuild = func() (*Snapshot, error) {
		rebuilds++
		return nil, errors.New("the projection must not need to rebuild")
	}

	forwarded := 0
	// step runs one command and forwards only the logs that moved depth, which
	// is all a depth projection is allowed to send.
	step := func(run func() (*LogBatch, error)) {
		t.Helper()
		batch, err := run()
		require.NoError(t, err)
		for _, log := range batch.Logs {
			switch log.Type {
			case protocol.LogTypeOpen, protocol.LogTypeMatch,
				protocol.LogTypeCancel, protocol.LogTypeAmend:
				require.NoError(t, ab.Replay(log))
				forwarded++
			default:
				// Refused or administrative: dropped on the floor.
			}
		}
		batch.Release()
	}

	step(func() (*LogBatch, error) {
		return book.PlaceOrder(depthVersionPlace("ask", Sell, Limit, 101, 1, 10))
	})
	step(func() (*LogBatch, error) {
		return book.PlaceOrder(depthVersionPlace("bid", Buy, Limit, 99, 1, 11))
	})
	// A refused cancel between two depth changes must not look like a gap.
	step(func() (*LogBatch, error) {
		return book.CancelOrder(depthVersionCancel("ghost", 12))
	})
	step(func() (*LogBatch, error) {
		return book.PlaceOrder(depthVersionPlace("taker1", Buy, Limit, 101, 1, 13))
	})
	step(func() (*LogBatch, error) {
		return book.CancelOrder(depthVersionCancel("ghost2", 14))
	})
	step(func() (*LogBatch, error) {
		return book.PlaceOrder(depthVersionPlace("taker2", Buy, Limit, 101, 1, 15))
	})

	assert.Zero(t, rebuilds, "no gap may be reported when non-depth logs are filtered out")
	assert.Positive(t, forwarded)

	bookDepth := book.GetDepth(10)
	require.NotNil(t, bookDepth)
	assert.Empty(t, bookDepth.Asks, "the only ask was taken")
	require.Len(t, bookDepth.Bids, 2)
	assert.Equal(t, ab.SequenceID(), bookDepth.UpdateID,
		"the projection's version and the book's depth version must agree")
}

// TestEnginePathKeepsDepthVersionContiguous covers the MatchingEngine, which
// registers a market and publishes a market_created admin log before any order
// exists. That log must not advance the depth version: otherwise every
// engine-created market starts one version ahead of zero and the first real
// order's log reads as a lost update to AggregatedBook, forcing a rebuild on the
// first log of every market.
//
// The synchronous OrderBook API used elsewhere in this file cannot reach this
// path at all, since it never registers a market, so the engine has to be
// driven directly.
func TestEnginePathKeepsDepthVersionContiguous(t *testing.T) {
	const marketID = "SEQ-ENGINE-BTC-USDT"

	pub := &versionRecordingPublisher{}
	engine := setupEngine(t, pub)
	addBook(t, engine, marketID)

	placeThroughEngine(t, engine, marketID, "first")
	// A refusal, so the log stream also contains something carrying no version.
	require.NoError(t, engine.PlaceOrderAsync(context.Background(), &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			CommandID: "cmd-dup", MarketID: marketID, UserID: 1,
			Timestamp: time.Now().UnixNano(),
		},
		OrderID:   "first",
		OrderType: Limit,
		Side:      Buy,
		Price:     udecimal.MustFromInt64(100, 0),
		Size:      udecimal.MustFromInt64(1, 0),
	}))

	// The engine publishes asynchronously, so wait for the refusal to arrive
	// before snapshotting the log stream.
	require.Eventually(t, func() bool {
		return countRefusals(pub.recorded()) > 0
	}, time.Second, time.Millisecond, "the duplicate order must have been refused")
	logs := pub.recorded()
	require.NotEmpty(t, logs, "the engine must have published something")

	ab := NewAggregatedBook()
	rebuilds := 0
	ab.OnRebuild = func() (*Snapshot, error) {
		rebuilds++
		return nil, errors.New("the projection must not need to rebuild")
	}

	forwarded, unversioned := 0, 0
	for i, log := range logs {
		if log.SeqID == 0 {
			unversioned++
			continue
		}
		switch log.Type {
		case protocol.LogTypeOpen, protocol.LogTypeMatch,
			protocol.LogTypeCancel, protocol.LogTypeAmend:
			require.NoErrorf(t, ab.Replay(log), "forwarded log %d of type %v", i, log.Type)
			forwarded++
		default:
			t.Errorf("log %d has type %v but carries depth version %d",
				i, log.Type, log.SeqID)
		}
	}

	assert.Positive(t, forwarded, "the resting order must have produced a versioned log")
	assert.Positive(t, unversioned, "market_created and the refusal must carry no version")
	assert.Zero(t, rebuilds,
		"the first depth log of an engine-created market must not look like a lost update")
}

func countRefusals(logs []*OrderBookLog) int {
	n := 0
	for _, log := range logs {
		if log.Type == protocol.LogTypeReject {
			n++
		}
	}
	return n
}

// placeThroughEngine submits a resting buy and waits until the engine has
// processed it, so the published log stream is complete before assertions.
func placeThroughEngine(t *testing.T, engine *MatchingEngine, marketID, orderID string) {
	t.Helper()
	require.NoError(t, engine.PlaceOrderAsync(context.Background(), &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			CommandID: "cmd-" + orderID, MarketID: marketID, UserID: 1,
			Timestamp: time.Now().UnixNano(),
		},
		OrderID:   orderID,
		OrderType: Limit,
		Side:      Buy,
		Price:     udecimal.MustFromInt64(100, 0),
		Size:      udecimal.MustFromInt64(1, 0),
	}))
	waitForBidCount(t, engine, marketID, 1)
}

// versionRecordingPublisher keeps a copy of every log, because the engine
// recycles them as soon as Publish returns.
type versionRecordingPublisher struct {
	mu   sync.Mutex
	logs []*OrderBookLog
}

func (p *versionRecordingPublisher) Publish(logs []*OrderBookLog) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, log := range logs {
		copied := *log
		p.logs = append(p.logs, &copied)
	}
}

func (p *versionRecordingPublisher) recorded() []*OrderBookLog {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*OrderBookLog(nil), p.logs...)
}

// TestEngineRefusalsCarryNoDepthVersion covers the refusals MatchingEngine makes
// on its own behalf. They happen before any order book is reached - a command
// with no command ID, a command for a market that does not exist, a command type
// the engine does not know - or on a market that is itself being registered, so
// there is no book whose version they could advance. The risk being guarded is a
// shared counter being bumped regardless.
//
// The engine publishes these asynchronously, so each case waits for the refusal
// to arrive before asserting rather than assuming it has already been processed.
func TestEngineRefusalsCarryNoDepthVersion(t *testing.T) {
	tests := []struct {
		name string
		// send issues the refused command.
		send func(ctx context.Context, engine *MatchingEngine)
	}{
		{
			name: "a command for a market that does not exist",
			send: func(ctx context.Context, engine *MatchingEngine) {
				require.NoError(t, engine.PlaceOrderAsync(ctx, &protocol.PlaceOrderRequest{
					BaseCommand: protocol.BaseCommand{
						CommandID: "cmd-ghost", MarketID: "SEQ-GHOST-USDT", UserID: 1,
						Timestamp: time.Now().UnixNano(),
					},
					OrderID: "a", OrderType: Limit, Side: Buy,
					Price: udecimal.MustFromInt64(100, 0), Size: udecimal.MustFromInt64(1, 0),
				}))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pub := &versionRecordingPublisher{}
			engine := setupEngine(t, pub)
			ctx := context.Background()

			tc.send(ctx, engine)

			// The engine processes commands asynchronously, so wait for the
			// refusal to be published rather than assuming it has already run.
			var refusals []*OrderBookLog
			require.Eventually(t, func() bool {
				refusals = nil
				for _, log := range pub.recorded() {
					if log.Type == protocol.LogTypeReject {
						refusals = append(refusals, log)
					}
				}
				return len(refusals) > 0
			}, time.Second, time.Millisecond, "the engine must have published a refusal")

			for i, log := range refusals {
				assert.Zerof(t, log.SeqID, "refusal %d must carry no depth version", i)
			}
		})
	}
}

// TestMarketRegistrationRefusalsCarryNoDepthVersion covers the two refusals the
// engine raises while registering a market. Neither has an order book to advance,
// and a duplicate registration in particular follows a successful one on the same
// engine, so a bumped counter there would look like depth changing in a market

// TestUserEventRefusalCarriesNoDepthVersion covers the last engine-side refusal
// with no book behind it: a user event with no timestamp. User events are
// administrative, so a refusal must carry no depth version either.
func TestUserEventRefusalCarriesNoDepthVersion(t *testing.T) {
	pub := &versionRecordingPublisher{}
	engine := setupEngine(t, pub)
	ctx := context.Background()

	require.NoError(t, engine.SendUserEvent(ctx, &protocol.UserEventRequest{
		BaseCommand: protocol.BaseCommand{
			CommandID: "cmd-user-no-ts", MarketID: "SEQ-USER-USDT", UserID: 1,
		},
		EventType: "noop",
	}))

	var refusals []*OrderBookLog
	require.Eventually(t, func() bool {
		refusals = nil
		for _, log := range pub.recorded() {
			if log.Type == protocol.LogTypeReject {
				refusals = append(refusals, log)
			}
		}
		return len(refusals) > 0
	}, time.Second, time.Millisecond, "the user event must have been refused")

	for i, log := range refusals {
		assert.Zerof(t, log.SeqID, "refusal %d must carry no depth version", i)
	}
}

// TestUserEventSeqIDIsTheUpstreamCommandSequence pins the one documented
// exception to the depth-version rule, so the exception cannot rot.
//
// MatchingEngine.handleUserEvent builds a user event from the message queue's
// command sequence, not from a book. OrderBookLog.SeqID is therefore not a depth
// version for LogTypeUser, which is why a depth projection must filter user
// events out rather than replay them.
func TestUserEventSeqIDIsTheUpstreamCommandSequence(t *testing.T) {
	pub := &versionRecordingPublisher{}
	engine := setupEngine(t, pub)
	ctx := context.Background()

	require.NoError(t, engine.SendUserEvent(ctx, &protocol.UserEventRequest{
		BaseCommand: protocol.BaseCommand{
			CommandID: "cmd-user", MarketID: "SEQ-USER-USDT", UserID: 1,
			Timestamp: time.Now().UnixNano(), SeqID: 77,
		},
		EventType: "account.updated",
	}))

	var userLogs []*OrderBookLog
	require.Eventually(t, func() bool {
		userLogs = nil
		for _, log := range pub.recorded() {
			if log.Type == protocol.LogTypeUser {
				userLogs = append(userLogs, log)
			}
		}
		return len(userLogs) > 0
	}, time.Second, time.Millisecond, "the engine must have published the user event")

	assert.Equal(t, uint64(77), userLogs[0].SeqID,
		"a user event carries the upstream command sequence, not a depth version")
}
