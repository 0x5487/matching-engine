package match

import (
	"fmt"
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

// dlDepthLimit is deep enough for every price the harness uses. The queue
// depth call allocates its result up to the limit, so it is kept small.
const (
	dlDepthLimit = 64
	dlMarket     = "DEPTH-LOG"
)

// dlHarness drives one OrderBook and one AggregatedBook with the same commands.
// The logs of each command go through a DepthDeltaBuilder, and the delta it
// settles is applied to the AggregatedBook, so a test can state the expected log
// fields and the expected depth at each step.
type dlHarness struct {
	t       *testing.T
	book    *OrderBook
	ab      *AggregatedBook
	builder *DepthDeltaBuilder
	seq     int
	ts      int64
	// changed and last describe the delta settled by the most recent command.
	changed bool
	last    protocol.DepthDelta
}

func newDLHarness(t *testing.T) *dlHarness {
	t.Helper()
	return &dlHarness{
		t:       t,
		book:    NewOrderBook(dlMarket, WithPublisher(NewDiscardPublishLog())),
		ab:      NewAggregatedBook(),
		builder: NewDepthDeltaBuilder(),
		ts:      1000,
	}
}

func (h *dlHarness) base() protocol.BaseCommand {
	h.seq++
	h.ts++
	return protocol.BaseCommand{
		MarketID:  dlMarket,
		CommandID: fmt.Sprintf("dl-%d", h.seq),
		UserID:    1,
		Timestamp: h.ts,
	}
}

// apply runs one command, settles its depth delta into the AggregatedBook, and
// returns copies of the logs. The copies are taken before Release zeroes the
// originals.
func (h *dlHarness) apply(run func(*OrderBook) (*LogBatch, error)) []OrderBookLog {
	h.t.Helper()
	copies := h.collect(run)
	h.settle()
	return copies
}

// collect runs one command and feeds its logs to the builder without settling.
// A test uses it to put several SDK calls under one delta.
func (h *dlHarness) collect(run func(*OrderBook) (*LogBatch, error)) []OrderBookLog {
	h.t.Helper()
	batch, err := run(h.book)
	require.NoError(h.t, err)
	copies := make([]OrderBookLog, 0, len(batch.Logs))
	for _, l := range batch.Logs {
		h.builder.Add(l)
		copies = append(copies, *l)
	}
	batch.Release()
	return copies
}

// settle reads the book once, as a caller does before the next command changes
// it, and applies the delta to the AggregatedBook. It requires that a delta exists
// exactly when the depth version moved.
func (h *dlHarness) settle() {
	h.t.Helper()
	versionBefore := h.ab.SequenceID()
	h.changed = h.builder.Flush(h.book, &h.last)
	h.builder.Reset()
	require.Equal(h.t, h.book.SeqID() != versionBefore, h.changed,
		"a delta is produced iff the depth version moved")
	if h.changed {
		require.NoError(h.t, h.ab.ApplyDelta(&h.last), "apply delta %d", h.last.Seq)
		require.Equal(h.t, h.book.SeqID(), h.last.Seq, "the delta ends at the book version")
	}
}

func (h *dlHarness) place(id string, side Side, price, size, visible int64) []OrderBookLog {
	h.t.Helper()
	req := &protocol.PlaceOrderRequest{
		BaseCommand: h.base(),
		OrderID:     id,
		OrderType:   Limit,
		Side:        side,
		Price:       udecimal.MustFromInt64(price, 0),
		Size:        udecimal.MustFromInt64(size, 0),
		VisibleSize: udecimal.MustFromInt64(visible, 0),
	}
	return h.apply(func(b *OrderBook) (*LogBatch, error) { return b.PlaceOrder(req) })
}

// amend sends an amend. A zero newPrice or newSize means "no change" to the
// protocol, so the harness passes it through as zero.
func (h *dlHarness) amend(id string, newPrice, newSize int64) []OrderBookLog {
	h.t.Helper()
	req := &protocol.AmendOrderRequest{
		BaseCommand: h.base(),
		OrderID:     id,
		NewPrice:    udecimal.MustFromInt64(newPrice, 0),
		NewSize:     udecimal.MustFromInt64(newSize, 0),
	}
	return h.apply(func(b *OrderBook) (*LogBatch, error) { return b.AmendOrder(req) })
}

func (h *dlHarness) cancel(id string) []OrderBookLog {
	h.t.Helper()
	req := &protocol.CancelOrderRequest{BaseCommand: h.base(), OrderID: id}
	return h.apply(func(b *OrderBook) (*LogBatch, error) { return b.CancelOrder(req) })
}

// snapshotFromDepth turns a book's depth into the snapshot ApplySnapshot takes. The
// levels are parsed here, in the test, because the depth read is in strings.
func snapshotFromDepth(tb testing.TB, seq uint64, asks, bids []*protocol.DepthItem) *protocol.DepthDelta {
	tb.Helper()
	levels := func(items []*protocol.DepthItem) []protocol.DepthLevel {
		out := make([]protocol.DepthLevel, 0, len(items))
		for _, it := range items {
			out = append(out, protocol.DepthLevel{
				Price: udecimal.MustParse(it.Price),
				Size:  udecimal.MustParse(it.Size),
			})
		}
		return out
	}
	return &protocol.DepthDelta{Seq: seq, Asks: levels(asks), Bids: levels(bids)}
}

// dlLevels renders depth items as "price@size", best level first.
func dlLevels(items []*protocol.DepthItem) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Price+"@"+it.Size)
	}
	return out
}

