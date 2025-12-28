package structure

import (
	"github.com/quagmt/udecimal"
)

// LLRB Tree implementation with arena-based memory management.
// This is a Left-Leaning Red-Black Tree optimized for zero-allocation operations
// in a high-frequency trading matching engine.
//
// Design Goals:
// 1. Zero allocation on hot path (insert/delete/search)
// 2. O(log N) worst-case performance guarantee
// 3. Efficient Min/Max and Successor operations for order book iteration
//
// Reference: Robert Sedgewick's LLRB implementation
// https://sedgewick.io/wp-content/themes/flavor/uploads/2016/02/LLRB.pdf

const (
	NullIndex  int32 = -1
	colorRed         = true
	colorBlack       = false
)

// PriceLevel represents a node in the LLRB tree.
// Each node corresponds to a price level in the order book.
type PriceLevel struct {
	Left   int32            // Left child index
	Right  int32            // Right child index
	Parent int32            // Parent index (for efficient successor traversal)
	Color  bool             // true = Red, false = Black
	Price  udecimal.Decimal // Key: price level
	// Orders can be added here when integrating with order book
	// For now, we just track the price for the tree structure
}

// PriceLevelTree is an arena-backed LLRB tree for price levels.
type PriceLevelTree struct {
	nodes    []PriceLevel // Pre-allocated node arena
	root     int32        // Root node index
	freeHead int32        // Head of free list
	count    int32        // Number of nodes in tree
	minCache int32        // Cached minimum node index
}

// NewPriceLevelTree creates a new LLRB tree with pre-allocated capacity.
func NewPriceLevelTree(capacity int32) *PriceLevelTree {
	tree := &PriceLevelTree{
		nodes:    make([]PriceLevel, capacity),
		root:     NullIndex,
		freeHead: 0,
		count:    0,
		minCache: NullIndex,
	}
	// Initialize free list using Left pointer
	for i := int32(0); i < capacity-1; i++ {
		tree.nodes[i].Left = i + 1
	}
	tree.nodes[capacity-1].Left = NullIndex
	return tree
}

// alloc allocates a node from the free list.
func (t *PriceLevelTree) alloc() int32 {
	if t.freeHead == NullIndex {
		// Arena exhausted - in production, this should trigger an alert
		panic("PriceLevelTree: arena exhausted")
	}
	idx := t.freeHead
	t.freeHead = t.nodes[idx].Left
	// Reset node
	t.nodes[idx] = PriceLevel{
		Left:   NullIndex,
		Right:  NullIndex,
		Parent: NullIndex,
		Color:  colorRed, // New nodes are always red in LLRB
	}
	return idx
}

// free returns a node to the free list.
func (t *PriceLevelTree) free(idx int32) {
	t.nodes[idx].Left = t.freeHead
	t.freeHead = idx
}

// isRed checks if a node is red (nil nodes are black).
func (t *PriceLevelTree) isRed(idx int32) bool {
	if idx == NullIndex {
		return false
	}
	return t.nodes[idx].Color == colorRed
}

// rotateLeft performs a left rotation.
//
//	  |              |
//	  h              x
//	 / \    =>      / \
//	a   x          h   c
//	   / \        / \
//	  b   c      a   b
func (t *PriceLevelTree) rotateLeft(h int32) int32 {
	x := t.nodes[h].Right
	t.nodes[h].Right = t.nodes[x].Left
	if t.nodes[x].Left != NullIndex {
		t.nodes[t.nodes[x].Left].Parent = h
	}
	t.nodes[x].Left = h
	t.nodes[x].Color = t.nodes[h].Color
	t.nodes[h].Color = colorRed
	// Update parent pointers
	t.nodes[x].Parent = t.nodes[h].Parent
	t.nodes[h].Parent = x
	return x
}

