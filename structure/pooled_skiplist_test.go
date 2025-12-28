package structure

import (
	"math/rand"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
)

func TestPooledSkiplist_BasicOperations(t *testing.T) {
	sl := NewPooledSkiplist(100, 42)

	// Test empty
	_, ok := sl.Min()
	assert.False(t, ok)
	assert.Equal(t, int32(0), sl.Count())

	// Insert
	inserted, err := sl.Insert(udecimal.MustFromInt64(100, 0))
	assert.NoError(t, err)
	assert.True(t, inserted)
	inserted, err = sl.Insert(udecimal.MustFromInt64(50, 0))
	assert.NoError(t, err)
	assert.True(t, inserted)
	inserted, err = sl.Insert(udecimal.MustFromInt64(150, 0))
	assert.NoError(t, err)
	assert.True(t, inserted)
	assert.Equal(t, int32(3), sl.Count())

	// Duplicate
	inserted, err = sl.Insert(udecimal.MustFromInt64(100, 0))
	assert.NoError(t, err)
	assert.False(t, inserted)

	// Contains
	assert.True(t, sl.Contains(udecimal.MustFromInt64(100, 0)))
	assert.True(t, sl.Contains(udecimal.MustFromInt64(50, 0)))
	assert.False(t, sl.Contains(udecimal.MustFromInt64(999, 0)))

	// Min
	min, ok := sl.Min()
	assert.True(t, ok)
	assert.True(t, min.Equal(udecimal.MustFromInt64(50, 0)))
}

func TestPooledSkiplist_Delete(t *testing.T) {
	sl := NewPooledSkiplist(100, 42)

	values := []int64{50, 25, 75, 10, 30, 60, 80}
	for _, v := range values {
		sl.MustInsert(udecimal.MustFromInt64(v, 0))
	}

	// Delete
	assert.True(t, sl.Delete(udecimal.MustFromInt64(10, 0)))
	assert.Equal(t, int32(6), sl.Count())
	assert.False(t, sl.Contains(udecimal.MustFromInt64(10, 0)))

	// Delete non-existent
	assert.False(t, sl.Delete(udecimal.MustFromInt64(999, 0)))
}

func TestPooledSkiplist_DeleteMin(t *testing.T) {
	sl := NewPooledSkiplist(100, 42)

	values := []int64{50, 25, 75, 10, 30}
	for _, v := range values {
		sl.MustInsert(udecimal.MustFromInt64(v, 0))
	}

	expected := []int64{10, 25, 30, 50, 75}
	for _, exp := range expected {
		min, ok := sl.DeleteMin()
		assert.True(t, ok)
		assert.True(t, min.Equal(udecimal.MustFromInt64(exp, 0)))
	}

	assert.Equal(t, int32(0), sl.Count())
}

func TestPooledSkiplist_OracleTest(t *testing.T) {
	sl := NewPooledSkiplist(10000, 42)
	oracle := make(map[int64]bool)

	rng := rand.New(rand.NewSource(42))

	for i := 0; i < 10000; i++ {
		price := rng.Int63n(1000)

		if rng.Intn(2) == 0 {
			sl.MustInsert(udecimal.MustFromInt64(price, 0))
			oracle[price] = true
		} else {
			sl.Delete(udecimal.MustFromInt64(price, 0))
			delete(oracle, price)
		}

		assert.Equal(t, int32(len(oracle)), sl.Count())
	}

	// Verify final state
	slSlice := sl.InOrderSlice()
	oracleSlice := make([]int64, 0, len(oracle))
	for k := range oracle {
		oracleSlice = append(oracleSlice, k)
	}
	sort.Slice(oracleSlice, func(i, j int) bool { return oracleSlice[i] < oracleSlice[j] })

	assert.Equal(t, len(oracleSlice), len(slSlice))
	for i := range oracleSlice {
		assert.True(t, slSlice[i].Equal(udecimal.MustFromInt64(oracleSlice[i], 0)))
	}
}

func TestPooledSkiplist_DynamicGrow(t *testing.T) {
	var growCount int32

	sl := NewPooledSkiplistWithOptions(10, 42, SkiplistOptions{
		OnGrow: func(oldCap, newCap int32) {
			atomic.AddInt32(&growCount, 1)
			t.Logf("Skiplist grew: %d -> %d", oldCap, newCap)
		},
	})

	// Insert more than initial capacity
	for i := int64(0); i < 100; i++ {
		inserted, err := sl.Insert(udecimal.MustFromInt64(i, 0))
		assert.NoError(t, err)
		assert.True(t, inserted)
	}

	assert.Equal(t, int32(100), sl.Count())
	assert.Greater(t, atomic.LoadInt32(&growCount), int32(0), "Should have grown at least once")
	t.Logf("Final capacity: %d, grow count: %d", sl.Capacity(), growCount)
}

func TestPooledSkiplist_MaxCapacity(t *testing.T) {
	sl := NewPooledSkiplistWithOptions(10, 42, SkiplistOptions{
		MaxCapacity: 20,
	})

	// Insert up to capacity
	for i := int64(0); i < 19; i++ { // 19 because head takes 1 slot
		inserted, err := sl.Insert(udecimal.MustFromInt64(i, 0))
		assert.NoError(t, err)
		assert.True(t, inserted)
	}

	// Next insert should fail
	_, err := sl.Insert(udecimal.MustFromInt64(999, 0))
	assert.ErrorIs(t, err, ErrMaxCapacityReached)
}