// requireAggregatedMatchesBook asserts that the AggregatedBook shows the same
// levels in the same order, and the same version, as the OrderBook itself.
func (h *dlHarness) requireAggregatedMatchesBook() {
	h.t.Helper()
	want := h.book.GetDepth(dlDepthLimit)
	got := h.ab.GetDepth(dlDepthLimit)
	require.Equal(h.t, want.UpdateID, got.UpdateID, "depth version: OrderBook vs AggregatedBook")
	require.Equal(h.t, dlLevels(want.Asks), dlLevels(got.Asks), "asks")
	require.Equal(h.t, dlLevels(want.Bids), dlLevels(got.Bids), "bids")
	require.Equal(h.t, h.book.SeqID(), h.ab.SequenceID())
}

// dlWantLog states every field the depth rule and the log contract depend on.
type dlWantLog struct {
	typ            protocol.LogType
	price          string
	size           string
	oldPrice       string
	oldSize        string
	visibleRemoved string
}

func dlRequireLogs(t *testing.T, got []OrderBookLog, want []dlWantLog) {
	t.Helper()
	gotRows := make([]dlWantLog, 0, len(got))
	for _, l := range got {
		gotRows = append(gotRows, dlWantLog{
			typ:            l.Type,
			price:          l.Price.String(),
			size:           l.Size.String(),
			oldPrice:       l.OldPrice.String(),
			oldSize:        l.OldSize.String(),
			visibleRemoved: l.VisibleRemoved.String(),
		})
	}
	require.Equal(t, want, gotRows)
}

type dlRow struct {
	name string
	// setup builds the resting state. Its logs are replayed but not asserted.
	setup func(h *dlHarness)
	// act is the single command under test.
	act func(h *dlHarness) []OrderBookLog

	wantLogs     []dlWantLog
	wantBids     []string
	wantAsks     []string
	versionMoves bool
	// check runs after the assertions above, for state the depth view does not show.
	check func(t *testing.T, h *dlHarness)
	// follow sends one more command after act. It must replay without a gap,
	// which is the contiguity check.
	follow func(h *dlHarness)
}

func dlIceberg(h *dlHarness) { h.place("i1", Buy, 100, 10, 2) }

