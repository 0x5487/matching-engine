package match

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

// This file measures *OrderBook the way trading-poc calls it: one goroutine, with PlaceOrder, CancelOrder,
// GetDepth, Snapshot, Restore and AggregatedBook.ApplyDelta called directly. There is no MatchingEngine, no
// disruptor and no channel; those are the paths BenchmarkCrossing_* and BenchmarkProductionWarmBook_* measure.
//
// Steady state. An op that leaves the book as it found it (a 1:1 cross, a rest followed by its cancel) needs no
// refill. Any other op runs in chunks of syncBenchChunk: before each chunk an untimed prepare step puts the book
// back in the state the chunk starts from, and only the chunk's ops are timed. Each benchmark counts the ops whose
// outcome is not the one its name claims, and requires that count to be zero after the timer has stopped.

const (
	syncBenchMarket = "BTC-USDT"
	syncBenchTS     = int64(1_000_000)
	syncBenchSeed   = int64(20261021)
	syncBenchChunk  = 1024

	syncBenchBestBid      = int64(49999) // warm bids rest at and below this price
	syncBenchCrossPrice   = int64(50001) // empty in the warm book: a maker here becomes the best ask
	syncBenchBestAsk      = int64(50002) // warm asks rest at and above this price
	syncBenchWarmLevels   = 200
	syncBenchWarmPerLevel = 5
	syncBenchDeepLevels   = 1000
	syncBenchDeepPerLevel = 10
	syncBenchSnapLevels   = 1000
	syncBenchSnapPerLevel = 5
	syncBenchRestoreBatch = 8
	syncBenchDepthN       = 20
	syncBenchMixLen       = 4096
	syncBenchMixCross     = 80 // percent of mixed units that are a 1:1 cross
	syncBenchMixRest      = 90 // percent that are a rest and its cancel; the remainder are market orders
	syncBenchSlippage     = "0.01"
)

const (
	syncMixCross = iota
	syncMixRest
	syncMixMarket
)

var syncBenchUnit = udecimal.MustFromInt64(1, 0)

type syncBenchSpec struct {
	id    string
	user  uint64
	side  Side
	price int64
}

func syncBenchLimitReq(s syncBenchSpec, size udecimal.Decimal) *protocol.PlaceOrderRequest {
	return &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			MarketID:  syncBenchMarket,
			CommandID: s.id,
			UserID:    s.user,
			Timestamp: syncBenchTS,
		},
		OrderID:   s.id,
		Side:      s.side,
		OrderType: Limit,
		Price:     udecimal.MustFromInt64(s.price, 0),
		Size:      size,
		StepSize:  syncBenchUnit,
	}
}

func syncBenchCancelReq(id string, user uint64) *protocol.CancelOrderRequest {
	return &protocol.CancelOrderRequest{
		BaseCommand: protocol.BaseCommand{
			MarketID:  syncBenchMarket,
			CommandID: id,
			UserID:    user,
			Timestamp: syncBenchTS,
		},
		OrderID: id,
	}
}

// syncBenchWarmSpecs lists the warm book: levels per side, perLevel resting orders at each level. Bids sit at
// syncBenchBestBid downward, asks at syncBenchBestAsk upward, so syncBenchCrossPrice is empty.
func syncBenchWarmSpecs(levels, perLevel int) []syncBenchSpec {
	specs := make([]syncBenchSpec, 0, 2*levels*perLevel)
	for lvl := range levels {
		for k := range perLevel {
			user := 3001 + uint64(k)
			specs = append(specs, syncBenchSpec{
				id:    fmt.Sprintf("wb-%d-%d", lvl, k),
				user:  user,
				side:  Buy,
				price: syncBenchBestBid - int64(lvl),
			}, syncBenchSpec{
				id:    fmt.Sprintf("ws-%d-%d", lvl, k),
				user:  user,
				side:  Sell,
				price: syncBenchBestAsk + int64(lvl),
			})
		}
	}
	return specs
}

func syncBenchWarm(tb testing.TB, levels, perLevel int) (*OrderBook, []syncBenchSpec) {
	tb.Helper()
	book := NewOrderBook(syncBenchMarket)
	specs := syncBenchWarmSpecs(levels, perLevel)
	for _, s := range specs {
		batch, err := book.PlaceOrder(syncBenchLimitReq(s, syncBenchUnit))
		require.NoError(tb, err)
		require.True(tb, syncBenchOK(batch, nil, protocol.LogTypeOpen), "warm order %s did not rest", s.id)
	}
	return book, specs
}