// rotateRight performs a right rotation.
//
//	    |          |
//	    h          x
//	   / \   =>   / \
//	  x   c      a   h
//	 / \            / \
//	a   b          b   c
func (t *PriceLevelTree) rotateRight(h int32) int32 {
	x := t.nodes[h].Left
	t.nodes[h].Left = t.nodes[x].Right
	if t.nodes[x].Right != NullIndex {
		t.nodes[t.nodes[x].Right].Parent = h
	}
	t.nodes[x].Right = h
	t.nodes[x].Color = t.nodes[h].Color
	t.nodes[h].Color = colorRed
	// Update parent pointers
	t.nodes[x].Parent = t.nodes[h].Parent
	t.nodes[h].Parent = x
	return x
}

// flipColors flips the colors of a node and its children.
func (t *PriceLevelTree) flipColors(h int32) {
	t.nodes[h].Color = !t.nodes[h].Color
	t.nodes[t.nodes[h].Left].Color = !t.nodes[t.nodes[h].Left].Color
	t.nodes[t.nodes[h].Right].Color = !t.nodes[t.nodes[h].Right].Color
}

// Insert inserts a price into the tree.
// Returns true if the price was newly inserted, false if it already existed.
func (t *PriceLevelTree) Insert(price udecimal.Decimal) bool {
	var inserted bool
	t.root, inserted = t.insert(t.root, NullIndex, price)
	t.nodes[t.root].Color = colorBlack // Root is always black
	if inserted {
		t.count++
		// Update min cache
		if t.minCache == NullIndex || price.LessThan(t.nodes[t.minCache].Price) {
			t.minCache = t.findMin(t.root)
		}
	}
	return inserted
}

func (t *PriceLevelTree) insert(h int32, parent int32, price udecimal.Decimal) (int32, bool) {
	if h == NullIndex {
		idx := t.alloc()
		t.nodes[idx].Price = price
		t.nodes[idx].Parent = parent
		return idx, true
	}

	var inserted bool
	cmp := price.Cmp(t.nodes[h].Price)
	if cmp < 0 {
		t.nodes[h].Left, inserted = t.insert(t.nodes[h].Left, h, price)
	} else if cmp > 0 {
		t.nodes[h].Right, inserted = t.insert(t.nodes[h].Right, h, price)
	} else {
		// Key already exists
		return h, false
	}

	// Fix up LLRB invariants
	if t.isRed(t.nodes[h].Right) && !t.isRed(t.nodes[h].Left) {
		h = t.rotateLeft(h)
	}
	if t.isRed(t.nodes[h].Left) && t.isRed(t.nodes[t.nodes[h].Left].Left) {
		h = t.rotateRight(h)
	}
	if t.isRed(t.nodes[h].Left) && t.isRed(t.nodes[h].Right) {
		t.flipColors(h)
	}

	return h, inserted
}

// Contains checks if a price exists in the tree.
func (t *PriceLevelTree) Contains(price udecimal.Decimal) bool {
	return t.search(t.root, price) != NullIndex
}

func (t *PriceLevelTree) search(h int32, price udecimal.Decimal) int32 {
	for h != NullIndex {
		cmp := price.Cmp(t.nodes[h].Price)
		if cmp < 0 {
			h = t.nodes[h].Left
		} else if cmp > 0 {
			h = t.nodes[h].Right
		} else {
			return h
		}
	}
	return NullIndex
}

// Min returns the minimum price in the tree.
// Returns zero value if tree is empty.
func (t *PriceLevelTree) Min() (udecimal.Decimal, bool) {
	if t.minCache == NullIndex {
		return udecimal.Zero, false
	}
	return t.nodes[t.minCache].Price, true
}

func (t *PriceLevelTree) findMin(h int32) int32 {
	if h == NullIndex {
		return NullIndex
	}
	for t.nodes[h].Left != NullIndex {
		h = t.nodes[h].Left
	}
	return h
}

// Max returns the maximum price in the tree.
func (t *PriceLevelTree) Max() (udecimal.Decimal, bool) {
	if t.root == NullIndex {
		return udecimal.Zero, false
	}
	h := t.root
	for t.nodes[h].Right != NullIndex {
		h = t.nodes[h].Right
	}
	return t.nodes[h].Price, true
}