func TestDepthLogTable(t *testing.T) {
	t.Parallel()

	rows := []dlRow{
		{
			name:  "plain amend price 100 to 101 keeps size",
			setup: func(h *dlHarness) { h.place("s1", Buy, 100, 5, 0) },
			act:   func(h *dlHarness) []OrderBookLog { return h.amend("s1", 101, 0) },
			wantLogs: []dlWantLog{
				{protocol.LogTypeAmend, "101", "5", "100", "5", "5"},
				{protocol.LogTypeOpen, "101", "5", "0", "0", "0"},
			},
			wantBids:     []string{"101@5"},
			versionMoves: true,
		},
		{
			name:  "plain amend size 5 to 8 at same price",
			setup: func(h *dlHarness) { h.place("s1", Buy, 100, 5, 0) },
			act:   func(h *dlHarness) []OrderBookLog { return h.amend("s1", 0, 8) },
			wantLogs: []dlWantLog{
				{protocol.LogTypeAmend, "100", "8", "100", "5", "5"},
				{protocol.LogTypeOpen, "100", "8", "0", "0", "0"},
			},
			wantBids:     []string{"100@8"},
			versionMoves: true,
		},
		{
			name:  "plain amend size 5 to 3 is in place and removes the difference",
			setup: func(h *dlHarness) { h.place("s1", Buy, 100, 5, 0) },
			act:   func(h *dlHarness) []OrderBookLog { return h.amend("s1", 0, 3) },
			wantLogs: []dlWantLog{
				{protocol.LogTypeAmend, "100", "3", "100", "5", "2"},
			},
			wantBids:     []string{"100@3"},
			versionMoves: true,
		},
		{
			name: "iceberg 10 visible 2 canceled, same price holds 5 elsewhere",
			setup: func(h *dlHarness) {
				h.place("s0", Buy, 100, 5, 0)
				dlIceberg(h)
			},
			act: func(h *dlHarness) []OrderBookLog { return h.cancel("i1") },
			wantLogs: []dlWantLog{
				{protocol.LogTypeCancel, "100", "10", "0", "0", "2"},
			},
			wantBids:     []string{"100@5"},
			versionMoves: true,
		},
		{
			name: "iceberg 10 visible 2 amended to 9 removes only the hidden unit",
			setup: func(h *dlHarness) {
				h.place("s0", Buy, 100, 5, 0)
				dlIceberg(h)
			},
			act: func(h *dlHarness) []OrderBookLog { return h.amend("i1", 0, 9) },
			wantLogs: []dlWantLog{
				{protocol.LogTypeAmend, "100", "9", "100", "10", "0"},
			},
			wantBids:     []string{"100@7"},
			versionMoves: false,
		},
		{
			name: "(a) iceberg fully eaten, replenish Open, taker stops",
			setup: func(h *dlHarness) {
				dlIceberg(h)
			},
			act: func(h *dlHarness) []OrderBookLog { return h.place("t1", Sell, 100, 2, 0) },
			wantLogs: []dlWantLog{
				{protocol.LogTypeMatch, "100", "2", "0", "0", "0"},
				{protocol.LogTypeOpen, "100", "2", "0", "0", "0"},
			},
			wantBids:     []string{"100@2"},
			versionMoves: true,
		},
		{
			name: "(a) iceberg fully eaten, replenish Open, taker continues into next order",
			setup: func(h *dlHarness) {
				dlIceberg(h)
				h.place("o1", Buy, 100, 5, 0)
			},
			act: func(h *dlHarness) []OrderBookLog { return h.place("t1", Sell, 100, 4, 0) },
			wantLogs: []dlWantLog{
				{protocol.LogTypeMatch, "100", "2", "0", "0", "0"},
				{protocol.LogTypeOpen, "100", "2", "0", "0", "0"},
				{protocol.LogTypeMatch, "100", "2", "0", "0", "0"},
			},
			wantBids:     []string{"100@5"},
			versionMoves: true,
		},
		{
			name: "(b) amend crosses and fills completely, only Match logs follow",
			setup: func(h *dlHarness) {
				h.place("b1", Buy, 100, 5, 0)
				h.place("a1", Sell, 101, 10, 0)
			},
			act: func(h *dlHarness) []OrderBookLog { return h.amend("b1", 101, 0) },
			wantLogs: []dlWantLog{
				{protocol.LogTypeAmend, "101", "5", "100", "5", "5"},
				{protocol.LogTypeMatch, "101", "5", "0", "0", "0"},
			},
			wantAsks:     []string{"101@5"},
			versionMoves: true,
		},
		{
			name: "(c) amend crosses, partly fills, rests the remainder",
			setup: func(h *dlHarness) {
				h.place("b1", Buy, 100, 5, 0)
				h.place("a1", Sell, 101, 2, 0)
			},
			act: func(h *dlHarness) []OrderBookLog { return h.amend("b1", 101, 0) },
			wantLogs: []dlWantLog{
				{protocol.LogTypeAmend, "101", "5", "100", "5", "5"},
				{protocol.LogTypeMatch, "101", "2", "0", "0", "0"},
				{protocol.LogTypeOpen, "101", "3", "0", "0", "0"},
			},
			wantBids:     []string{"101@3"},
			versionMoves: true,
		},
		{
			name:  "(d) in-place decrease that only touches hidden size",
			setup: func(h *dlHarness) { dlIceberg(h) },
			act:   func(h *dlHarness) []OrderBookLog { return h.amend("i1", 0, 7) },
			wantLogs: []dlWantLog{
				{protocol.LogTypeAmend, "100", "7", "100", "10", "0"},
			},
			wantBids:     []string{"100@2"},
			versionMoves: false,
			follow: func(h *dlHarness) {
				h.place("o2", Sell, 110, 1, 0)
			},
		},
		{
			name:  "(e) in-place decrease past hidden size into the visible part",
			setup: func(h *dlHarness) { dlIceberg(h) },
			act:   func(h *dlHarness) []OrderBookLog { return h.amend("i1", 0, 1) },
			wantLogs: []dlWantLog{
				{protocol.LogTypeAmend, "100", "1", "100", "10", "1"},
			},
			wantBids:     []string{"100@1"},
			versionMoves: true,
		},
		{
			name:  "(e) in-place decrease to exactly the visible cap",
			setup: func(h *dlHarness) { dlIceberg(h) },
			act:   func(h *dlHarness) []OrderBookLog { return h.amend("i1", 0, 2) },
			wantLogs: []dlWantLog{
				{protocol.LogTypeAmend, "100", "2", "100", "10", "0"},
			},
			wantBids:     []string{"100@2"},
			versionMoves: false,
		},
		{
			name: "(f) iceberg amend with price change: old visible leaves, new visible rests, hidden kept",
			setup: func(h *dlHarness) {
				h.place("o0", Buy, 100, 5, 0)
				dlIceberg(h)
			},
			act: func(h *dlHarness) []OrderBookLog { return h.amend("i1", 101, 0) },
			wantLogs: []dlWantLog{
				{protocol.LogTypeAmend, "101", "10", "100", "10", "2"},
				{protocol.LogTypeOpen, "101", "2", "0", "0", "0"},
			},
			wantBids:     []string{"101@2", "100@5"},
			versionMoves: true,
			check: func(t *testing.T, h *dlHarness) {
				t.Helper()
				o, ok := h.book.findOrder("i1")
				require.True(t, ok)
				require.Equal(t, "8", o.HiddenSize.String(), "hidden size must survive a price change")
			},
		},
		{
			name:  "(g) no-op amend repeats the version and removes nothing",
			setup: func(h *dlHarness) { dlIceberg(h) },
			act:   func(h *dlHarness) []OrderBookLog { return h.amend("i1", 100, 10) },
			wantLogs: []dlWantLog{
				{protocol.LogTypeAmend, "100", "10", "100", "10", "0"},
			},
			wantBids:     []string{"100@2"},
			versionMoves: false,
		},
		{
			name: "(i) cancel of an iceberg whose visible part was partly filled",
			setup: func(h *dlHarness) {
				dlIceberg(h)
				h.place("t1", Sell, 100, 1, 0)
			},
			act: func(h *dlHarness) []OrderBookLog { return h.cancel("i1") },
			wantLogs: []dlWantLog{
				{protocol.LogTypeCancel, "100", "9", "0", "0", "1"},
			},
			wantBids:     nil,
			versionMoves: true,
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			h := newDLHarness(t)
			if row.setup != nil {
				row.setup(h)
			}
			h.requireAggregatedMatchesBook()

			before := h.book.SeqID()
			logs := row.act(h)

			dlRequireLogs(t, logs, row.wantLogs)
			require.Equal(t, row.versionMoves, h.book.SeqID() != before, "version moved")
			require.Equal(t, row.wantBids, dlLevels(h.book.GetDepth(dlDepthLimit).Bids), "OrderBook bids")
			require.Equal(t, row.wantAsks, dlLevels(h.book.GetDepth(dlDepthLimit).Asks), "OrderBook asks")
			h.requireAggregatedMatchesBook()

			if row.check != nil {
				row.check(t, h)
			}
			if row.follow != nil {
				row.follow(h)
				h.requireAggregatedMatchesBook()
			}
		})
	}
}

