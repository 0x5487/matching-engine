package structure

import (
	"math/rand"
	"sort"
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
)

func TestPriceLevelTree_BasicOperations(t *testing.T) {
	tree := NewPriceLevelTree(100)

	// Test empty tree
	_, ok := tree.Min()
	assert.False(t, ok)
	assert.Equal(t, int32(0), tree.Count())

	// Insert
	assert.True(t, tree.Insert(udecimal.MustFromInt64(100, 0)))
	assert.True(t, tree.Insert(udecimal.MustFromInt64(50, 0)))
	assert.True(t, tree.Insert(udecimal.MustFromInt64(150, 0)))
	assert.Equal(t, int32(3), tree.Count())

	// Duplicate insert should return false
	assert.False(t, tree.Insert(udecimal.MustFromInt64(100, 0)))
	assert.Equal(t, int32(3), tree.Count())

	// Contains
	assert.True(t, tree.Contains(udecimal.MustFromInt64(100, 0)))
	assert.True(t, tree.Contains(udecimal.MustFromInt64(50, 0)))
	assert.False(t, tree.Contains(udecimal.MustFromInt64(999, 0)))

	// Min
	min, ok := tree.Min()
	assert.True(t, ok)
	assert.True(t, min.Equal(udecimal.MustFromInt64(50, 0)))

	// Max
	max, ok := tree.Max()
	assert.True(t, ok)
	assert.True(t, max.Equal(udecimal.MustFromInt64(150, 0)))
}

func TestPriceLevelTree_Delete(t *testing.T) {
	tree := NewPriceLevelTree(100)

	// Insert values
	values := []int64{50, 25, 75, 10, 30, 60, 80}
	for _, v := range values {
		tree.Insert(udecimal.MustFromInt64(v, 0))
	}
	assert.Equal(t, int32(7), tree.Count())

	// Delete leaf
	assert.True(t, tree.Delete(udecimal.MustFromInt64(10, 0)))
	assert.Equal(t, int32(6), tree.Count())
	assert.False(t, tree.Contains(udecimal.MustFromInt64(10, 0)))

	// Delete node with one child
	assert.True(t, tree.Delete(udecimal.MustFromInt64(25, 0)))
	assert.Equal(t, int32(5), tree.Count())

	// Delete node with two children
	assert.True(t, tree.Delete(udecimal.MustFromInt64(75, 0)))
	assert.Equal(t, int32(4), tree.Count())

	// Delete root
	assert.True(t, tree.Delete(udecimal.MustFromInt64(50, 0)))
	assert.Equal(t, int32(3), tree.Count())

	// Delete non-existent
	assert.False(t, tree.Delete(udecimal.MustFromInt64(999, 0)))

	// Verify remaining structure
	assert.True(t, tree.Contains(udecimal.MustFromInt64(30, 0)))
	assert.True(t, tree.Contains(udecimal.MustFromInt64(60, 0)))
	assert.True(t, tree.Contains(udecimal.MustFromInt64(80, 0)))
}

func TestPriceLevelTree_DeleteMin(t *testing.T) {
	tree := NewPriceLevelTree(100)

	// Empty tree
	_, ok := tree.DeleteMin()
	assert.False(t, ok)

	// Insert and delete min repeatedly
	values := []int64{50, 25, 75, 10, 30}
	for _, v := range values {
		tree.Insert(udecimal.MustFromInt64(v, 0))
	}

	// Delete mins in order
	expected := []int64{10, 25, 30, 50, 75}
	for _, exp := range expected {
		min, ok := tree.DeleteMin()
		assert.True(t, ok)
		assert.True(t, min.Equal(udecimal.MustFromInt64(exp, 0)), "Expected %d, got %s", exp, min.String())
	}

	assert.Equal(t, int32(0), tree.Count())
}

func TestPriceLevelTree_Successor(t *testing.T) {
	tree := NewPriceLevelTree(100)

	values := []int64{50, 25, 75, 10, 30, 60, 80}
	for _, v := range values {
		tree.Insert(udecimal.MustFromInt64(v, 0))
	}

	// Test successors
	succ, ok := tree.Successor(udecimal.MustFromInt64(10, 0))
	assert.True(t, ok)
	assert.True(t, succ.Equal(udecimal.MustFromInt64(25, 0)))

	succ, ok = tree.Successor(udecimal.MustFromInt64(50, 0))
	assert.True(t, ok)
	assert.True(t, succ.Equal(udecimal.MustFromInt64(60, 0)))

	// No successor for max
	_, ok = tree.Successor(udecimal.MustFromInt64(80, 0))
	assert.False(t, ok)

	// Non-existent key
	_, ok = tree.Successor(udecimal.MustFromInt64(999, 0))
	assert.False(t, ok)
}

func TestPriceLevelTree_InOrderSlice(t *testing.T) {
	tree := NewPriceLevelTree(100)

	values := []int64{50, 25, 75, 10, 30, 60, 80, 5, 15, 27, 35}
	for _, v := range values {
		tree.Insert(udecimal.MustFromInt64(v, 0))
	}

	result := tree.InOrderSlice()
	assert.Equal(t, len(values), len(result))

	// Verify sorted order
	for i := 1; i < len(result); i++ {
		assert.True(t, result[i-1].LessThan(result[i]),
			"InOrder not sorted: %s >= %s", result[i-1].String(), result[i].String())
	}
}