// syncBenchRun runs b.N operations in chunks of at most chunk. Before each chunk, prepare(n) runs with the timer
// stopped (nil: nothing to prepare); then timed(n) runs the chunk's n ops with the timer on.
func syncBenchRun(b *testing.B, chunk int, prepare, timed func(n int)) {
	b.Helper()
	b.StopTimer()
	b.ResetTimer()
	for done := 0; done < b.N; {
		n := min(chunk, b.N-done)
		if prepare != nil {
			prepare(n)
		}
		b.StartTimer()
		timed(n)
		b.StopTimer()
		done += n
	}
}

// syncBenchOK reports whether a batch holds exactly one log of the wanted type, and releases it.
func syncBenchOK(batch *LogBatch, err error, want protocol.LogType) bool {
	if batch == nil {
		return false
	}
	return err == nil && len(batch.Logs) == 1 && batch.Logs[0].Type == want
}

// syncBenchFills reports whether a batch is one match against makerID for size, and releases it.
func syncBenchFills(batch *LogBatch, err error, makerID string, size udecimal.Decimal) bool {
	if batch == nil {
		return false
	}
	if err != nil || len(batch.Logs) != 1 {
		return false
	}
	l := batch.Logs[0]
	return l.Type == protocol.LogTypeMatch && l.MakerOrderID == makerID && l.Size.Equal(size)
}

// syncBenchSweepOK reports whether a batch is exactly k matches and nothing else, and releases it.
func syncBenchSweepOK(batch *LogBatch, err error, k int) bool {
	if batch == nil {
		return false
	}
	if err != nil || len(batch.Logs) != k {
		return false
	}
	for _, l := range batch.Logs {
		if l.Type != protocol.LogTypeMatch {
			return false
		}
	}
	return true
}

// syncBenchTakenMaker returns the maker a single match consumed, and releases the batch. The ID is copied out
// before the release.
func syncBenchTakenMaker(batch *LogBatch, err error) (string, bool) {
	if batch == nil {
		return "", false
	}
	if err != nil || len(batch.Logs) != 1 || batch.Logs[0].Type != protocol.LogTypeMatch {
		return "", false
	}
	return batch.Logs[0].MakerOrderID, true
}

// syncBenchSlippageLimit is the protection price matching.go derives for a market buy with a slippage limit:
// the best ask times factor. It reads the book through GetDepth(1), as matching.go does.
func syncBenchSlippageLimit(book *OrderBook, factor udecimal.Decimal) (udecimal.Decimal, bool) {
	depth := book.GetDepth(1)
	if len(depth.Asks) != 1 {
		return udecimal.Zero, false
	}
	best, err := udecimal.Parse(depth.Asks[0].Price)
	if err != nil {
		return udecimal.Zero, false
	}
	return best.Mul(factor), true
}

// BenchmarkSyncS1_PlaceResting: a limit order that rests on a warm book (200 levels per side, 5 orders each),
// at varied levels on both sides. Timed: PlaceOrder. Untimed refill: CancelOrder of the chunk.
func BenchmarkSyncS1_PlaceResting(b *testing.B) {
	book, _ := syncBenchWarm(b, syncBenchWarmLevels, syncBenchWarmPerLevel)

	placed := make([]*protocol.PlaceOrderRequest, syncBenchChunk)
	cancels := make([]*protocol.CancelOrderRequest, syncBenchChunk)
	for j := range placed {
		lvl := int64(j % syncBenchWarmLevels)
		s := syncBenchSpec{
			id:    fmt.Sprintf("s1-%d", j),
			user:  1001,
			side:  Buy,
			price: syncBenchBestBid - lvl,
		}
		if j%2 == 1 {
			s.side, s.price = Sell, syncBenchBestAsk+lvl
		}
		placed[j] = syncBenchLimitReq(s, syncBenchUnit)
		cancels[j] = syncBenchCancelReq(s.id, s.user)
	}

	prev, bad := 0, 0
	syncBenchRun(b, syncBenchChunk,
		func(_ int) {
			for j := range prev {
				batch, err := book.CancelOrder(cancels[j])
				if !syncBenchOK(batch, err, protocol.LogTypeCancel) {
					b.Fatalf("refill: cancel %d did not find its order", j)
				}
			}
		},
		func(n int) {
			for j := range n {
				batch, err := book.PlaceOrder(placed[j])
				if !syncBenchOK(batch, err, protocol.LogTypeOpen) {
					bad++
				}
			}
			prev = n
		},
	)
	require.Zero(b, bad, "%d placements did not rest", bad)
}

