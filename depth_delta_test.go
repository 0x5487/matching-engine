package match

import (
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

// market sends a market order of the given base size, with StepSize 1.
func (h *dlHarness) market(size int64) []OrderBookLog {
	h.t.Helper()
	req := &protocol.PlaceOrderRequest{
		BaseCommand: h.base(),
		OrderID:     "taker",
		OrderType:   Market,
		Side:        Buy,
		Size:        udecimal.MustFromInt64(size, 0),
		StepSize:    udecimal.MustFromInt64(1, 0),
	}
	return h.apply(func(b *OrderBook) (*LogBatch, error) { return b.PlaceOrder(req) })
}

// requireDelta asserts the settled delta's version and its levels. Levels are
// "price@size", the form dlLevels uses.
func (h *dlHarness) requireDelta(seq uint64, asks, bids []string) {
	h.t.Helper()
	require.True(h.t, h.changed, "a delta was expected")
	require.Equal(h.t, seq, h.last.Seq, "version")
	require.Equal(h.t, asks, dlDeltaLevels(h.last.Asks), "asks")
	require.Equal(h.t, bids, dlDeltaLevels(h.last.Bids), "bids")
}

// requireNoDelta asserts the command moved no depth: no delta and no version move.
func (h *dlHarness) requireNoDelta(version uint64) {
	h.t.Helper()
	require.False(h.t, h.changed, "no delta expected")
	require.Equal(h.t, version, h.book.SeqID(), "version must not move")
}

func dlDeltaLevels(levels []protocol.DepthLevel) []string {
	if len(levels) == 0 {
		return nil
	}
	out := make([]string, 0, len(levels))
	for _, l := range levels {
		out = append(out, l.Price.String()+"@"+l.Size.String())
	}
	return out
}

// TestDepthDeltaNoChangeYieldsNoDelta is rare path (a): a command that moves no
// visible depth produces no delta, and the version does not move.
func TestDepthDeltaNoChangeYieldsNoDelta(t *testing.T) {
	t.Parallel()

	t.Run("refused command", func(t *testing.T) {
		t.Parallel()
		h := newDLHarness(t)
		h.place("a", Sell, 101, 5, 0)
		version := h.book.SeqID()
		h.apply(func(b *OrderBook) (*LogBatch, error) {
			return b.PlaceOrder(&protocol.PlaceOrderRequest{
				BaseCommand: h.base(), OrderID: "a", OrderType: Limit, Side: Buy,
				Price: udecimal.MustFromInt64(101, 0), Size: udecimal.MustFromInt64(1, 0),
			})
		})
		h.requireNoDelta(version)
	})

	t.Run("hidden-only amend", func(t *testing.T) {
		t.Parallel()
		h := newDLHarness(t)
		h.place("i", Buy, 100, 10, 2)
		version := h.book.SeqID()
		h.amend("i", 0, 8)
		h.requireNoDelta(version)
	})

	t.Run("no-op amend", func(t *testing.T) {
		t.Parallel()
		h := newDLHarness(t)
		h.place("a", Buy, 100, 4, 0)
		version := h.book.SeqID()
		h.amend("a", 100, 4)
		h.requireNoDelta(version)
	})
}

// TestDepthDeltaSpansSeveralSDKCalls is rare path (b) for a caller that merges:
// one command made of several SDK calls is settled as one delta, once, after the
// last call. Each call advances the version by one, so the merged delta ends at the
// last version and is not a step from the one before it: it is the caller's own
// output, not a feed a follower applies with ApplyDelta.
func TestDepthDeltaSpansSeveralSDKCalls(t *testing.T) {
	t.Parallel()

	h := newDLHarness(t)
	h.place("a", Sell, 101, 1, 0)
	h.place("b", Sell, 102, 2, 0)
	h.place("c", Sell, 103, 3, 0)
	version := h.book.SeqID()

	for _, id := range []string{"a", "b", "c"} {
		h.collect(func(b *OrderBook) (*LogBatch, error) {
			return b.CancelOrder(&protocol.CancelOrderRequest{BaseCommand: h.base(), OrderID: id})
		})
	}
	var out protocol.DepthDelta
	require.True(t, h.builder.Flush(h.book, &out))
	h.builder.Reset()
	require.Equal(t, version+3, out.Seq, "the merged delta carries the last version")
	require.Equal(t, []string{"101@0", "102@0", "103@0"}, dlDeltaLevels(out.Asks))
	require.Equal(t, version+3, h.book.SeqID())
}

// TestDepthDeltaBatchFormSpansCalls feeds the same three cancels through
// AddBatch, one batch per call, and checks the delta equals the per-log form.
func TestDepthDeltaBatchFormSpansCalls(t *testing.T) {
	t.Parallel()

	h := newDLHarness(t)
	h.place("a", Sell, 101, 1, 0)
	h.place("b", Sell, 102, 2, 0)
	version := h.book.SeqID()

	builder := NewDepthDeltaBuilder()
	for _, id := range []string{"a", "b"} {
		batch, err := h.book.CancelOrder(&protocol.CancelOrderRequest{BaseCommand: h.base(), OrderID: id})
		require.NoError(t, err)
		builder.AddBatch(batch.Logs)
		batch.Release()
	}
	var out protocol.DepthDelta
	require.True(t, builder.Flush(h.book, &out))
	require.Equal(t, version+2, out.Seq)
	require.Equal(t, []string{"101@0", "102@0"}, dlDeltaLevels(out.Asks))
}

// TestDepthDeltaTouchesEachLevelOnce is rare path (c): a command that touches the
// same level twice appears once in the delta, with the level's final size.
func TestDepthDeltaTouchesEachLevelOnce(t *testing.T) {
	t.Parallel()

	h := newDLHarness(t)
	h.place("m1", Sell, 100, 3, 0)
	h.place("m2", Sell, 100, 4, 0)
	h.place("m3", Sell, 101, 6, 0)
	version := h.book.SeqID()

	// The buy takes m1 whole and 2 of m2, both at 100: two match logs on one
	// level. It never reaches 101, so only 100 is in the delta, at its final size.
	h.market(5)
	h.requireDelta(version+1, []string{"100@2"}, nil)
	require.Equal(t, []string{"100@2", "101@6"}, dlLevels(h.book.GetDepth(dlDepthLimit).Asks))
}

// TestDepthDeltaEmptiedLevelIsZeroAndRemoved is rare path (d): a level that
// empties appears with size zero, and applying the delta removes it.
func TestDepthDeltaEmptiedLevelIsZeroAndRemoved(t *testing.T) {
	t.Parallel()

	h := newDLHarness(t)
	h.place("only", Buy, 99, 5, 0)
	h.place("keep", Buy, 98, 2, 0)
	version := h.book.SeqID()

	h.cancel("only")
	h.requireDelta(version+1, nil, []string{"99@0"})
	require.Equal(t, []string{"98@2"}, dlLevels(h.ab.GetDepth(dlDepthLimit).Bids))
	size, err := h.ab.Depth(Buy, udecimal.MustParse("99"))
	require.NoError(t, err)
	require.True(t, size.IsZero(), "the emptied level is gone from the AggregatedBook")
}

// TestDepthDeltaCrossingPriorityLossFillsCompletely is rare path (e): an amend
// that loses priority by crossing the book and filling completely. The amend
// removes the old bid level, and the match takes the ask; the delta carries both.
func TestDepthDeltaCrossingPriorityLossFillsCompletely(t *testing.T) {
	t.Parallel()

	h := newDLHarness(t)
	h.place("ask", Sell, 101, 5, 0)
	h.place("bid", Buy, 100, 5, 0)
	version := h.book.SeqID()

	// Price 101 crosses the ask and the size fills it in full.
	h.amend("bid", 101, 5)
	h.requireDelta(version+1, []string{"101@0"}, []string{"100@0"})
	require.Equal(t, version+1, h.book.SeqID(), "the amend and its match are one call, one version")
	_, ok := h.book.findOrder("bid")
	require.False(t, ok, "the amended order filled completely and must not rest")
}

// TestDepthDeltaReplenishInsideSweep is rare path (f): a taker sweeps an iceberg
// and its hidden size reloads inside the same command. The level's visible size
// after the sweep is the reloaded remainder, once.
func TestDepthDeltaReplenishInsideSweep(t *testing.T) {
	t.Parallel()

	h := newDLHarness(t)
	h.place("ice", Sell, 101, 10, 2)
	version := h.book.SeqID()

	// Buy 5 walks the iceberg: match 2, reload 2, match 2, reload 2, match 1. The
	// order keeps 1 visible and 4 hidden, so the level's visible size is 1.
	h.market(5)
	require.Equal(t, version+1, h.book.SeqID(),
		"three matches and two replenish opens are one call, one version")
	h.requireDelta(version+1, []string{"101@1"}, nil)
	ice, ok := h.book.findOrder("ice")
	require.True(t, ok)
	require.Equal(t, "1", ice.Size.String())
	require.Equal(t, "4", ice.HiddenSize.String())
}

// TestDepthDeltaPriceUnitReopens is rare path (k): a level empties, its price unit
// returns to the pool, and the same price reopens with a new order. The delta
// shows only the new order's size, with no size carried over.
func TestDepthDeltaPriceUnitReopens(t *testing.T) {
	t.Parallel()

	h := newDLHarness(t)
	h.place("old", Buy, 100, 9, 0)
	h.cancel("old")
	require.Equal(t, []string{"100@0"}, dlDeltaLevels(h.last.Bids))

	version := h.book.SeqID()
	h.place("new", Buy, 100, 4, 0)
	h.requireDelta(version+1, nil, []string{"100@4"})
	require.Equal(t, []string{"100@4"}, dlLevels(h.ab.GetDepth(dlDepthLimit).Bids))
}

// TestOneCallOneVersion is rare path (b): one SDK call that moves visible depth in
// several logs gives every one of them the same version, and the book advances by
// exactly one. The three shapes are a sweep over several levels, an iceberg
// replenish inside a sweep, and a priority-losing amend that re-matches.
func TestOneCallOneVersion(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		setup func(h *dlHarness)
		run   func(h *dlHarness) []OrderBookLog
	}{
		"sweep over several levels": {
			setup: func(h *dlHarness) {
				h.place("a1", Sell, 101, 1, 0)
				h.place("a2", Sell, 102, 1, 0)
				h.place("a3", Sell, 103, 1, 0)
			},
			run: func(h *dlHarness) []OrderBookLog {
				return h.market(3)
			},
		},
		"iceberg replenish inside a sweep": {
			setup: func(h *dlHarness) {
				h.place("ice", Sell, 101, 10, 2)
			},
			run: func(h *dlHarness) []OrderBookLog {
				return h.market(3)
			},
		},
		"priority-losing amend that re-matches": {
			setup: func(h *dlHarness) {
				h.place("ask", Sell, 101, 5, 0)
				h.place("bid", Buy, 100, 5, 0)
			},
			run: func(h *dlHarness) []OrderBookLog {
				return h.amend("bid", 101, 5)
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newDLHarness(t)
			tc.setup(h)
			before := h.book.SeqID()
			logs := tc.run(h)

			require.Greater(t, len(logs), 1, "the call must produce several depth logs")
			for _, l := range logs {
				require.Equal(t, before+1, l.SeqID, "log %s carries the call's version", l.Type)
			}
			require.Equal(t, before+1, h.book.SeqID(), "the call advances the book by exactly one")
			require.True(t, h.changed, "the call settles one delta")
			require.Equal(t, before+1, h.last.Seq, "the delta carries the call's version")
		})
	}
}
