package structure

import (
	"errors"
	"math/rand"

	"github.com/quagmt/udecimal"
)

// PooledSkiplist implements a fixed-level skiplist with arena-based memory management.
// This provides O(log N) operations with zero allocations on the hot path.
//
// Design:
// - All nodes have fixed MaxLevel pointers (wastes some memory but enables pooling)
// - Node arena is pre-allocated with automatic expansion when exhausted
// - Uses random level generation for probabilistic balancing

const (
	SkiplistMaxLevel    = 16 // Maximum level height
	SkiplistP           = 4  // 1/P probability of level increase
	DefaultGrowthFactor = 2  // Default expansion factor
)

var (
	ErrMaxCapacityReached = errors.New("skiplist: max capacity reached")
)

// SkiplistNode represents a node in the pooled skiplist.
type SkiplistNode struct {
	Forward [SkiplistMaxLevel]int32 // Forward pointers (fixed size for pooling)
	Price   udecimal.Decimal        // Key
	Level   int32                   // Actual level of this node (1 to MaxLevel)
}

// SkiplistOptions configures the pooled skiplist behavior.
type SkiplistOptions struct {
	// MaxCapacity sets the maximum number of nodes allowed.
	// If 0 (default), there is no limit and the skiplist will grow indefinitely.
	MaxCapacity int32

	// OnGrow is called when the skiplist expands.
	// Can be used for logging or metrics.
	OnGrow func(oldCap, newCap int32)
}

// PooledSkiplist is an arena-backed skiplist for price levels.
type PooledSkiplist struct {
	nodes       []SkiplistNode     // Pre-allocated node arena
	head        int32              // Head sentinel node index
	freeHead    int32              // Head of free list
	count       int32              // Number of nodes in list
	level       int32              // Current max level in use
	rng         *rand.Rand         // Random number generator
	maxCapacity int32              // Max capacity (0 = unlimited)
	onGrow      func(int32, int32) // Callback on grow
}

// NewPooledSkiplist creates a new pooled skiplist with pre-allocated capacity.
func NewPooledSkiplist(capacity int32, seed int64) *PooledSkiplist {
	return NewPooledSkiplistWithOptions(capacity, seed, SkiplistOptions{})
}

// NewPooledSkiplistWithOptions creates a new pooled skiplist with custom options.
func NewPooledSkiplistWithOptions(capacity int32, seed int64, opts SkiplistOptions) *PooledSkiplist {
	// +1 for head sentinel
	totalCap := capacity + 1
	sl := &PooledSkiplist{
		nodes:       make([]SkiplistNode, totalCap),
		freeHead:    1, // 0 is reserved for head
		count:       0,
		level:       1,
		rng:         rand.New(rand.NewSource(seed)),
		maxCapacity: opts.MaxCapacity,
		onGrow:      opts.OnGrow,
	}

	// Initialize head sentinel at index 0
	sl.head = 0
	sl.nodes[0].Level = SkiplistMaxLevel
	for i := 0; i < SkiplistMaxLevel; i++ {
		sl.nodes[0].Forward[i] = NullIndex
	}

	// Initialize free list (starting from index 1)
	for i := int32(1); i < totalCap-1; i++ {
		sl.nodes[i].Forward[0] = i + 1
	}
	sl.nodes[totalCap-1].Forward[0] = NullIndex

	return sl
}

// grow expands the arena capacity.
// Returns error if max capacity would be exceeded.
func (sl *PooledSkiplist) grow() error {
	oldCap := int32(len(sl.nodes))
	newCap := oldCap * DefaultGrowthFactor

	// Check max capacity limit
	if sl.maxCapacity > 0 && newCap > sl.maxCapacity {
		// Try to grow up to max capacity
		if oldCap >= sl.maxCapacity {
			return ErrMaxCapacityReached
		}
		newCap = sl.maxCapacity
	}

	// Notify callback
	if sl.onGrow != nil {
		sl.onGrow(oldCap, newCap)
	}

	// Create new slice and copy old data
	newNodes := make([]SkiplistNode, newCap)
	copy(newNodes, sl.nodes)

	// Initialize new nodes as free list
	for i := oldCap; i < newCap-1; i++ {
		newNodes[i].Forward[0] = i + 1
	}
	// Connect end of new free list to existing free list (if any)
	newNodes[newCap-1].Forward[0] = sl.freeHead
	sl.freeHead = oldCap // New free list head is first new node

	sl.nodes = newNodes
	return nil
}