// Count returns the number of nodes in the tree.
func (t *PriceLevelTree) Count() int32 {
	return t.count
}

// Successor returns the next larger price after the given price.
func (t *PriceLevelTree) Successor(price udecimal.Decimal) (udecimal.Decimal, bool) {
	idx := t.search(t.root, price)
	if idx == NullIndex {
		return udecimal.Zero, false
	}
	succIdx := t.successor(idx)
	if succIdx == NullIndex {
		return udecimal.Zero, false
	}
	return t.nodes[succIdx].Price, true
}

func (t *PriceLevelTree) successor(idx int32) int32 {
	node := &t.nodes[idx]
	if node.Right != NullIndex {
		return t.findMin(node.Right)
	}
	parent := node.Parent
	for parent != NullIndex && idx == t.nodes[parent].Right {
		idx = parent
		parent = t.nodes[parent].Parent
	}
	return parent
}

// Delete removes a price from the tree.
// Returns true if the price was found and deleted.
func (t *PriceLevelTree) Delete(price udecimal.Decimal) bool {
	if t.root == NullIndex {
		return false
	}

	// Check if we need to update min cache before delete
	needUpdateMin := t.minCache != NullIndex && t.nodes[t.minCache].Price.Equal(price)

	// Single-pass delete with found flag
	var found bool
	if !t.isRed(t.nodes[t.root].Left) && !t.isRed(t.nodes[t.root].Right) {
		t.nodes[t.root].Color = colorRed
	}
	t.root, found = t.deleteWithFlag(t.root, price)
	if !found {
		// Restore root color if nothing was deleted
		if t.root != NullIndex {
			t.nodes[t.root].Color = colorBlack
		}
		return false
	}

	if t.root != NullIndex {
		t.nodes[t.root].Color = colorBlack
		t.nodes[t.root].Parent = NullIndex
	}
	t.count--

	// Update min cache
	if needUpdateMin {
		t.minCache = t.findMin(t.root)
	}

	return true
}

func (t *PriceLevelTree) deleteWithFlag(h int32, price udecimal.Decimal) (int32, bool) {
	if h == NullIndex {
		return NullIndex, false
	}

	var found bool
	if price.LessThan(t.nodes[h].Price) {
		if t.nodes[h].Left == NullIndex {
			return h, false // Not found
		}
		if !t.isRed(t.nodes[h].Left) && !t.isRed(t.nodes[t.nodes[h].Left].Left) {
			h = t.moveRedLeft(h)
		}
		t.nodes[h].Left, found = t.deleteWithFlag(t.nodes[h].Left, price)
	} else {
		if t.isRed(t.nodes[h].Left) {
			h = t.rotateRight(h)
		}
		if price.Equal(t.nodes[h].Price) && t.nodes[h].Right == NullIndex {
			t.free(h)
			return NullIndex, true
		}
		if t.nodes[h].Right == NullIndex {
			return h, false // Not found (price > current but no right child)
		}
		if !t.isRed(t.nodes[h].Right) && !t.isRed(t.nodes[t.nodes[h].Right].Left) {
			h = t.moveRedRight(h)
		}
		if price.Equal(t.nodes[h].Price) {
			// Find minimum in right subtree
			minIdx := t.findMin(t.nodes[h].Right)
			t.nodes[h].Price = t.nodes[minIdx].Price
			t.nodes[h].Right = t.deleteMin(t.nodes[h].Right)
			found = true
		} else {
			t.nodes[h].Right, found = t.deleteWithFlag(t.nodes[h].Right, price)
		}
	}
	return t.balance(h), found
}

