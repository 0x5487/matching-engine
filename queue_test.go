package match

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestBuyerQueue(t *testing.T) {
	q := NewBuyerQueue()

	q.insertOrder(&Order{
		ID:    "101",
		Price: decimal.NewFromInt(10),
		Size:  decimal.NewFromInt(10),
	}, false)

	q.insertOrder(&Order{
		ID:    "201",
		Price: decimal.NewFromInt(20),
		Size:  decimal.NewFromInt(10),
	}, false)

	q.insertOrder(&Order{
		ID:    "301",
		Price: decimal.NewFromInt(30),
		Size:  decimal.NewFromInt(10),
	}, false)

	q.insertOrder(&Order{
		ID:    "202",
		Price: decimal.NewFromInt(20),
		Size:  decimal.NewFromInt(100),
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
	ord.Size = decimal.NewFromInt(2)
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
		Price: decimal.NewFromInt(10),
		Size:  decimal.NewFromInt(10),
	}, false)

	q.insertOrder(&Order{
		ID:    "201",
		Price: decimal.NewFromInt(20),
		Size:  decimal.NewFromInt(10),
	}, false)

	q.insertOrder(&Order{
		ID:    "301",
		Price: decimal.NewFromInt(30),
		Size:  decimal.NewFromInt(10),
	}, false)

	q.insertOrder(&Order{
		ID:    "202",
		Price: decimal.NewFromInt(20),
		Size:  decimal.NewFromInt(100),
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
	ord.Size = decimal.NewFromInt(2)
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