// alloc allocates a node from the free list, growing if necessary.
func (sl *PooledSkiplist) alloc() (int32, error) {
	if sl.freeHead == NullIndex {
		if err := sl.grow(); err != nil {
			return NullIndex, err
		}
	}
	idx := sl.freeHead
	sl.freeHead = sl.nodes[idx].Forward[0]

	// Reset forward pointers
	for i := 0; i < SkiplistMaxLevel; i++ {
		sl.nodes[idx].Forward[i] = NullIndex
	}
	return idx, nil
}

// free returns a node to the free list.
func (sl *PooledSkiplist) free(idx int32) {
	sl.nodes[idx].Forward[0] = sl.freeHead
	sl.freeHead = idx
}

// randomLevel generates a random level for a new node.
func (sl *PooledSkiplist) randomLevel() int32 {
	level := int32(1)
	for level < SkiplistMaxLevel && sl.rng.Intn(SkiplistP) == 0 {
		level++
	}
	return level
}

// Insert inserts a price into the skiplist.
// Returns true if inserted, false if already exists.
// Returns error if max capacity is reached.
func (sl *PooledSkiplist) Insert(price udecimal.Decimal) (bool, error) {
	var update [SkiplistMaxLevel]int32
	x := sl.head

	// Find position and track update path
	for i := sl.level - 1; i >= 0; i-- {
		for sl.nodes[x].Forward[i] != NullIndex &&
			sl.nodes[sl.nodes[x].Forward[i]].Price.LessThan(price) {
			x = sl.nodes[x].Forward[i]
		}
		update[i] = x
	}

	x = sl.nodes[x].Forward[0]

	// Check if already exists
	if x != NullIndex && sl.nodes[x].Price.Equal(price) {
		return false, nil
	}

	// Generate random level
	newLevel := sl.randomLevel()
	if newLevel > sl.level {
		for i := sl.level; i < newLevel; i++ {
			update[i] = sl.head
		}
		sl.level = newLevel
	}

	// Allocate new node (may trigger grow)
	newNode, err := sl.alloc()
	if err != nil {
		return false, err
	}
	sl.nodes[newNode].Price = price
	sl.nodes[newNode].Level = newLevel

	for i := int32(0); i < newLevel; i++ {
		sl.nodes[newNode].Forward[i] = sl.nodes[update[i]].Forward[i]
		sl.nodes[update[i]].Forward[i] = newNode
	}

	sl.count++
	return true, nil
}

// MustInsert is like Insert but panics on error.
// Use only when you're certain capacity won't be exceeded.
func (sl *PooledSkiplist) MustInsert(price udecimal.Decimal) bool {
	inserted, err := sl.Insert(price)
	if err != nil {
		panic(err)
	}
	return inserted
}

// Contains checks if a price exists in the skiplist.
func (sl *PooledSkiplist) Contains(price udecimal.Decimal) bool {
	x := sl.head

	for i := sl.level - 1; i >= 0; i-- {
		for sl.nodes[x].Forward[i] != NullIndex &&
			sl.nodes[sl.nodes[x].Forward[i]].Price.LessThan(price) {
			x = sl.nodes[x].Forward[i]
		}
	}

	x = sl.nodes[x].Forward[0]
	return x != NullIndex && sl.nodes[x].Price.Equal(price)
}

