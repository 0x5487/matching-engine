package engine

import (
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestBuyerQueue(t *testing.T) {
	q := NewBuyerQueue()

	q.addOrder(Order{
		ID:    "101",
		Price: decimal.NewFromInt(10),
	}, false)

	q.addOrder(Order{
		ID:    "201",
		Price: decimal.NewFromInt(20),
		Size:  decimal.NewFromInt(10),
	}, false)

	q.addOrder(Order{
		ID:    "301",
		Price: decimal.NewFromInt(30),
		Size:  decimal.NewFromInt(10),
	}, false)

	q.addOrder(Order{
		ID:    "202",
		Price: decimal.NewFromInt(20),
		Size:  decimal.NewFromInt(100),
	}, false)

	assert.Equal(t, int64(4), q.Len())

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

	assert.Equal(t, int64(0), q.Len())
}

func TestSellerQueue(t *testing.T) {
	q := NewSellerQueue()

	q.addOrder(Order{
		ID:    "101",
		Price: decimal.NewFromInt(10),
	}, false)

	q.addOrder(Order{
		ID:    "201",
		Price: decimal.NewFromInt(20),
		Size:  decimal.NewFromInt(10),
	}, false)

	q.addOrder(Order{
		ID:    "301",
		Price: decimal.NewFromInt(30),
		Size:  decimal.NewFromInt(10),
	}, false)

	q.addOrder(Order{
		ID:    "202",
		Price: decimal.NewFromInt(20),
		Size:  decimal.NewFromInt(100),
	}, false)

	assert.Equal(t, int64(4), q.Len())

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

	assert.Equal(t, int64(0), q.Len())

}

func BenchmarkAddDepth(b *testing.B) {
	n := decimal.NewFromInt(1000)
	q := NewBuyerQueue()

	for i := 0; i < b.N; i++ {
		price := decimal.NewFromInt(int64(rand.Intn(1000000))).Div(n)

		id := strconv.Itoa(i)
		q.addOrder(Order{
			ID:        id,
			Price:     price,
			Side:      1,
			CreatedAt: time.Now(),
		}, false)

	}
}

func BenchmarkAddSize(b *testing.B) {
	q := NewBuyerQueue()
	price := decimal.NewFromInt(10)

	for i := 0; i < b.N; i++ {
		id := strconv.Itoa(i)
		q.addOrder(Order{
			ID:        id,
			Price:     price,
			Side:      1,
			CreatedAt: time.Now(),
		}, false)

	}

	b.Logf("total counts: %d", q.Len())
}

func BenchmarkRemoveSize(b *testing.B) {
	q := NewBuyerQueue()
	price := decimal.NewFromInt(10)

	for i := 0; i < 500000; i++ {
		id := strconv.Itoa(i)
		q.addOrder(Order{
			ID:        id,
			Price:     price,
			Side:      1,
			CreatedAt: time.Now(),
		}, false)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.popHeadOrder()
		if q.Len() == 0 {
			return
		}
	}

	b.Logf("after total counts: %d", q.Len())
}