// BenchmarkSyncS2_Cross1to1: trading-poc's pattern on a warm book. A maker rests at the top (50001, empty
// otherwise) and a taker of equal size crosses it. Each order counts as one op: b.N orders, half makers and half
// takers.
func BenchmarkSyncS2_Cross1to1(b *testing.B) {
	book, _ := syncBenchWarm(b, syncBenchWarmLevels, syncBenchWarmPerLevel)

	maker := syncBenchLimitReq(syncBenchSpec{
		id: "s2-maker", user: 1001, side: Sell, price: syncBenchCrossPrice,
	}, syncBenchUnit)
	taker := syncBenchLimitReq(syncBenchSpec{
		id: "s2-taker", user: 2002, side: Buy, price: syncBenchCrossPrice,
	}, syncBenchUnit)

	bad := 0
	syncBenchRun(b, syncBenchChunk, nil, func(n int) {
		for j := range n {
			if j%2 == 0 {
				batch, err := book.PlaceOrder(maker)
				if !syncBenchOK(batch, err, protocol.LogTypeOpen) {
					bad++
				}
				continue
			}
			batch, err := book.PlaceOrder(taker)
			if !syncBenchFills(batch, err, maker.OrderID, syncBenchUnit) {
				bad++
			}
		}
	})
	require.Zero(b, bad, "%d orders did not do what the benchmark claims", bad)
}

// BenchmarkSyncS3_Sweep: an IOC taker of size k consumes k resting makers, either all at one level or one each
// across k levels. The book holds only the makers. One op is one sweep (k matches); per consumed maker, divide by k.
func BenchmarkSyncS3_Sweep(b *testing.B) {
	for _, k := range [...]int{1, 10, 100} {
		for _, shape := range [...]syncBenchShape{syncBenchSameLevel, syncBenchAcrossLevels} {
			b.Run(fmt.Sprintf("k=%d_%s", k, shape), func(b *testing.B) {
				syncBenchSweep(b, k, shape)
			})
		}
	}
}

// syncBenchShape is where the makers of a sweep rest: all at one level, or one per level.
type syncBenchShape string

const (
	syncBenchSameLevel    syncBenchShape = "same_level"
	syncBenchAcrossLevels syncBenchShape = "across_levels"
)

func syncBenchSweep(b *testing.B, k int, shape syncBenchShape) {
	b.Helper()
	across := shape == syncBenchAcrossLevels
	perChunk := max(1, syncBenchChunk/k)

	makers := make([]*protocol.PlaceOrderRequest, perChunk*k)
	for j := range makers {
		price := syncBenchBestAsk
		if across {
			price += int64(j % k)
		}
		makers[j] = syncBenchLimitReq(syncBenchSpec{
			id: fmt.Sprintf("sw-%d", j), user: 1001, side: Sell, price: price,
		}, syncBenchUnit)
	}

	bound := syncBenchBestAsk
	if across {
		bound += int64(k - 1)
	}
	taker := syncBenchLimitReq(syncBenchSpec{
		id: "sw-taker", user: 2002, side: Buy, price: bound,
	}, udecimal.MustFromInt64(int64(k), 0))
	taker.OrderType = IOC

	book := NewOrderBook(syncBenchMarket)
	bad := 0
	syncBenchRun(b, perChunk,
		func(n int) {
			for j := range n * k {
				batch, err := book.PlaceOrder(makers[j])
				if !syncBenchOK(batch, err, protocol.LogTypeOpen) {
					b.Fatalf("maker %d did not rest", j)
				}
			}
		},
		func(n int) {
			for range n {
				batch, err := book.PlaceOrder(taker)
				if !syncBenchSweepOK(batch, err, k) {
					bad++
				}
			}
		},
	)
	require.Zero(b, bad, "%d sweeps did not take exactly %d makers", bad, k)
}