// Delete removes a price from the skiplist.
// Returns true if deleted, false if not found.
func (sl *PooledSkiplist) Delete(price udecimal.Decimal) bool {
	var update [SkiplistMaxLevel]int32
	x := sl.head

	// Find position and track update path
	for i := sl.level - 1; i >= 0; i-- {
		for sl.nodes[x].Forward[i] != NullIndex &&
			sl.nodes[sl.nodes[x].Forward[i]].Price.LessThan(price) {
			x = sl.nodes[x].Forward[i]
		}
		update[i] = x
	}

	x = sl.nodes[x].Forward[0]

	// Check if found
	if x == NullIndex || !sl.nodes[x].Price.Equal(price) {
		return false
	}

	// Update forward pointers
	for i := int32(0); i < sl.level; i++ {
		if sl.nodes[update[i]].Forward[i] != x {
			break
		}
		sl.nodes[update[i]].Forward[i] = sl.nodes[x].Forward[i]
	}

	// Free the node
	sl.free(x)

	// Update list level
	for sl.level > 1 && sl.nodes[sl.head].Forward[sl.level-1] == NullIndex {
		sl.level--
	}

	sl.count--
	return true
}

// Min returns the minimum price in the skiplist.
func (sl *PooledSkiplist) Min() (udecimal.Decimal, bool) {
	x := sl.nodes[sl.head].Forward[0]
	if x == NullIndex {
		return udecimal.Zero, false
	}
	return sl.nodes[x].Price, true
}

// DeleteMin removes and returns the minimum price.
func (sl *PooledSkiplist) DeleteMin() (udecimal.Decimal, bool) {
	x := sl.nodes[sl.head].Forward[0]
	if x == NullIndex {
		return udecimal.Zero, false
	}

	price := sl.nodes[x].Price

	// Update forward pointers from head
	for i := int32(0); i < sl.level; i++ {
		if sl.nodes[sl.head].Forward[i] != x {
			break
		}
		sl.nodes[sl.head].Forward[i] = sl.nodes[x].Forward[i]
	}

	// Free the node
	sl.free(x)

	// Update list level
	for sl.level > 1 && sl.nodes[sl.head].Forward[sl.level-1] == NullIndex {
		sl.level--
	}

	sl.count--
	return price, true
}

// Count returns the number of nodes.
func (sl *PooledSkiplist) Count() int32 {
	return sl.count
}

// Capacity returns the current capacity of the arena.
func (sl *PooledSkiplist) Capacity() int32 {
	return int32(len(sl.nodes)) - 1 // -1 for head sentinel
}

// InOrderSlice returns all prices in sorted order.
func (sl *PooledSkiplist) InOrderSlice() []udecimal.Decimal {
	result := make([]udecimal.Decimal, 0, sl.count)
	x := sl.nodes[sl.head].Forward[0]
	for x != NullIndex {
		result = append(result, sl.nodes[x].Price)
		x = sl.nodes[x].Forward[0]
	}
	return result
}

// SkiplistIterator provides ordered traversal over the skiplist.
// Usage:
//
//	iter := sl.Iterator()
//	for iter.Valid() {
//	    price := iter.Price()
//	    // ...
//	    iter.Next()
//	}
type SkiplistIterator struct {
	sl      *PooledSkiplist
	current int32
}

// Iterator returns an iterator positioned at the first (minimum) element.
func (sl *PooledSkiplist) Iterator() *SkiplistIterator {
	return &SkiplistIterator{
		sl:      sl,
		current: sl.nodes[sl.head].Forward[0],
	}
}

// Valid returns true if the iterator points to a valid element.
func (it *SkiplistIterator) Valid() bool {
	return it.current != NullIndex
}

// Next advances the iterator to the next element.
func (it *SkiplistIterator) Next() {
	if it.current != NullIndex {
		it.current = it.sl.nodes[it.current].Forward[0]
	}
}

// Price returns the price at the current iterator position.
// Only valid when Valid() returns true.
func (it *SkiplistIterator) Price() udecimal.Decimal {
	return it.sl.nodes[it.current].Price
}
