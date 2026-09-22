package match

import (
	"testing"
	"time"

	"github.com/quagmt/udecimal"
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

func TestAggregatedBook_Replay_Open(t *testing.T) {
	ab := NewAggregatedBook()

	// 1. Buy order opens at price 100 with size 10
	err := ab.Replay(&OrderBookLog{
		SeqID: 1,
		Type:  protocol.LogTypeOpen,
		Side:  Buy,
		Price: udecimal.MustParse("100"),
		Size:  udecimal.MustParse("10"),
	})
	require.NoError(t, err)

	// 2. Another buy order opens at price 100 with size 5
	err = ab.Replay(&OrderBookLog{
		SeqID: 2,
		Type:  protocol.LogTypeOpen,
		Side:  Buy,
		Price: udecimal.MustParse("100"),
		Size:  udecimal.MustParse("5"),
	})
	require.NoError(t, err)

	// 3. Sell order opens at price 105 with size 8
	err = ab.Replay(&OrderBookLog{
		SeqID: 3,
		Type:  protocol.LogTypeOpen,
		Side:  Sell,
		Price: udecimal.MustParse("105"),
		Size:  udecimal.MustParse("8"),
	})
	require.NoError(t, err)

	require.Equal(t, uint64(3), ab.SequenceID())

	bidDepth, err := ab.Depth(Buy, udecimal.MustParse("100"))
	require.NoError(t, err)
	require.Equal(t, udecimal.MustParse("15"), bidDepth)

	askDepth, err := ab.Depth(Sell, udecimal.MustParse("105"))
	require.NoError(t, err)
	require.Equal(t, udecimal.MustParse("8"), askDepth)

	depth := ab.GetDepth(10)
	require.Len(t, depth.Asks, 1)
	require.Equal(t, "105", depth.Asks[0].Price)
	require.Equal(t, "8", depth.Asks[0].Size)

	require.Len(t, depth.Bids, 1)
	require.Equal(t, "100", depth.Bids[0].Price)
	require.Equal(t, "15", depth.Bids[0].Size)
}

func TestAggregatedBook_Replay_Match(t *testing.T) {
	ab := NewAggregatedBook()

	// Initial book: Sell 10 @ 105, Buy 20 @ 100
	require.NoError(t, ab.Replay(&OrderBookLog{
		SeqID: 1,
		Type:  protocol.LogTypeOpen,
		Side:  Sell,
		Price: udecimal.MustParse("105"),
		Size:  udecimal.MustParse("10"),
	}))
	require.NoError(t, ab.Replay(&OrderBookLog{
		SeqID: 2,
		Type:  protocol.LogTypeOpen,
		Side:  Buy,
		Price: udecimal.MustParse("100"),
		Size:  udecimal.MustParse("20"),
	}))

	// 1. Taker Buy matches 4 @ 105 against maker Ask
	require.NoError(t, ab.Replay(&OrderBookLog{
		SeqID: 3,
		Type:  protocol.LogTypeMatch,
		Side:  Buy, // taker side
		Price: udecimal.MustParse("105"),
		Size:  udecimal.MustParse("4"),
	}))

	askDepth, err := ab.Depth(Sell, udecimal.MustParse("105"))
	require.NoError(t, err)
	require.Equal(t, udecimal.MustParse("6"), askDepth)

	// 2. Taker Buy matches remaining 6 @ 105 against maker Ask
	require.NoError(t, ab.Replay(&OrderBookLog{
		SeqID: 4,
		Type:  protocol.LogTypeMatch,
		Side:  Buy,
		Price: udecimal.MustParse("105"),
		Size:  udecimal.MustParse("6"),
	}))

	askDepth, err = ab.Depth(Sell, udecimal.MustParse("105"))
	require.NoError(t, err)
	require.Equal(t, udecimal.Zero, askDepth)

	depth := ab.GetDepth(10)
	require.Empty(t, depth.Asks)

	// 3. Taker Sell matches 15 @ 100 against maker Bid
	require.NoError(t, ab.Replay(&OrderBookLog{
		SeqID: 5,
		Type:  protocol.LogTypeMatch,
		Side:  Sell, // taker side
		Price: udecimal.MustParse("100"),
		Size:  udecimal.MustParse("15"),
	}))

	bidDepth, err := ab.Depth(Buy, udecimal.MustParse("100"))
	require.NoError(t, err)
	require.Equal(t, udecimal.MustParse("5"), bidDepth)

	depth = ab.GetDepth(10)
	require.Len(t, depth.Bids, 1)
	require.Equal(t, "100", depth.Bids[0].Price)
	require.Equal(t, "5", depth.Bids[0].Size)
}

func TestAggregatedBook_Replay_Cancel(t *testing.T) {
	ab := NewAggregatedBook()

	// Initial book: Buy 15 @ 100, Sell 10 @ 105
	require.NoError(t, ab.Replay(&OrderBookLog{
		SeqID: 1,
		Type:  protocol.LogTypeOpen,
		Side:  Buy,
		Price: udecimal.MustParse("100"),
		Size:  udecimal.MustParse("15"),
	}))
	require.NoError(t, ab.Replay(&OrderBookLog{
		SeqID: 2,
		Type:  protocol.LogTypeOpen,
		Side:  Sell,
		Price: udecimal.MustParse("105"),
		Size:  udecimal.MustParse("10"),
	}))

	// 1. Cancel partial size on Buy side (cancel 5 out of 15)
	require.NoError(t, ab.Replay(&OrderBookLog{
		SeqID: 3,
		Type:  protocol.LogTypeCancel,
		Side:  Buy,
		Price: udecimal.MustParse("100"),
		Size:  udecimal.MustParse("5"),
	}))

	bidDepth, err := ab.Depth(Buy, udecimal.MustParse("100"))
	require.NoError(t, err)
	require.Equal(t, udecimal.MustParse("10"), bidDepth)

	// 2. Cancel full size on Sell side (cancel 10 out of 10)
	require.NoError(t, ab.Replay(&OrderBookLog{
		SeqID: 4,
		Type:  protocol.LogTypeCancel,
		Side:  Sell,
		Price: udecimal.MustParse("105"),
		Size:  udecimal.MustParse("10"),
	}))

	askDepth, err := ab.Depth(Sell, udecimal.MustParse("105"))
	require.NoError(t, err)
	require.Equal(t, udecimal.Zero, askDepth)

	depth := ab.GetDepth(10)
	require.Empty(t, depth.Asks)
}

func TestAggregatedBook_Replay_Amend(t *testing.T) {
	t.Run("amend size at same price", func(t *testing.T) {
		ab := NewAggregatedBook()

		require.NoError(t, ab.Replay(&OrderBookLog{
			SeqID: 1,
			Type:  protocol.LogTypeOpen,
			Side:  Buy,
			Price: udecimal.MustParse("100"),
			Size:  udecimal.MustParse("10"),
		}))

		// Amend increase size from 10 to 15
		require.NoError(t, ab.Replay(&OrderBookLog{
			SeqID:    2,
			Type:     protocol.LogTypeAmend,
			Side:     Buy,
			Price:    udecimal.MustParse("100"),
			Size:     udecimal.MustParse("15"),
			OldPrice: udecimal.MustParse("100"),
			OldSize:  udecimal.MustParse("10"),
		}))

		depth, err := ab.Depth(Buy, udecimal.MustParse("100"))
		require.NoError(t, err)
		require.Equal(t, udecimal.MustParse("15"), depth)

		// Amend decrease size from 15 to 6
		require.NoError(t, ab.Replay(&OrderBookLog{
			SeqID:    3,
			Type:     protocol.LogTypeAmend,
			Side:     Buy,
			Price:    udecimal.MustParse("100"),
			Size:     udecimal.MustParse("6"),
			OldPrice: udecimal.MustParse("100"),
			OldSize:  udecimal.MustParse("15"),
		}))

		depth, err = ab.Depth(Buy, udecimal.MustParse("100"))
		require.NoError(t, err)
		require.Equal(t, udecimal.MustParse("6"), depth)
	})

	t.Run("amend price moves order to new price level", func(t *testing.T) {
		ab := NewAggregatedBook()

		require.NoError(t, ab.Replay(&OrderBookLog{
			SeqID: 1,
			Type:  protocol.LogTypeOpen,
			Side:  Buy,
			Price: udecimal.MustParse("90"),
			Size:  udecimal.MustParse("5"),
		}))

		// Amend price from 90 (size 5) to 95 (size 8)
		require.NoError(t, ab.Replay(&OrderBookLog{
			SeqID:    2,
			Type:     protocol.LogTypeAmend,
			Side:     Buy,
			Price:    udecimal.MustParse("95"),
			Size:     udecimal.MustParse("8"),
			OldPrice: udecimal.MustParse("90"),
			OldSize:  udecimal.MustParse("5"),
		}))

		oldDepth, err := ab.Depth(Buy, udecimal.MustParse("90"))
		require.NoError(t, err)
		require.Equal(t, udecimal.Zero, oldDepth)

		newDepth, err := ab.Depth(Buy, udecimal.MustParse("95"))
		require.NoError(t, err)
		require.Equal(t, udecimal.MustParse("8"), newDepth)
	})
}

func TestAggregatedBook_Replay_DeduplicationAndGaps(t *testing.T) {
	t.Run("deduplication skips already processed sequence IDs", func(t *testing.T) {
		ab := NewAggregatedBook()
		snap := &Snapshot{
			SequenceID: 10,
			Bids: []*protocol.DepthItem{
				{Price: "100", Size: "20"},
			},
		}
		require.NoError(t, ab.ApplySnapshot(snap))

		// Older log (SeqID <= 10) should be ignored
		err := ab.Replay(&OrderBookLog{
			SeqID: 5,
			Type:  protocol.LogTypeOpen,
			Side:  Buy,
			Price: udecimal.MustParse("100"),
			Size:  udecimal.MustParse("10"),
		})
		require.NoError(t, err)
		require.Equal(t, uint64(10), ab.SequenceID())
		depth, _ := ab.Depth(Buy, udecimal.MustParse("100"))
		require.Equal(t, udecimal.MustParse("20"), depth)

		// Next log (SeqID == 11) should be applied
		err = ab.Replay(&OrderBookLog{
			SeqID: 11,
			Type:  protocol.LogTypeOpen,
			Side:  Buy,
			Price: udecimal.MustParse("100"),
			Size:  udecimal.MustParse("10"),
		})
		require.NoError(t, err)
		require.Equal(t, uint64(11), ab.SequenceID())
		depth, _ = ab.Depth(Buy, udecimal.MustParse("100"))
		require.Equal(t, udecimal.MustParse("30"), depth)

		// Duplicate log (SeqID == 11) should be ignored
		err = ab.Replay(&OrderBookLog{
			SeqID: 11,
			Type:  protocol.LogTypeOpen,
			Side:  Buy,
			Price: udecimal.MustParse("100"),
			Size:  udecimal.MustParse("10"),
		})
		require.NoError(t, err)
		require.Equal(t, uint64(11), ab.SequenceID())
		depth, _ = ab.Depth(Buy, udecimal.MustParse("100"))
		require.Equal(t, udecimal.MustParse("30"), depth)
	})

	t.Run("sequence gap without OnRebuild returns ErrSequenceGap", func(t *testing.T) {
		ab := NewAggregatedBook()
		snap := &Snapshot{
			SequenceID: 10,
		}
		require.NoError(t, ab.ApplySnapshot(snap))

		// Log with SeqID 15 when current is 10 (gap: expected 11)
		err := ab.Replay(&OrderBookLog{
			SeqID: 15,
			Type:  protocol.LogTypeOpen,
			Side:  Buy,
			Price: udecimal.MustParse("100"),
			Size:  udecimal.MustParse("10"),
		})
		require.ErrorIs(t, err, ErrSequenceGap)
		require.Equal(t, uint64(10), ab.SequenceID())
	})

	t.Run("sequence gap triggers OnRebuild and covers gap", func(t *testing.T) {
		ab := NewAggregatedBook()
		snap := &Snapshot{
			SequenceID: 10,
		}
		require.NoError(t, ab.ApplySnapshot(snap))

		rebuildCalled := false
		ab.OnRebuild = func() (*Snapshot, error) {
			rebuildCalled = true
			return &Snapshot{
				SequenceID: 20,
				Bids: []*protocol.DepthItem{
					{Price: "100", Size: "50"},
				},
			}, nil
		}

		// Incoming log SeqID 15 triggers rebuild, new snapshot SeqID is 20 (>= 15)
		err := ab.Replay(&OrderBookLog{
			SeqID: 15,
			Type:  protocol.LogTypeOpen,
			Side:  Buy,
			Price: udecimal.MustParse("100"),
			Size:  udecimal.MustParse("10"),
		})
		require.NoError(t, err)
		require.True(t, rebuildCalled)
		require.Equal(t, uint64(20), ab.SequenceID())
		depth, _ := ab.Depth(Buy, udecimal.MustParse("100"))
		require.Equal(t, udecimal.MustParse("50"), depth)
	})

	t.Run("sequence gap triggers OnRebuild and applies immediate next log", func(t *testing.T) {
		ab := NewAggregatedBook()
		snap := &Snapshot{
			SequenceID: 10,
		}
		require.NoError(t, ab.ApplySnapshot(snap))

		ab.OnRebuild = func() (*Snapshot, error) {
			return &Snapshot{
				SequenceID: 14,
				Bids: []*protocol.DepthItem{
					{Price: "100", Size: "40"},
				},
			}, nil
		}

		// Incoming log SeqID 15 triggers rebuild, snapshot is at 14, log 15 is then applied
		err := ab.Replay(&OrderBookLog{
			SeqID: 15,
			Type:  protocol.LogTypeOpen,
			Side:  Buy,
			Price: udecimal.MustParse("100"),
			Size:  udecimal.MustParse("10"),
		})
		require.NoError(t, err)
		require.Equal(t, uint64(15), ab.SequenceID())
		depth, _ := ab.Depth(Buy, udecimal.MustParse("100"))
		require.Equal(t, udecimal.MustParse("50"), depth)
	})

	t.Run("sequence gap triggers OnRebuild but gap remains returns ErrSequenceGap", func(t *testing.T) {
		ab := NewAggregatedBook()
		snap := &Snapshot{
			SequenceID: 10,
		}
		require.NoError(t, ab.ApplySnapshot(snap))

		ab.OnRebuild = func() (*Snapshot, error) {
			return &Snapshot{
				SequenceID: 12, // snapshot only caught up to 12
			}, nil
		}

		// Incoming log SeqID 25 triggers rebuild, but 25 > 12 + 1
		err := ab.Replay(&OrderBookLog{
			SeqID: 25,
			Type:  protocol.LogTypeOpen,
			Side:  Buy,
			Price: udecimal.MustParse("100"),
			Size:  udecimal.MustParse("10"),
		})
		require.ErrorIs(t, err, ErrSequenceGap)
	})
}

func TestAggregatedBook_EdgeCases(t *testing.T) {
	t.Run("nil snapshot returns ErrNilSnapshot", func(t *testing.T) {
		ab := NewAggregatedBook()
		err := ab.ApplySnapshot(nil)
		require.ErrorIs(t, err, ErrNilSnapshot)
	})

	t.Run("nil log in Replay returns error", func(t *testing.T) {
		ab := NewAggregatedBook()
		err := ab.Replay(nil)
		require.Error(t, err)
	})

	t.Run("Rebuild without callback returns ErrOnRebuildNotSet", func(t *testing.T) {
		ab := NewAggregatedBook()
		err := ab.Rebuild()
		require.ErrorIs(t, err, ErrOnRebuildNotSet)
	})

	t.Run("LogTypeReject updates seqID without modifying book", func(t *testing.T) {
		ab := NewAggregatedBook()
		require.NoError(t, ab.Replay(&OrderBookLog{
			SeqID: 1,
			Type:  protocol.LogTypeOpen,
			Side:  Buy,
			Price: udecimal.MustParse("100"),
			Size:  udecimal.MustParse("10"),
		}))

		require.NoError(t, ab.Replay(&OrderBookLog{
			SeqID: 2,
			Type:  protocol.LogTypeReject,
		}))

		require.Equal(t, uint64(2), ab.SequenceID())
		depth, err := ab.Depth(Buy, udecimal.MustParse("100"))
		require.NoError(t, err)
		require.Equal(t, udecimal.MustParse("10"), depth)
	})
}

func TestAggregatedBook_Concurrency_Race(t *testing.T) {
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
	numWriters := 2
	numReaders := 4

	for range numReaders {
		go func() {
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
		}()
	}

	for range numWriters {
		go func() {
			for seq := uint64(1); seq <= 200; seq++ {
				// Replay matching or open
				_ = ab.Replay(&OrderBookLog{
					SeqID: seq,
					Type:  protocol.LogTypeOpen,
					Side:  Buy,
					Price: udecimal.MustParse("100"),
					Size:  udecimal.MustParse("1"),
				})
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(done)
}

func BenchmarkAggregatedBook_Replay_Open(b *testing.B) {
	ab := NewAggregatedBook()
	price := udecimal.MustParse("100")
	size := udecimal.MustParse("1")

	b.ResetTimer()
	b.ReportAllocs()
	for i := range b.N {
		_ = ab.Replay(&OrderBookLog{
			SeqID: uint64(i + 1),
			Type:  protocol.LogTypeOpen,
			Side:  Buy,
			Price: price,
			Size:  size,
		})
	}
}

func BenchmarkAggregatedBook_GetDepth(b *testing.B) {
	ab := NewAggregatedBook()
	for i := range 50 {
		p := udecimal.MustFromInt64(int64(100+i), 0)
		_ = ab.Replay(&OrderBookLog{
			SeqID: uint64(i + 1),
			Type:  protocol.LogTypeOpen,
			Side:  Sell,
			Price: p,
			Size:  udecimal.MustFromInt64(10, 0),
		})
	}

	b.ReportAllocs()
	for b.Loop() {
		_ = ab.GetDepth(10)
	}
}