func TestPriceLevelTree_OracleTest(t *testing.T) {
	tree := NewPriceLevelTree(10000)
	oracle := make(map[int64]bool)

	rng := rand.New(rand.NewSource(42))

	// Random insert/delete operations
	for i := 0; i < 10000; i++ {
		price := rng.Int63n(1000)

		if rng.Intn(2) == 0 {
			// Insert
			tree.Insert(udecimal.MustFromInt64(price, 0))
			oracle[price] = true
		} else {
			// Delete
			tree.Delete(udecimal.MustFromInt64(price, 0))
			delete(oracle, price)
		}

		// Verify count
		assert.Equal(t, int32(len(oracle)), tree.Count())

		// Verify min
		if len(oracle) > 0 {
			minOracle := int64(1<<63 - 1)
			for k := range oracle {
				if k < minOracle {
					minOracle = k
				}
			}
			treeMin, ok := tree.Min()
			assert.True(t, ok)
			assert.True(t, treeMin.Equal(udecimal.MustFromInt64(minOracle, 0)),
				"Min mismatch: tree=%s, oracle=%d", treeMin.String(), minOracle)
		}
	}

	// Verify final state
	treeSlice := tree.InOrderSlice()
	oracleSlice := make([]int64, 0, len(oracle))
	for k := range oracle {
		oracleSlice = append(oracleSlice, k)
	}
	sort.Slice(oracleSlice, func(i, j int) bool { return oracleSlice[i] < oracleSlice[j] })

	assert.Equal(t, len(oracleSlice), len(treeSlice))
	for i := range oracleSlice {
		assert.True(t, treeSlice[i].Equal(udecimal.MustFromInt64(oracleSlice[i], 0)))
	}
}

func TestPriceLevelTree_AscendingInsert(t *testing.T) {
	tree := NewPriceLevelTree(1000)

	// Insert ascending sequence (worst case for naive BST)
	for i := int64(1); i <= 100; i++ {
		tree.Insert(udecimal.MustFromInt64(i, 0))
	}

	assert.Equal(t, int32(100), tree.Count())

	// Verify order
	result := tree.InOrderSlice()
	for i := int64(1); i <= 100; i++ {
		assert.True(t, result[i-1].Equal(udecimal.MustFromInt64(i, 0)))
	}
}

func TestPriceLevelTree_DescendingInsert(t *testing.T) {
	tree := NewPriceLevelTree(1000)

	// Insert descending sequence
	for i := int64(100); i >= 1; i-- {
		tree.Insert(udecimal.MustFromInt64(i, 0))
	}

	assert.Equal(t, int32(100), tree.Count())

	min, _ := tree.Min()
	assert.True(t, min.Equal(udecimal.MustFromInt64(1, 0)))

	max, _ := tree.Max()
	assert.True(t, max.Equal(udecimal.MustFromInt64(100, 0)))
}

// Benchmark against current operations
func BenchmarkPriceLevelTree_Insert(b *testing.B) {
	prices := make([]udecimal.Decimal, 1000)
	for i := 0; i < 1000; i++ {
		prices[i] = udecimal.MustFromInt64(int64(i), 0)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tree := NewPriceLevelTree(1100)
		for _, p := range prices {
			tree.Insert(p)
		}
	}
}

func BenchmarkPriceLevelTree_Search(b *testing.B) {
	tree := NewPriceLevelTree(10000)
	for i := int64(0); i < 1000; i++ {
		tree.Insert(udecimal.MustFromInt64(i, 0))
	}
	searchTarget := udecimal.MustFromInt64(500, 0)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Do 1000 searches per op to normalize units
		for j := 0; j < 1000; j++ {
			tree.Contains(searchTarget)
		}
	}
}

func BenchmarkPriceLevelTree_DeleteMin(b *testing.B) {
	// Create and populate tree for each iteration
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		tree := NewPriceLevelTree(1100)
		for j := int64(0); j < 1000; j++ {
			tree.Insert(udecimal.MustFromInt64(j, 0))
		}
		b.StartTimer()

		// Delete all mins
		for tree.Count() > 0 {
			tree.DeleteMin()
		}
	}
}

// FuzzPriceLevelTree verifies tree invariants under random operations.
func FuzzPriceLevelTree(f *testing.F) {
	// Seed corpus
	f.Add([]byte{0, 1, 2, 3, 4, 5})
	f.Add([]byte{5, 4, 3, 2, 1, 0})
	f.Add([]byte{1, 1, 1, 1, 1})

	f.Fuzz(func(t *testing.T, data []byte) {
		tree := NewPriceLevelTree(1000)
		oracle := make(map[int64]bool)

		for _, b := range data {
			price := int64(b % 100) // Limit range to increase collisions

			if b%2 == 0 {
				// Insert
				tree.Insert(udecimal.MustFromInt64(price, 0))
				oracle[price] = true
			} else {
				// Delete
				tree.Delete(udecimal.MustFromInt64(price, 0))
				delete(oracle, price)
			}
		}

		// Verify count
		if int32(len(oracle)) != tree.Count() {
			t.Errorf("Count mismatch: oracle=%d, tree=%d", len(oracle), tree.Count())
		}

		// Verify in-order traversal is sorted
		slice := tree.InOrderSlice()
		for i := 1; i < len(slice); i++ {
			if !slice[i-1].LessThan(slice[i]) {
				t.Errorf("Not sorted at index %d: %s >= %s", i, slice[i-1].String(), slice[i].String())
			}
		}

		// Verify all oracle elements exist in tree
		for price := range oracle {
			if !tree.Contains(udecimal.MustFromInt64(price, 0)) {
				t.Errorf("Missing price %d in tree", price)
			}
		}
	})
}
