package engine

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestBuyerQueue(t *testing.T) {
	q := NewBuyerQueue()

	q.addOrder(&Order{
		ID:    "101",
		Price: decimal.NewFromInt(10),
		Size:  decimal.NewFromInt(10),
	}, false)

	q.addOrder(&Order{
		ID:    "201",
		Price: decimal.NewFromInt(20),
		Size:  decimal.NewFromInt(10),
	}, false)

	q.addOrder(&Order{
		ID:    "301",
		Price: decimal.NewFromInt(30),
		Size:  decimal.NewFromInt(10),
	}, false)

	q.addOrder(&Order{
		ID:    "202",
		Price: decimal.NewFromInt(20),
		Size:  decimal.NewFromInt(100),
	}, false)

	assert.Equal(t, int64(4), q.orderCount())

	ord := q.popHeadOrder()
	assert.Equal(t, "301", ord.ID)
	assert.Equal(t, "30", ord.Price.String())
	assert.Equal(t, "10", ord.Size.String())

	ord = q.popHeadOrder()
	assert.Equal(t, "201", ord.ID)
	assert.Equal(t, "20", ord.Price.String())
	assert.Equal(t, "10", ord.Size.String())
	ord.Size = decimal.NewFromInt(2)
	q.addOrder(ord, true)

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

	q.addOrder(&Order{
		ID:    "101",
		Price: decimal.NewFromInt(10),
		Size:  decimal.NewFromInt(10),
	}, false)

	q.addOrder(&Order{
		ID:    "201",
		Price: decimal.NewFromInt(20),
		Size:  decimal.NewFromInt(10),
	}, false)

	q.addOrder(&Order{
		ID:    "301",
		Price: decimal.NewFromInt(30),
		Size:  decimal.NewFromInt(10),
	}, false)

	q.addOrder(&Order{
		ID:    "202",
		Price: decimal.NewFromInt(20),
		Size:  decimal.NewFromInt(100),
	}, false)

	assert.Equal(t, int64(4), q.orderCount())

	ord := q.popHeadOrder()
	assert.Equal(t, "101", ord.ID)
	assert.Equal(t, "10", ord.Price.String())

	ord = q.popHeadOrder()
	assert.Equal(t, "201", ord.ID)
	assert.Equal(t, "20", ord.Price.String())
	assert.Equal(t, "10", ord.Size.String())
	ord.Size = decimal.NewFromInt(2)
	q.addOrder(ord, true)

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

func TestUpdateEvents(t *testing.T) {
	q := NewBuyerQueue()

	q.addOrder(&Order{
		ID:    "101",
		Price: decimal.NewFromInt(10),
		Size:  decimal.NewFromInt(1),
	}, false)

	q.addOrder(&Order{
		ID:    "102",
		Price: decimal.NewFromInt(10),
		Size:  decimal.NewFromInt(2),
	}, false)

	q.addOrder(&Order{
		ID:    "201",
		Price: decimal.NewFromInt(20),
		Size:  decimal.NewFromInt(5),
	}, false)

	events := q.sinceLastUpdateEvents()
	assert.Equal(t, int(2), len(events))

	for _, e := range events {
		if e.Price == "10" {
			assert.Equal(t, "3", e.Size)
		}

		if e.Price == "20" {
			assert.Equal(t, "5", e.Size)
		}
	}

	events = q.sinceLastUpdateEvents()
	assert.Equal(t, int(0), len(events))

	q.popHeadOrder()

	events = q.sinceLastUpdateEvents()
	assert.Equal(t, int(1), len(events))
	evt := events[0]
	assert.Equal(t, "20", evt.Price)
	assert.Equal(t, "0", evt.Size)
}