// TestDepthLogSnapshotWithRestingIceberg is rare path (h): an iceberg rests when
// the snapshot is taken. The restored book and AggregatedBook then continue the
// same sequence, and must keep the hidden sizes and the contiguous version.
func TestDepthLogSnapshotWithRestingIceberg(t *testing.T) {
	t.Parallel()

	h := newDLHarness(t)
	h.place("o0", Buy, 100, 5, 0)
	h.place("i1", Buy, 100, 10, 2)
	h.place("i2", Sell, 105, 9, 3)

	snap := h.book.Snapshot()
	depth := h.book.GetDepth(dlDepthLimit)

	restored := NewOrderBook(dlMarket, WithPublisher(NewDiscardPublishLog()))
	restored.Restore(snap)
	ab := NewAggregatedBook()
	require.NoError(t, ab.ApplySnapshot(snapshotFromDepth(t, h.book.SeqID(), depth.Asks, depth.Bids)))
	h.book, h.ab = restored, ab
	h.requireAggregatedMatchesBook()

	i2, ok := h.book.findOrder("i2")
	require.True(t, ok)
	require.Equal(t, "6", i2.HiddenSize.String(), "hidden size survives restore")

	// Hidden-only decrease on the restored sell iceberg: no version move.
	before := h.book.SeqID()
	dlRequireLogs(t, h.amend("i2", 0, 5), []dlWantLog{
		{protocol.LogTypeAmend, "105", "5", "105", "9", "0"},
	})
	require.Equal(t, before, h.book.SeqID())
	h.requireAggregatedMatchesBook()

	// Cancel the restored buy iceberg: removes its visible 2, not its total 10.
	dlRequireLogs(t, h.cancel("i1"), []dlWantLog{
		{protocol.LogTypeCancel, "100", "10", "0", "0", "2"},
	})
	h.requireAggregatedMatchesBook()
	require.Equal(t, []string{"100@5"}, dlLevels(h.book.GetDepth(dlDepthLimit).Bids))
	require.Equal(t, []string{"105@3"}, dlLevels(h.book.GetDepth(dlDepthLimit).Asks))
}