// BenchmarkSyncS4_CancelDeep: CancelOrder of a resting order at a random position in a deep book (1000 levels per
// side, 10 orders each). Timed: CancelOrder only. Untimed refill: the same orders placed back at the tail of their
// levels, so the book keeps its size.
func BenchmarkSyncS4_CancelDeep(b *testing.B) {
	book, specs := syncBenchWarm(b, syncBenchDeepLevels, syncBenchDeepPerLevel)
	total := len(specs)
	order := rand.New(rand.NewSource(syncBenchSeed)).Perm(total) //nolint:gosec // seeded, not security

	cancels := make([]*protocol.CancelOrderRequest, total)
	places := make([]*protocol.PlaceOrderRequest, total)
	for i, s := range specs {
		cancels[i] = syncBenchCancelReq(s.id, s.user)
		places[i] = syncBenchLimitReq(s, syncBenchUnit)
	}

	start, prev, bad := 0, 0, 0
	syncBenchRun(b, syncBenchChunk,
		func(_ int) {
			for j := range prev {
				batch, err := book.PlaceOrder(places[order[(start+j)%total]])
				if !syncBenchOK(batch, err, protocol.LogTypeOpen) {
					b.Fatalf("refill: place %d did not rest", j)
				}
			}
			start += prev
		},
		func(n int) {
			for j := range n {
				batch, err := book.CancelOrder(cancels[order[(start+j)%total]])
				if !syncBenchOK(batch, err, protocol.LogTypeCancel) {
					bad++
				}
			}
			prev = n
		},
	)
	require.Zero(b, bad, "%d cancels did not find their order", bad)
}

// BenchmarkSyncS5_MarketSlippage: a market buy with a slippage limit, as matching.go does it: GetDepth(1), the
// protection price best ask x (1 + 0.01), then PlaceOrder. Untimed refill: sells at the best ask, so every market
// buy takes one order from the same level.
func BenchmarkSyncS5_MarketSlippage(b *testing.B) {
	book, _ := syncBenchWarm(b, syncBenchWarmLevels, syncBenchWarmPerLevel)
	factor := udecimal.One.Add(udecimal.MustParse(syncBenchSlippage))

	market := &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			MarketID:  syncBenchMarket,
			CommandID: "s5-market",
			UserID:    2002,
			Timestamp: syncBenchTS,
		},
		OrderID:   "s5-market",
		Side:      Buy,
		OrderType: Market,
		Size:      syncBenchUnit,
		StepSize:  syncBenchUnit,
	}

	// Refill orders carry IDs from a ring of 2*syncBenchChunk requests built here. Between two chunks at most
	// syncBenchWarmPerLevel refill orders stay resting (the ones behind the chunk's consumed head), so a ring
	// longer than chunk+5 never reuses a resting ID.
	refills := make([]*protocol.PlaceOrderRequest, 2*syncBenchChunk)
	for j := range refills {
		refills[j] = syncBenchLimitReq(syncBenchSpec{
			id: fmt.Sprintf("s5-%d", j), user: 1001, side: Sell, price: syncBenchBestAsk,
		}, syncBenchUnit)
	}
	next, bad := 0, 0
	syncBenchRun(b, syncBenchChunk,
		func(n int) {
			for range n {
				req := refills[next%len(refills)]
				next++
				batch, err := book.PlaceOrder(req)
				if !syncBenchOK(batch, err, protocol.LogTypeOpen) {
					b.Fatalf("refill %s did not rest", req.OrderID)
				}
			}
		},
		func(n int) {
			for range n {
				limit, ok := syncBenchSlippageLimit(book, factor)
				if !ok {
					bad++
					continue
				}
				market.Price = limit
				batch, err := book.PlaceOrder(market)
				if !syncBenchOK(batch, err, protocol.LogTypeMatch) {
					bad++
				}
			}
		},
	)
	require.Zero(b, bad, "%d market orders did not fill one maker", bad)
}

// BenchmarkSyncS6_GetDepth20: GetDepth(20) on the warm book. Read only; no refill.
func BenchmarkSyncS6_GetDepth20(b *testing.B) {
	book, _ := syncBenchWarm(b, syncBenchWarmLevels, syncBenchWarmPerLevel)

	bad := 0
	syncBenchRun(b, syncBenchChunk, nil, func(n int) {
		for range n {
			depth := book.GetDepth(syncBenchDepthN)
			if len(depth.Bids) != syncBenchDepthN || len(depth.Asks) != syncBenchDepthN {
				bad++
			}
		}
	})
	require.Zero(b, bad, "%d depth reads were not %d levels per side", bad, syncBenchDepthN)
}

// syncBenchFollower returns an AggregatedBook that holds the levels of book at its
// version, as a follower holds them once it has applied every delta. Lookups on it
// read the same sizes the book reports.
func syncBenchFollower(tb testing.TB, book *OrderBook) *AggregatedBook {
	tb.Helper()
	depth := book.GetDepth(syncBenchWarmLevels * 2)
	ab := NewAggregatedBook()
	require.NoError(tb, ab.ApplySnapshot(snapshotFromDepth(tb, book.SeqID(), depth.Asks, depth.Bids)))
	return ab
}

