package match

import (
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/require"
)

// TestOrderBookLevelSizeAndBestPrice covers the two query methods that replace
// the depth view's second copy of the book: LevelSize reports the visible size
// at one price, and BestPrice the best price on a side.
func TestOrderBookLevelSizeAndBestPrice(t *testing.T) {
	t.Parallel()

	t.Run("empty book has no best price and no size", func(t *testing.T) {
		t.Parallel()
		book := NewOrderBook(dlMarket)
		for _, side := range []Side{Buy, Sell} {
			_, ok := book.BestPrice(side)
			require.False(t, ok)
			require.True(t, book.LevelSize(side, udecimal.MustParse("100")).IsZero())
		}
	})

	t.Run("absent price is zero while the other side and price are sized", func(t *testing.T) {
		t.Parallel()
		h := newDLHarness(t)
		h.place("a1", Sell, 101, 5, 0)
		h.place("b1", Buy, 99, 7, 0)
		require.True(t, h.book.LevelSize(Sell, udecimal.MustParse("100")).IsZero())
		require.True(t, h.book.LevelSize(Buy, udecimal.MustParse("101")).IsZero(),
			"the bid side has no level at 101")
		require.Equal(t, "5", h.book.LevelSize(Sell, udecimal.MustParse("101")).String())
		require.Equal(t, "7", h.book.LevelSize(Buy, udecimal.MustParse("99")).String())
	})

	t.Run("best price is the top of each side and moves after a cancel", func(t *testing.T) {
		t.Parallel()
		h := newDLHarness(t)
		h.place("a1", Sell, 103, 1, 0)
		h.place("a2", Sell, 101, 1, 0)
		h.place("b1", Buy, 97, 1, 0)
		h.place("b2", Buy, 98, 1, 0)

		ask, ok := h.book.BestPrice(Sell)
		require.True(t, ok)
		require.Equal(t, "101", ask.String(), "lowest ask is best")
		bid, ok := h.book.BestPrice(Buy)
		require.True(t, ok)
		require.Equal(t, "98", bid.String(), "highest bid is best")

		h.cancel("a2")
		ask, ok = h.book.BestPrice(Sell)
		require.True(t, ok)
		require.Equal(t, "103", ask.String())
	})

	t.Run("level size is visible size only", func(t *testing.T) {
		t.Parallel()
		h := newDLHarness(t)
		h.place("ice", Sell, 101, 10, 2)
		require.Equal(t, "2", h.book.LevelSize(Sell, udecimal.MustParse("101")).String(),
			"the hidden 8 is not depth")
	})

	t.Run("a level that empties reads zero and a reopened price reads only its new order", func(t *testing.T) {
		t.Parallel()
		h := newDLHarness(t)
		h.place("old", Buy, 100, 9, 0)
		h.cancel("old")
		require.True(t, h.book.LevelSize(Buy, udecimal.MustParse("100")).IsZero())
		_, ok := h.book.BestPrice(Buy)
		require.False(t, ok)

		h.place("new", Buy, 100, 4, 0)
		require.Equal(t, "4", h.book.LevelSize(Buy, udecimal.MustParse("100")).String(),
			"the reused price unit must not carry the old total")
		best, ok := h.book.BestPrice(Buy)
		require.True(t, ok)
		require.Equal(t, "100", best.String())
	})
}

// TestOrderBookLevelQueriesAllocateNothing pins the zero-allocation contract of
// LevelSize and BestPrice.
func TestOrderBookLevelQueriesAllocateNothing(t *testing.T) {
	h := newDLHarness(t)
	h.place("a", Sell, 101, 5, 0)
	h.place("b", Buy, 99, 5, 0)
	price := udecimal.MustParse("101")

	require.Zero(t, testing.AllocsPerRun(1000, func() {
		_ = h.book.LevelSize(Sell, price)
	}), "LevelSize must not allocate")
	require.Zero(t, testing.AllocsPerRun(1000, func() {
		_, _ = h.book.BestPrice(Buy)
	}), "BestPrice must not allocate")
}

// TestPriceUnitReuseStartsClean drives the price unit pool directly. A recycled
// unit must come back with no total, count or hidden size left from its previous
// level, even though a unit that came back through releasePriceUnit is already
// zeroed. The dirty unit is put into the pool without releasePriceUnit, so the
// reset that is checked is the one acquirePriceUnit makes. A pool that drops a Put
// simply hands out a new unit, which the loop tolerates.
func TestPriceUnitReuseStartsClean(t *testing.T) {
	t.Parallel()

	order := &Order{
		ID: "fresh", Side: Buy, Price: udecimal.MustParse("100"),
		Size: udecimal.MustParse("3"), HiddenSize: udecimal.MustParse("2"), Type: Limit, UserID: 1,
	}
	for range 64 {
		dirty := &priceUnit{
			totalSize:  udecimal.MustParse("777"),
			hiddenSize: udecimal.MustParse("888"),
			count:      9,
			head:       order,
			tail:       order,
		}
		priceUnitPool.Put(dirty)

		unit := acquirePriceUnit(order)
		require.Equal(t, "3", unit.totalSize.String(), "total is the new order's visible size")
		require.Equal(t, "2", unit.hiddenSize.String(), "hidden is the new order's hidden size")
		require.Equal(t, int64(1), unit.count)
		require.Same(t, order, unit.head)
		require.Same(t, order, unit.tail)
		releasePriceUnit(unit)
	}
}