// TestDepthDeltaBuilderAllocatesNothing pins the steady-state producer: with a
// reused DepthDelta, Add and Flush allocate nothing. The logs are built before
// measuring, and the level they touch keeps its size, so only the producer is
// counted.
func TestDepthDeltaBuilderAllocatesNothing(t *testing.T) {
	const rounds = 1000
	h := newDLHarness(t)
	h.place("resting", Buy, 100, 50, 0)

	// Alternating open and cancel touch the same level, so every round is the same
	// shape: one level, sized from the book, which the synthetic logs never change.
	logs := make([]*OrderBookLog, 0, 2*(rounds+1))
	seq := uint64(1)
	for range rounds + 1 {
		seq++
		logs = append(logs, &OrderBookLog{
			SeqID: seq, Type: protocol.LogTypeOpen, Side: Buy,
			Price: udecimal.MustParse("100"), Size: udecimal.MustParse("1"),
		})
		seq++
		logs = append(logs, &OrderBookLog{
			SeqID: seq, Type: protocol.LogTypeCancel, Side: Buy,
			Price: udecimal.MustParse("100"), Size: udecimal.MustParse("1"),
			VisibleRemoved: udecimal.MustParse("1"),
		})
	}

	builder := NewDepthDeltaBuilder()
	var out protocol.DepthDelta
	next := 0
	allocs := testing.AllocsPerRun(rounds, func() {
		builder.Add(logs[next])
		next++
		builder.Flush(h.book, &out)
	})
	require.Zero(t, allocs, "DepthDeltaBuilder Add and Flush must not allocate with a reused DepthDelta")
}