func TestPooledSkiplist_Iterator(t *testing.T) {
	sl := NewPooledSkiplist(100, 42)

	// Insert in random order
	values := []int64{50, 25, 75, 10, 30, 60, 80, 5, 15}
	for _, v := range values {
		sl.MustInsert(udecimal.MustFromInt64(v, 0))
	}

	// Iterate and verify sorted order
	expected := []int64{5, 10, 15, 25, 30, 50, 60, 75, 80}
	i := 0
	iter := sl.Iterator()
	for iter.Valid() {
		assert.True(t, iter.Price().Equal(udecimal.MustFromInt64(expected[i], 0)),
			"Expected %d at position %d, got %s", expected[i], i, iter.Price().String())
		i++
		iter.Next()
	}
	assert.Equal(t, len(expected), i)

	// Empty skiplist iterator
	sl2 := NewPooledSkiplist(10, 42)
	iter2 := sl2.Iterator()
	assert.False(t, iter2.Valid())
}

// Benchmarks
func BenchmarkPooledSkiplist_Insert(b *testing.B) {
	prices := make([]udecimal.Decimal, 1000)
	for i := 0; i < 1000; i++ {
		prices[i] = udecimal.MustFromInt64(int64(i), 0)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		sl := NewPooledSkiplist(1100, int64(i))
		for _, p := range prices {
			sl.MustInsert(p)
		}
	}
}

func BenchmarkPooledSkiplist_Delete(b *testing.B) {
	prices := make([]udecimal.Decimal, 1000)
	for i := 0; i < 1000; i++ {
		prices[i] = udecimal.MustFromInt64(int64(i), 0)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		sl := NewPooledSkiplist(1100, int64(i))
		for _, p := range prices {
			sl.MustInsert(p)
		}
		b.StartTimer()

		for j := 0; j < 500; j++ {
			sl.Delete(prices[j])
		}
	}
}

func BenchmarkPooledSkiplist_DeleteMin(b *testing.B) {
	prices := make([]udecimal.Decimal, 1000)
	for i := 0; i < 1000; i++ {
		prices[i] = udecimal.MustFromInt64(int64(i), 0)
	}

	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		sl := NewPooledSkiplist(1100, int64(i))
		for _, p := range prices {
			sl.MustInsert(p)
		}
		b.StartTimer()

		for sl.Count() > 0 {
			sl.DeleteMin()
		}
	}
}

func BenchmarkPooledSkiplist_Search(b *testing.B) {
	sl := NewPooledSkiplist(1100, 42)
	for i := 0; i < 1000; i++ {
		sl.MustInsert(udecimal.MustFromInt64(int64(i), 0))
	}

	target := udecimal.MustFromInt64(500, 0)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		sl.Contains(target)
	}
}

func BenchmarkPooledSkiplist_DynamicGrow(b *testing.B) {
	prices := make([]udecimal.Decimal, 1000)
	for i := 0; i < 1000; i++ {
		prices[i] = udecimal.MustFromInt64(int64(i), 0)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Start with small capacity to force growth
		sl := NewPooledSkiplist(100, int64(i))
		for _, p := range prices {
			sl.MustInsert(p)
		}
	}
}

// FuzzPooledSkiplist verifies skiplist invariants under random operations.
func FuzzPooledSkiplist(f *testing.F) {
	// Seed corpus
	f.Add([]byte{0, 1, 2, 3, 4, 5})
	f.Add([]byte{5, 4, 3, 2, 1, 0})
	f.Add([]byte{1, 1, 1, 1, 1})
	f.Add([]byte{0, 0, 0, 1, 1, 1})

	f.Fuzz(func(t *testing.T, data []byte) {
		sl := NewPooledSkiplist(1000, 42)
		oracle := make(map[int64]bool)

		for _, b := range data {
			price := int64(b % 100) // Limit range to increase collisions

			if b%2 == 0 {
				// Insert
				sl.MustInsert(udecimal.MustFromInt64(price, 0))
				oracle[price] = true
			} else {
				// Delete
				sl.Delete(udecimal.MustFromInt64(price, 0))
				delete(oracle, price)
			}
		}

		// Verify count
		if int32(len(oracle)) != sl.Count() {
			t.Errorf("Count mismatch: oracle=%d, skiplist=%d", len(oracle), sl.Count())
		}

		// Verify in-order traversal is sorted
		slice := sl.InOrderSlice()
		for i := 1; i < len(slice); i++ {
			if !slice[i-1].LessThan(slice[i]) {
				t.Errorf("Not sorted at index %d: %s >= %s", i, slice[i-1].String(), slice[i].String())
			}
		}

		// Verify all oracle elements exist in skiplist
		for price := range oracle {
			if !sl.Contains(udecimal.MustFromInt64(price, 0)) {
				t.Errorf("Missing price %d in skiplist", price)
			}
		}

		// Verify Min is correct
		if len(oracle) > 0 {
			minOracle := int64(1<<63 - 1)
			for k := range oracle {
				if k < minOracle {
					minOracle = k
				}
			}
			min, ok := sl.Min()
			if !ok {
				t.Errorf("Min() returned false but oracle has %d elements", len(oracle))
			}
			if !min.Equal(udecimal.MustFromInt64(minOracle, 0)) {
				t.Errorf("Min mismatch: skiplist=%s, oracle=%d", min.String(), minOracle)
			}
		}
	})
}