func (t *PriceLevelTree) delete(h int32, price udecimal.Decimal) int32 {
	if price.LessThan(t.nodes[h].Price) {
		if !t.isRed(t.nodes[h].Left) && !t.isRed(t.nodes[t.nodes[h].Left].Left) {
			h = t.moveRedLeft(h)
		}
		t.nodes[h].Left = t.delete(t.nodes[h].Left, price)
	} else {
		if t.isRed(t.nodes[h].Left) {
			h = t.rotateRight(h)
		}
		if price.Equal(t.nodes[h].Price) && t.nodes[h].Right == NullIndex {
			t.free(h)
			return NullIndex
		}
		if !t.isRed(t.nodes[h].Right) && !t.isRed(t.nodes[t.nodes[h].Right].Left) {
			h = t.moveRedRight(h)
		}
		if price.Equal(t.nodes[h].Price) {
			// Find minimum in right subtree
			minIdx := t.findMin(t.nodes[h].Right)
			t.nodes[h].Price = t.nodes[minIdx].Price
			t.nodes[h].Right = t.deleteMin(t.nodes[h].Right)
		} else {
			t.nodes[h].Right = t.delete(t.nodes[h].Right, price)
		}
	}
	return t.balance(h)
}

func (t *PriceLevelTree) moveRedLeft(h int32) int32 {
	t.flipColors(h)
	if t.isRed(t.nodes[t.nodes[h].Right].Left) {
		t.nodes[h].Right = t.rotateRight(t.nodes[h].Right)
		h = t.rotateLeft(h)
		t.flipColors(h)
	}
	return h
}

func (t *PriceLevelTree) moveRedRight(h int32) int32 {
	t.flipColors(h)
	if t.isRed(t.nodes[t.nodes[h].Left].Left) {
		h = t.rotateRight(h)
		t.flipColors(h)
	}
	return h
}

func (t *PriceLevelTree) deleteMin(h int32) int32 {
	if t.nodes[h].Left == NullIndex {
		t.free(h)
		return NullIndex
	}
	if !t.isRed(t.nodes[h].Left) && !t.isRed(t.nodes[t.nodes[h].Left].Left) {
		h = t.moveRedLeft(h)
	}
	t.nodes[h].Left = t.deleteMin(t.nodes[h].Left)
	return t.balance(h)
}

func (t *PriceLevelTree) balance(h int32) int32 {
	if t.isRed(t.nodes[h].Right) && !t.isRed(t.nodes[h].Left) {
		h = t.rotateLeft(h)
	}
	if t.isRed(t.nodes[h].Left) && t.isRed(t.nodes[t.nodes[h].Left].Left) {
		h = t.rotateRight(h)
	}
	if t.isRed(t.nodes[h].Left) && t.isRed(t.nodes[h].Right) {
		t.flipColors(h)
	}
	return h
}

// DeleteMin removes and returns the minimum price.
func (t *PriceLevelTree) DeleteMin() (udecimal.Decimal, bool) {
	if t.root == NullIndex {
		return udecimal.Zero, false
	}
	minPrice := t.nodes[t.minCache].Price

	if !t.isRed(t.nodes[t.root].Left) && !t.isRed(t.nodes[t.root].Right) {
		t.nodes[t.root].Color = colorRed
	}
	t.root = t.deleteMin(t.root)
	if t.root != NullIndex {
		t.nodes[t.root].Color = colorBlack
		t.nodes[t.root].Parent = NullIndex
	}
	t.count--
	t.minCache = t.findMin(t.root)

	return minPrice, true
}

// InOrderSlice returns all prices in sorted order (for testing/debugging).
func (t *PriceLevelTree) InOrderSlice() []udecimal.Decimal {
	result := make([]udecimal.Decimal, 0, t.count)
	t.inOrder(t.root, &result)
	return result
}

func (t *PriceLevelTree) inOrder(h int32, result *[]udecimal.Decimal) {
	if h == NullIndex {
		return
	}
	t.inOrder(t.nodes[h].Left, result)
	*result = append(*result, t.nodes[h].Price)
	t.inOrder(t.nodes[h].Right, result)
}
