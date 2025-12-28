package match

import (
	"testing"

	"github.com/quagmt/udecimal"
	"github.com/stretchr/testify/assert"
)

func TestBuyerQueue(t *testing.T) {
	q := NewBuyerQueue()

	q.insertOrder(&Order{
		ID:    "101",
		Price: udecimal.MustFromInt64(10, 0),
		Size:  udecimal.MustFromInt64(10, 0),
	}, false)

	q.insertOrder(&Order{
		ID:    "201",
		Price: udecimal.MustFromInt64(20, 0),
		Size:  udecimal.MustFromInt64(10, 0),
	}, false)

	q.insertOrder(&Order{
		ID:    "301",
		Price: udecimal.MustFromInt64(30, 0),
		Size:  udecimal.MustFromInt64(10, 0),
	}, false)

	q.insertOrder(&Order{
		ID:    "202",
		Price: udecimal.MustFromInt64(20, 0),
		Size:  udecimal.MustFromInt64(100, 0),
	}, false)

	assert.Equal(t, int64(4), q.orderCount())

	depths := q.depth(25)
	assert.Len(t, depths, 3)
	assert.Equal(t, "30", depths[0].Price.String()) // depth 1
	assert.Equal(t, "110", depths[1].Size.String()) // depth 2

	ord := q.popHeadOrder()
	assert.Equal(t, "301", ord.ID)
	assert.Equal(t, "30", ord.Price.String())
	assert.Equal(t, "10", ord.Size.String())

	ord = q.popHeadOrder()
	assert.Equal(t, "201", ord.ID)
	assert.Equal(t, "20", ord.Price.String())
	assert.Equal(t, "10", ord.Size.String())
	ord.Size = udecimal.MustFromInt64(2, 0)
	q.insertOrder(ord, true)

	ord = q.popHeadOrder()
	assert.Equal(t, "201", ord.ID)
	assert.Equal(t, "20", ord.Price.String())
	assert.Equal(t, "2", ord.Size.String())

	ord = q.popHeadOrder()
	assert.Equal(t, "202", ord.ID)
	assert.Equal(t, "20", ord.Price.String())

	ord = q.popHeadOrder()
	assert.Equal(t, "101", ord.ID)
	assert.Equal(t, "10", ord.Price.String())

	assert.Equal(t, int64(0), q.orderCount())
}

func TestSellerQueue(t *testing.T) {
	q := NewSellerQueue()

	q.insertOrder(&Order{
		ID:    "101",
		Price: udecimal.MustFromInt64(10, 0),
		Size:  udecimal.MustFromInt64(10, 0),
	}, false)

	q.insertOrder(&Order{
		ID:    "201",
		Price: udecimal.MustFromInt64(20, 0),
		Size:  udecimal.MustFromInt64(10, 0),
	}, false)

	q.insertOrder(&Order{
		ID:    "301",
		Price: udecimal.MustFromInt64(30, 0),
		Size:  udecimal.MustFromInt64(10, 0),
	}, false)

	q.insertOrder(&Order{
		ID:    "202",
		Price: udecimal.MustFromInt64(20, 0),
		Size:  udecimal.MustFromInt64(100, 0),
	}, false)

	assert.Equal(t, int64(4), q.orderCount())
	depths := q.depth(25)
	assert.Len(t, depths, 3)
	assert.Equal(t, "10", depths[0].Price.String()) // depth 1
	assert.Equal(t, "110", depths[1].Size.String()) // depth 2

	ord := q.popHeadOrder()
	assert.Equal(t, "101", ord.ID)
	assert.Equal(t, "10", ord.Price.String())

	ord = q.popHeadOrder()
	assert.Equal(t, "201", ord.ID)
	assert.Equal(t, "20", ord.Price.String())
	assert.Equal(t, "10", ord.Size.String())
	ord.Size = udecimal.MustFromInt64(2, 0)
	q.insertOrder(ord, true)

	ord = q.popHeadOrder()
	assert.Equal(t, "201", ord.ID)
	assert.Equal(t, "20", ord.Price.String())
	assert.Equal(t, "2", ord.Size.String())

	ord = q.popHeadOrder()
	assert.Equal(t, "202", ord.ID)
	assert.Equal(t, "20", ord.Price.String())

	ord = q.popHeadOrder()
	assert.Equal(t, "301", ord.ID)
	assert.Equal(t, "30", ord.Price.String())

	assert.Equal(t, int64(0), q.orderCount())

}