// BenchmarkSyncS7_DepthDelta: what trading-poc pays per command to produce the depth delta of the S2 cross. Each
// order is one call on the warm book; its logs go to a DepthDeltaBuilder, which is settled and reset after the
// call. One op is one order. The produced delta must end at the book's version.
func BenchmarkSyncS7_DepthDelta(b *testing.B) {
	book, _ := syncBenchWarm(b, syncBenchWarmLevels, syncBenchWarmPerLevel)
	maker := syncBenchLimitReq(syncBenchSpec{
		id: "s2-maker", user: 1001, side: Sell, price: syncBenchCrossPrice,
	}, syncBenchUnit)
	taker := syncBenchLimitReq(syncBenchSpec{
		id: "s2-taker", user: 2002, side: Buy, price: syncBenchCrossPrice,
	}, syncBenchUnit)

	builder := NewDepthDeltaBuilder()
	var delta protocol.DepthDelta
	bad := 0
	syncBenchRun(b, syncBenchChunk, nil, func(n int) {
		for j := range n {
			var batch *LogBatch
			var err error
			if j%2 == 0 {
				batch, err = book.PlaceOrder(maker)
			} else {
				batch, err = book.PlaceOrder(taker)
			}
			if err != nil || batch == nil {
				bad++
				continue
			}
			builder.AddBatch(batch.Logs)
			if !builder.Flush(book, &delta) || delta.Seq != book.SeqID() {
				bad++
			}
			builder.Reset()
		}
	})
	require.Zero(b, bad, "%d orders produced no delta that ends at the book version", bad)
}

// BenchmarkSyncS7_DepthLookup: AggregatedBook.Depth(side, price) on a view that holds the warm book's 200 levels
// per side, 5 orders each. Every lookup must return 5.
func BenchmarkSyncS7_DepthLookup(b *testing.B) {
	book, _ := syncBenchWarm(b, syncBenchWarmLevels, syncBenchWarmPerLevel)
	view := syncBenchFollower(b, book)

	type lookup struct {
		side  Side
		price udecimal.Decimal
	}
	lookups := make([]lookup, 0, 2*syncBenchWarmLevels)
	for lvl := range syncBenchWarmLevels {
		lookups = append(lookups,
			lookup{Buy, udecimal.MustFromInt64(syncBenchBestBid-int64(lvl), 0)},
			lookup{Sell, udecimal.MustFromInt64(syncBenchBestAsk+int64(lvl), 0)},
		)
	}

	want := udecimal.MustFromInt64(syncBenchWarmPerLevel, 0)
	bad := 0
	b.ResetTimer()
	for i := range b.N {
		q := lookups[i%len(lookups)]
		size, err := view.Depth(q.side, q.price)
		if err != nil || !size.Equal(want) {
			bad++
		}
	}
	b.StopTimer()
	require.Zero(b, bad, "%d lookups were not %s", bad, want)
}

// BenchmarkSyncS8_Snapshot: Snapshot of a warm book of 10000 orders (1000 levels per side, 5 each). Reference
// only; not on the hot path.
func BenchmarkSyncS8_Snapshot(b *testing.B) {
	book, _ := syncBenchWarm(b, syncBenchSnapLevels, syncBenchSnapPerLevel)
	const total = 2 * syncBenchSnapLevels * syncBenchSnapPerLevel

	bad := 0
	syncBenchRun(b, syncBenchChunk, nil, func(n int) {
		for range n {
			snap := book.Snapshot()
			if len(snap.Bids)+len(snap.Asks) != total {
				bad++
			}
		}
	})
	require.Zero(b, bad, "%d snapshots did not hold %d orders", bad, total)
}

// BenchmarkSyncS8_Restore: Restore of a 10000-order snapshot into fresh books, syncBenchRestoreBatch at a time.
// The fresh books are built untimed; the last restored book is checked after the timer stops.
func BenchmarkSyncS8_Restore(b *testing.B) {
	src, _ := syncBenchWarm(b, syncBenchSnapLevels, syncBenchSnapPerLevel)
	snap := src.Snapshot()

	restored := make([]*OrderBook, syncBenchRestoreBatch)
	lastN := 0
	syncBenchRun(b, syncBenchRestoreBatch,
		func(n int) {
			for j := range n {
				restored[j] = NewOrderBook(syncBenchMarket)
			}
		},
		func(n int) {
			for j := range n {
				restored[j].Restore(snap)
			}
			lastN = n
		},
	)
	got := restored[lastN-1].Snapshot()
	require.Len(b, got.Bids, syncBenchSnapLevels*syncBenchSnapPerLevel)
	require.Len(b, got.Asks, syncBenchSnapLevels*syncBenchSnapPerLevel)
}

