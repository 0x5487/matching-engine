package match

import (
	"sync"
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0x5487/matching-engine/protocol"
)

func TestAggregatedBook_GetDepth(t *testing.T) {
	ab := NewAggregatedBook()

	snap := &Snapshot{
		SequenceID: 42,
		Asks: []*protocol.DepthItem{
			{Price: "101.5", Size: "10.0"},
			{Price: "102.0", Size: "20.0"},
			{Price: "103.0", Size: "30.0"},
		},
		Bids: []*protocol.DepthItem{
			{Price: "100.0", Size: "15.0"},
			{Price: "99.0", Size: "25.0"},
			{Price: "98.5", Size: "35.0"},
		},
	}

	err := ab.ApplySnapshot(snap)
	require.NoError(t, err)

	t.Run("limit 2 returns top 2 levels", func(t *testing.T) {
		depth := ab.GetDepth(2)
		require.NotNil(t, depth)
		require.Equal(t, uint64(42), depth.UpdateID)

		require.Len(t, depth.Asks, 2)
		require.Equal(t, "101.5", depth.Asks[0].Price)
		require.Equal(t, "10", depth.Asks[0].Size)
		require.Equal(t, "102", depth.Asks[1].Price)
		require.Equal(t, "20", depth.Asks[1].Size)

		require.Len(t, depth.Bids, 2)
		require.Equal(t, "100", depth.Bids[0].Price)
		require.Equal(t, "15", depth.Bids[0].Size)
		require.Equal(t, "99", depth.Bids[1].Price)
		require.Equal(t, "25", depth.Bids[1].Size)
	})

	t.Run("limit larger than book depth returns all levels", func(t *testing.T) {
		depth := ab.GetDepth(10)
		require.NotNil(t, depth)
		require.Len(t, depth.Asks, 3)
		require.Len(t, depth.Bids, 3)
	})

	t.Run("limit 0 returns empty slices", func(t *testing.T) {
		depth := ab.GetDepth(0)
		require.NotNil(t, depth)
		require.Empty(t, depth.Asks)
		require.Empty(t, depth.Bids)
	})
}

func TestAggregatedBook_EdgeCases(t *testing.T) {
	t.Run("nil snapshot returns ErrNilSnapshot", func(t *testing.T) {
		ab := NewAggregatedBook()
		err := ab.ApplySnapshot(nil)
		require.ErrorIs(t, err, ErrNilSnapshot)
	})

	t.Run("Rebuild without callback returns ErrOnRebuildNotSet", func(t *testing.T) {
		ab := NewAggregatedBook()
		err := ab.Rebuild()
		require.ErrorIs(t, err, ErrOnRebuildNotSet)
	})
}

// TestAggregatedBook_Concurrency_Race runs one writer applying deltas against
// several readers. The writer ends the readers when it is done, so the test waits
// on the event, not on a timer.
func TestAggregatedBook_Concurrency_Race(t *testing.T) {
	t.Parallel()

	ab := NewAggregatedBook()
	require.NoError(t, ab.ApplySnapshot(&Snapshot{
		SequenceID: 0,
		Asks: []*protocol.DepthItem{
			{Price: "105", Size: "100"},
		},
		Bids: []*protocol.DepthItem{
			{Price: "100", Size: "100"},
		},
	}))

	done := make(chan struct{})
	var readers sync.WaitGroup
	for range 4 {
		readers.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
					_ = ab.GetDepth(10)
					_, _ = ab.Depth(Buy, udecimal.MustParse("100"))
					_, _ = ab.Depth(Sell, udecimal.MustParse("105"))
				}
			}
		})
	}

	// One writer: a feed is applied in order by one goroutine.
	var writer sync.WaitGroup
	writer.Go(func() {
		for seq := uint64(1); seq <= 200; seq++ {
			assert.NoError(t, ab.ApplyDelta(&protocol.DepthDelta{
				Seq:  seq,
				Bids: []protocol.DepthLevel{lvl("100", udecimal.MustFromInt64(int64(seq), 0).String())},
			}))
		}
	})
	writer.Wait()
	close(done)
	readers.Wait()

	require.Equal(t, uint64(200), ab.SequenceID())
}

func BenchmarkAggregatedBook_ApplyDelta(b *testing.B) {
	ab := NewAggregatedBook()
	price := udecimal.MustParse("100")
	size := udecimal.MustParse("1")
	d := protocol.DepthDelta{Bids: []protocol.DepthLevel{{Price: price, Size: size}}}

	b.ResetTimer()
	b.ReportAllocs()
	for i := range b.N {
		d.Seq = uint64(i + 1)
		_ = ab.ApplyDelta(&d)
	}
}

func BenchmarkAggregatedBook_GetDepth(b *testing.B) {
	ab := NewAggregatedBook()
	for i := range 50 {
		p := udecimal.MustFromInt64(int64(100+i), 0)
		_ = ab.ApplyDelta(&protocol.DepthDelta{
			Seq:  uint64(i + 1),
			Asks: []protocol.DepthLevel{{Price: p, Size: udecimal.MustFromInt64(10, 0)}},
		})
	}

	b.ReportAllocs()
	for b.Loop() {
		_ = ab.GetDepth(10)
	}
}
