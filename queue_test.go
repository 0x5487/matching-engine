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

func BenchmarkDepthAdd(b *testing.B) {
	q := NewBuyerQueue()

	//runtime.GOMAXPROCS(8)
	//b.SetParallelism(1000)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := rand.Intn(100000000)
			price := decimal.NewFromInt(int64(id))

			q.addOrder(Order{
				ID:        strconv.Itoa(id),
				Price:     price,
				Side:      1,
				CreatedAt: time.Now(),
			}, false)
		}
	})

	// for i := 0; i < b.N; i++ {
	// 	id := rand.Intn(100000000)
	// 	price := decimal.NewFromInt(int64(id))

	// 	q.addOrder(Order{
	// 		ID:        strconv.Itoa(id),
	// 		Price:     price,
	// 		Side:      1,
	// 		CreatedAt: time.Now(),
	// 	}, false)
	// }

	//b.Logf("after depth count: %d", q.depthCount())
}

func BenchmarkDepthRemove(b *testing.B) {
	q := NewBuyerQueue()

	for i := 0; i < b.N; i++ {
		price := decimal.NewFromInt(int64(rand.Intn(100000000)))

		id := strconv.Itoa(i)
		q.addOrder(Order{
			ID:        id,
			Price:     price,
			Side:      1,
			CreatedAt: time.Now(),
		}, false)
	}

	//b.Logf("before depth count: %d", q.depthCount())

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			q.popHeadOrder()
			if q.depthCount() == 0 {
				b.StopTimer()
				break
			}
		}
	})

	// for i := 0; i < b.N; i++ {
	// 	q.popHeadOrder()
	// 	if q.depthCount() == 0 {
	// 		b.StopTimer()
	// 		break
	// 	}
	// }

	//b.Logf("after depth count: %d", q.depthCount())
}

func BenchmarkSizeAdd(b *testing.B) {
	q := NewBuyerQueue()
	price := decimal.NewFromInt(10)
	size := decimal.NewFromInt(2)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := rand.Intn(100000000)

			q.addOrder(Order{
				ID:        strconv.Itoa(id),
				Price:     price,
				Size:      size,
				CreatedAt: time.Now(),
			}, false)
		}
	})

	// for i := 0; i < b.N; i++ {
	// 	id := strconv.Itoa(i)
	// 	q.addOrder(Order{
	// 		ID:        id,
	// 		Price:     price,
	// 		Size:      size,
	// 		CreatedAt: time.Now(),
	// 	}, false)
	// }

	//b.Logf("after total counts: %d", q.orderCount())
}

// func BenchmarkMapAdd(b *testing.B) {
// 	q := map[string]*Order{}

// 	price := decimal.NewFromInt(10)
// 	size := decimal.NewFromInt(2)

// 	for i := 0; i < b.N; i++ {
// 		id := strconv.Itoa(i)
// 		q[id] = &Order{
// 			ID:        id,
// 			Price:     price,
// 			Size:      size,
// 			CreatedAt: time.Now(),
// 		}
// 	}
// }

// func BenchmarkSMapAdd(b *testing.B) {
// 	q := sync.Map{}

// 	price := decimal.NewFromInt(10)
// 	size := decimal.NewFromInt(2)

// 	for i := 0; i < b.N; i++ {
// 		id := strconv.Itoa(i)

// 		q.Store(id, &Order{
// 			ID:        id,
// 			Price:     price,
// 			Size:      size,
// 			CreatedAt: time.Now(),
// 		})
// 	}
// }

// func BenchmarkMapRead(b *testing.B) {
// 	q := map[string]*Order{}

// 	price := decimal.NewFromInt(10)
// 	size := decimal.NewFromInt(2)

// 	for i := 0; i < b.N; i++ {
// 		id := strconv.Itoa(i)
// 		q[id] = &Order{
// 			ID:        id,
// 			Price:     price,
// 			Size:      size,
// 			CreatedAt: time.Now(),
// 		}
// 	}

// 	b.ResetTimer()

// 	total := 0
// 	for i := 0; i < b.N; i++ {
// 		_, ok := q[strconv.Itoa(i)]
// 		if ok {
// 			total++
// 		}
// 	}

// 	b.Logf("total: %d", total)
// }

func BenchmarkSizeRemove(b *testing.B) {
	q := NewBuyerQueue()
	price := decimal.NewFromInt(10)
	size := decimal.NewFromInt(2)

	for i := 0; i < b.N; i++ {
		id := strconv.Itoa(i)
		q.addOrder(Order{
			ID:        id,
			Price:     price,
			Size:      size,
			CreatedAt: time.Now(),
		}, false)
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			q.popHeadOrder()
			if q.orderCount() == 0 {
				b.StopTimer()
				break
			}
		}
	})

	// for i := 0; i < b.N; i++ {
	// 	q.popHeadOrder()
	// 	if q.orderCount() == 0 {
	// 		b.StopTimer()
	// 		break
	// 	}
	// }

	//b.Logf("after total counts: %d", q.orderCount())
}