// syncBenchMixSchedule is a seeded sequence of unit kinds: syncBenchMixCross percent crosses, then rests, then
// market orders with their replenishing maker.
func syncBenchMixSchedule() []int {
	rng := rand.New(rand.NewSource(syncBenchSeed)) //nolint:gosec // seeded, not security
	kinds := make([]int, syncBenchMixLen)
	for i := range kinds {
		switch r := rng.Intn(100); {
		case r < syncBenchMixCross:
			kinds[i] = syncMixCross
		case r < syncBenchMixRest:
			kinds[i] = syncMixRest
		default:
			kinds[i] = syncMixMarket
		}
	}
	return kinds
}

// BenchmarkSyncMixed: one unit per b.N, drawn from a seeded schedule near the GCP load: 80% 1:1 crosses (2 book
// ops), 10% a rest and its cancel (2 book ops), 10% market buys with a slippage limit (GetDepth(1), the market
// order, then a replenishing sell at the level it consumed: 2 book ops). Each unit restores the book it started
// from, so the replenish is timed as part of the unit. Report per unit, and per book op as ns/op divided by 2.
func BenchmarkSyncMixed(b *testing.B) {
	book, _ := syncBenchWarm(b, syncBenchWarmLevels, syncBenchWarmPerLevel)
	schedule := syncBenchMixSchedule()
	factor := udecimal.One.Add(udecimal.MustParse(syncBenchSlippage))

	cross := syncBenchLimitReq(syncBenchSpec{
		id: "mx-maker", user: 1001, side: Sell, price: syncBenchCrossPrice,
	}, syncBenchUnit)
	taker := syncBenchLimitReq(syncBenchSpec{
		id: "mx-taker", user: 2002, side: Buy, price: syncBenchCrossPrice,
	}, syncBenchUnit)
	rest := syncBenchLimitReq(syncBenchSpec{
		id: "mx-rest", user: 1001, side: Buy, price: syncBenchBestBid,
	}, syncBenchUnit)
	restCancel := syncBenchCancelReq("mx-rest", 1001)
	market := &protocol.PlaceOrderRequest{
		BaseCommand: protocol.BaseCommand{
			MarketID:  syncBenchMarket,
			CommandID: "mx-market",
			UserID:    2002,
			Timestamp: syncBenchTS,
		},
		OrderID:   "mx-market",
		Side:      Buy,
		OrderType: Market,
		Size:      syncBenchUnit,
		StepSize:  syncBenchUnit,
	}
	replenish := syncBenchLimitReq(syncBenchSpec{
		user: 3001, side: Sell, price: syncBenchBestAsk,
	}, syncBenchUnit)

	bad := 0
	b.ResetTimer()
	for i := range b.N {
		switch schedule[i%len(schedule)] {
		case syncMixCross:
			batch, err := book.PlaceOrder(cross)
			if !syncBenchOK(batch, err, protocol.LogTypeOpen) {
				bad++
			}
			batch, err = book.PlaceOrder(taker)
			if !syncBenchFills(batch, err, cross.OrderID, syncBenchUnit) {
				bad++
			}
		case syncMixRest:
			batch, err := book.PlaceOrder(rest)
			if !syncBenchOK(batch, err, protocol.LogTypeOpen) {
				bad++
			}
			batch, err = book.CancelOrder(restCancel)
			if !syncBenchOK(batch, err, protocol.LogTypeCancel) {
				bad++
			}
		default:
			limit, ok := syncBenchSlippageLimit(book, factor)
			if !ok {
				bad++
				continue
			}
			market.Price = limit
			batch, err := book.PlaceOrder(market)
			consumed, ok := syncBenchTakenMaker(batch, err)
			if !ok {
				bad++
				continue
			}
			replenish.OrderID, replenish.CommandID = consumed, consumed
			batch, err = book.PlaceOrder(replenish)
			if !syncBenchOK(batch, err, protocol.LogTypeOpen) {
				bad++
			}
		}
	}
	b.StopTimer()
	require.Zero(b, bad, "%d units did not do what the schedule claims", bad)
}
