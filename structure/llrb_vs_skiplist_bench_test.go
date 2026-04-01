package structure

import (
	"testing"

	"github.com/quagmt/udecimal"
)

// Comparative benchmarks: LLRB Tree vs Skiplist
// These benchmarks simulate matching engine scenarios:
// 1. Insert: Adding new price levels
// 2. Search: Looking up a specific price
// 3. Delete: Removing price levels after full execution
// 4. DeleteMin: Iterating from best price (critical for matching)

const benchSize = 1000 // Simulating 1000 price levels

// ============= INSERT BENCHMARKS =============

func BenchmarkCompare_Insert_LLRB(b *testing.B) {
	prices := make([]udecimal.Decimal, benchSize)
	for i := range benchSize {
		prices[i] = udecimal.MustFromInt64(int64(i), 0)
	}

	b.ReportAllocs()

	for b.Loop() {
		tree := NewPriceLevelTree(int32(benchSize + 100))
		for _, p := range prices {
			tree.Insert(p)
		}
	}
}

// ============= SEARCH BENCHMARKS =============

func BenchmarkCompare_Search_LLRB(b *testing.B) {
	tree := NewPriceLevelTree(int32(benchSize + 100))
	for i := range benchSize {
		tree.Insert(udecimal.MustFromInt64(int64(i), 0))
	}

	target := udecimal.MustFromInt64(500, 0)

	b.ReportAllocs()

	for b.Loop() {
		tree.Contains(target)
	}
}

// ============= DELETE BENCHMARKS =============

func BenchmarkCompare_Delete_LLRB(b *testing.B) {
	prices := make([]udecimal.Decimal, benchSize)
	for i := range benchSize {
		prices[i] = udecimal.MustFromInt64(int64(i), 0)
	}

	b.ReportAllocs()

	for b.Loop() {
		b.StopTimer()
		tree := NewPriceLevelTree(int32(benchSize + 100))
		for _, p := range prices {
			tree.Insert(p)
		}
		b.StartTimer()

		// Delete half the elements (simulating partial execution)
		for j := range benchSize / 2 {
			tree.Delete(prices[j])
		}
	}
}

// ============= DELETE MIN BENCHMARKS (Critical for matching) =============

func BenchmarkCompare_DeleteMin_LLRB(b *testing.B) {
	prices := make([]udecimal.Decimal, benchSize)
	for i := range benchSize {
		prices[i] = udecimal.MustFromInt64(int64(i), 0)
	}

	b.ReportAllocs()

	for b.Loop() {
		b.StopTimer()
		tree := NewPriceLevelTree(int32(benchSize + 100))
		for _, p := range prices {
			tree.Insert(p)
		}
		b.StartTimer()

		// Delete all elements from min (simulating order book drain)
		for tree.Count() > 0 {
			tree.DeleteMin()
		}
	}
}

// ============= MIXED WORKLOAD (Realistic Matching Scenario) =============
// Simulates: Insert new orders, search for price levels, delete executed orders

func BenchmarkCompare_MixedWorkload_LLRB(b *testing.B) {
	// Pre-compute prices
	prices := make([]udecimal.Decimal, benchSize)
	for i := range benchSize {
		prices[i] = udecimal.MustFromInt64(int64(i), 0)
	}

	b.ReportAllocs()

	for b.Loop() {
		tree := NewPriceLevelTree(int32(benchSize + 100))

		// Phase 1: Build order book (insert all)
		for _, p := range prices {
			tree.Insert(p)
		}

		// Phase 2: Matching simulation (search + deleteMin cycle)
		for j := range 100 {
			tree.Contains(prices[j%benchSize])
			if tree.Count() > 0 {
				tree.DeleteMin()
			}
		}

		// Phase 3: Cancel orders (random deletes)
		for j := benchSize / 2; j < benchSize; j++ {
			tree.Delete(prices[j])
		}
	}
}

// ============= POOLED SKIPLIST BENCHMARKS =============

func BenchmarkCompare_Insert_PooledSkiplist(b *testing.B) {
	prices := make([]udecimal.Decimal, benchSize)
	for i := range benchSize {
		prices[i] = udecimal.MustFromInt64(int64(i), 0)
	}

	b.ReportAllocs()

	for i := 0; b.Loop(); i++ {
		sl := NewPooledSkiplist(int32(benchSize+100), int64(i))
		for _, p := range prices {
			sl.MustInsert(p)
		}
	}
}

func BenchmarkCompare_Search_PooledSkiplist(b *testing.B) {
	sl := NewPooledSkiplist(int32(benchSize+100), 42)
	for i := range benchSize {
		_, _ = sl.Insert(udecimal.MustFromInt64(int64(i), 0))
	}

	target := udecimal.MustFromInt64(500, 0)

	b.ReportAllocs()

	for b.Loop() {
		sl.Contains(target)
	}
}

func BenchmarkCompare_Delete_PooledSkiplist(b *testing.B) {
	prices := make([]udecimal.Decimal, benchSize)
	for i := range benchSize {
		prices[i] = udecimal.MustFromInt64(int64(i), 0)
	}

	b.ReportAllocs()

	for i := 0; b.Loop(); i++ {
		b.StopTimer()
		sl := NewPooledSkiplist(int32(benchSize+100), int64(i))
		for _, p := range prices {
			sl.MustInsert(p)
		}
		b.StartTimer()

		for j := range benchSize / 2 {
			sl.Delete(prices[j])
		}
	}
}

func BenchmarkCompare_DeleteMin_PooledSkiplist(b *testing.B) {
	prices := make([]udecimal.Decimal, benchSize)
	for i := range benchSize {
		prices[i] = udecimal.MustFromInt64(int64(i), 0)
	}

	b.ReportAllocs()

	for i := 0; b.Loop(); i++ {
		b.StopTimer()
		sl := NewPooledSkiplist(int32(benchSize+100), int64(i))
		for _, p := range prices {
			sl.MustInsert(p)
		}
		b.StartTimer()

		for sl.Count() > 0 {
			sl.DeleteMin()
		}
	}
}

func BenchmarkCompare_MixedWorkload_PooledSkiplist(b *testing.B) {
	prices := make([]udecimal.Decimal, benchSize)
	for i := range benchSize {
		prices[i] = udecimal.MustFromInt64(int64(i), 0)
	}

	b.ReportAllocs()

	for i := 0; b.Loop(); i++ {
		sl := NewPooledSkiplist(int32(benchSize+100), int64(i))

		// Phase 1: Build order book (insert all)
		for _, p := range prices {
			sl.MustInsert(p)
		}

		// Phase 2: Matching simulation (search + deleteMin cycle)
		for j := range 100 {
			sl.Contains(prices[j%benchSize])
			if sl.Count() > 0 {
				sl.DeleteMin()
			}
		}

		// Phase 3: Cancel orders (random deletes)
		for j := benchSize / 2; j < benchSize; j++ {
			sl.Delete(prices[j])
		}
	}
}
