package match

import (
	"errors"
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

// lvl builds a depth level from decimal strings.
func lvl(price, size string) protocol.DepthLevel {
	return protocol.DepthLevel{Price: udecimal.MustParse(price), Size: udecimal.MustParse(size)}
}

// deltaBook returns an AggregatedBook at version 10 with bid 100@20 and ask 101@5.
func deltaBook(t *testing.T) *AggregatedBook {
	t.Helper()
	ab := NewAggregatedBook()
	require.NoError(t, ab.ApplySnapshot(&Snapshot{
		SequenceID: 10,
		Asks:       []*protocol.DepthItem{{Price: "101", Size: "5"}},
		Bids:       []*protocol.DepthItem{{Price: "100", Size: "20"}},
	}))
	return ab
}

// requireDeltaBookState asserts the whole book: version and every level on both sides.
func requireDeltaBookState(t *testing.T, ab *AggregatedBook, seq uint64, asks, bids []string) {
	t.Helper()
	require.Equal(t, seq, ab.SequenceID(), "version")
	got := ab.GetDepth(64)
	require.Equal(t, asks, dlLevels(got.Asks), "asks")
	require.Equal(t, bids, dlLevels(got.Bids), "bids")
}

func TestAggregatedBookApplyDeltaRules(t *testing.T) {
	baseAsks := []string{"101@5"}
	baseBids := []string{"100@20"}

	t.Run("continuation sets each level to its size and advances the version", func(t *testing.T) {
		ab := deltaBook(t)
		require.NoError(t, ab.ApplyDelta(&protocol.DepthDelta{
			Seq:  11,
			Bids: []protocol.DepthLevel{lvl("100", "30")},
			Asks: []protocol.DepthLevel{lvl("101", "0")},
		}))
		requireDeltaBookState(t, ab, 11, nil, []string{"100@30"})
	})

	t.Run("a level absent from the book with size zero is a no-op", func(t *testing.T) {
		ab := deltaBook(t)
		require.NoError(t, ab.ApplyDelta(&protocol.DepthDelta{
			Seq:  11,
			Bids: []protocol.DepthLevel{lvl("99", "0")},
		}))
		requireDeltaBookState(t, ab, 11, baseAsks, baseBids)
	})

	t.Run("resend of applied versions is skipped and returns nil", func(t *testing.T) {
		ab := deltaBook(t)
		require.NoError(t, ab.ApplyDelta(&protocol.DepthDelta{
			Seq: 11, Bids: []protocol.DepthLevel{lvl("100", "30")},
		}))
		// Same delta again, and an older one that ends before the version.
		require.NoError(t, ab.ApplyDelta(&protocol.DepthDelta{
			Seq: 11, Bids: []protocol.DepthLevel{lvl("100", "30")},
		}))
		require.NoError(t, ab.ApplyDelta(&protocol.DepthDelta{
			Seq: 10, Bids: []protocol.DepthLevel{lvl("100", "99")},
		}))
		requireDeltaBookState(t, ab, 11, baseAsks, []string{"100@30"})
	})

	t.Run("gap without OnRebuild returns ErrSequenceGap and leaves the book alone", func(t *testing.T) {
		ab := deltaBook(t)
		err := ab.ApplyDelta(&protocol.DepthDelta{
			Seq: 13, Bids: []protocol.DepthLevel{lvl("100", "30")},
		})
		require.ErrorIs(t, err, ErrSequenceGap)
		requireDeltaBookState(t, ab, 10, baseAsks, baseBids)
	})

	t.Run("gap with OnRebuild that connects applies the delta after the snapshot", func(t *testing.T) {
		ab := deltaBook(t)
		rebuilds := 0
		ab.OnRebuild = func() (*Snapshot, error) {
			rebuilds++
			return &Snapshot{
				SequenceID: 12,
				Asks:       []*protocol.DepthItem{{Price: "102", Size: "1"}},
			}, nil
		}
		require.NoError(t, ab.ApplyDelta(&protocol.DepthDelta{
			Seq: 13, Bids: []protocol.DepthLevel{lvl("100", "30")},
		}))
		require.Equal(t, 1, rebuilds)
		requireDeltaBookState(t, ab, 13, []string{"102@1"}, []string{"100@30"})
	})

	t.Run(
		"gap whose OnRebuild snapshot does not connect returns ErrSequenceGap and does not apply it",
		func(t *testing.T) {
			ab := deltaBook(t)
			ab.OnRebuild = func() (*Snapshot, error) {
				return &Snapshot{SequenceID: 9}, nil
			}
			err := ab.ApplyDelta(&protocol.DepthDelta{
				Seq: 13, Bids: []protocol.DepthLevel{lvl("100", "30")},
			})
			require.ErrorIs(t, err, ErrSequenceGap)
			requireDeltaBookState(t, ab, 10, baseAsks, baseBids)
		},
	)

	t.Run("gap whose OnRebuild snapshot already covers the delta skips it", func(t *testing.T) {
		ab := deltaBook(t)
		ab.OnRebuild = func() (*Snapshot, error) {
			return &Snapshot{
				SequenceID: 20,
				Bids:       []*protocol.DepthItem{{Price: "100", Size: "77"}},
			}, nil
		}
		require.NoError(t, ab.ApplyDelta(&protocol.DepthDelta{
			Seq: 13, Bids: []protocol.DepthLevel{lvl("100", "30")},
		}))
		requireDeltaBookState(t, ab, 20, nil, []string{"100@77"})
	})

	t.Run("OnRebuild error is returned and the book is untouched", func(t *testing.T) {
		ab := deltaBook(t)
		boom := errors.New("snapshot unavailable")
		ab.OnRebuild = func() (*Snapshot, error) { return nil, boom }
		err := ab.ApplyDelta(&protocol.DepthDelta{
			Seq: 13, Bids: []protocol.DepthLevel{lvl("100", "30")},
		})
		require.ErrorIs(t, err, boom)
		requireDeltaBookState(t, ab, 10, baseAsks, baseBids)
	})

	t.Run("format errors return ErrInvalidDepthDelta and leave the book alone", func(t *testing.T) {
		cases := map[string]*protocol.DepthDelta{
			"nil delta": nil,
			"seq zero": {
				Seq: 0, Bids: []protocol.DepthLevel{lvl("100", "30")},
			},
			"malformed resend": {
				Seq: 10, Bids: []protocol.DepthLevel{lvl("100", "-1")},
			},
			"negative size": {
				Seq: 11, Bids: []protocol.DepthLevel{lvl("100", "-1")},
			},
			"duplicate bid price": {
				Seq:  11,
				Bids: []protocol.DepthLevel{lvl("100", "30"), lvl("100", "31")},
			},
			"duplicate ask price": {
				Seq:  11,
				Asks: []protocol.DepthLevel{lvl("101", "1"), lvl("101", "2")},
			},
			"bids not descending": {
				Seq:  11,
				Bids: []protocol.DepthLevel{lvl("99", "1"), lvl("100", "2")},
			},
			"asks not ascending": {
				Seq:  11,
				Asks: []protocol.DepthLevel{lvl("102", "1"), lvl("101", "2")},
			},
		}
		for name, d := range cases {
			t.Run(name, func(t *testing.T) {
				ab := deltaBook(t)
				require.ErrorIs(t, ab.ApplyDelta(d), ErrInvalidDepthDelta)
				requireDeltaBookState(t, ab, 10, baseAsks, baseBids)
			})
		}
	})

	t.Run("a malformed level after a valid one leaves every level unchanged", func(t *testing.T) {
		ab := deltaBook(t)
		err := ab.ApplyDelta(&protocol.DepthDelta{
			Seq: 11,
			Bids: []protocol.DepthLevel{
				lvl("100", "40"),
				lvl("99", "-3"),
			},
			Asks: []protocol.DepthLevel{lvl("101", "0")},
		})
		require.ErrorIs(t, err, ErrInvalidDepthDelta)
		requireDeltaBookState(t, ab, 10, baseAsks, baseBids)
	})

	t.Run("malformed delta is refused before any rebuild", func(t *testing.T) {
		ab := deltaBook(t)
		ab.OnRebuild = func() (*Snapshot, error) {
			t.Fatal("OnRebuild must not run for a malformed delta")
			return nil, errors.New("unreachable")
		}
		err := ab.ApplyDelta(&protocol.DepthDelta{
			Seq: 13, Bids: []protocol.DepthLevel{lvl("100", "-30")},
		})
		require.ErrorIs(t, err, ErrInvalidDepthDelta)
		requireDeltaBookState(t, ab, 10, baseAsks, baseBids)
	})

	t.Run("fresh book accepts version 1", func(t *testing.T) {
		ab := NewAggregatedBook()
		require.NoError(t, ab.ApplyDelta(&protocol.DepthDelta{
			Seq: 1, Bids: []protocol.DepthLevel{lvl("100", "3")},
		}))
		requireDeltaBookState(t, ab, 1, nil, []string{"100@3"})
	})
}
