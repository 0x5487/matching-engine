package engine

import (
	"fmt"
	"math/rand"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

const (
	start = 10   // actual = start  * goprocs
	end   = 3000 // actual = end    * goprocs
	step  = 200
)

func BenchmarkDepthAdd(b *testing.B) {
	goprocs := runtime.GOMAXPROCS(0)

	for i := start; i < end; i += step {
		q := NewBuyerQueue()

		b.Run(fmt.Sprintf("goroutines-%d", i*goprocs), func(b *testing.B) {
			b.SetParallelism(i)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					id := rand.Intn(100000000)
					price := decimal.NewFromInt(int64(id))

					q.addOrder(&Order{
						ID:        strconv.Itoa(id),
						Price:     price,
						Side:      1,
						CreatedAt: time.Now(),
					}, false)
				}
			})
		})
	}

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
	goprocs := runtime.GOMAXPROCS(0)

	for i := start; i < end; i += step {
		b.Run(fmt.Sprintf("goroutines-%d", i*goprocs), func(b *testing.B) {
			q := NewBuyerQueue()

			for i := 0; i < b.N; i++ {
				price := decimal.NewFromInt(int64(rand.Intn(100000000)))

				id := strconv.Itoa(i)
				q.addOrder(&Order{
					ID:        id,
					Price:     price,
					Side:      1,
					CreatedAt: time.Now(),
				}, false)
			}

			b.ResetTimer()

			b.SetParallelism(i)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					q.popHeadOrder()
					if q.depthCount() == 0 {
						break
					}
				}
			})
		})
	}

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
	goprocs := runtime.GOMAXPROCS(0)

	price := decimal.NewFromInt(10)
	size := decimal.NewFromInt(2)

	for i := start; i < end; i += step {
		q := NewBuyerQueue()

		b.Run(fmt.Sprintf("goroutines-%d", i*goprocs), func(b *testing.B) {
			b.SetParallelism(i)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					id := rand.Intn(100000000)

					q.addOrder(&Order{
						ID:        strconv.Itoa(id),
						Price:     price,
						Size:      size,
						CreatedAt: time.Now(),
					}, false)
				}
			})
		})
	}

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
	goprocs := runtime.GOMAXPROCS(0)

	for i := start; i < end; i += step {
		b.Run(fmt.Sprintf("goroutines-%d", i*goprocs), func(b *testing.B) {
			q := NewBuyerQueue()
			price := decimal.NewFromInt(10)
			size := decimal.NewFromInt(2)

			for i := 0; i < b.N; i++ {
				id := strconv.Itoa(i)
				q.addOrder(&Order{
					ID:        id,
					Price:     price,
					Size:      size,
					CreatedAt: time.Now(),
				}, false)
			}

			b.ResetTimer()
			b.SetParallelism(i)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					q.popHeadOrder()
					if q.orderCount() == 0 {
						break
					}
				}
			})
		})
	}

	// for i := 0; i < b.N; i++ {
	// 	q.popHeadOrder()
	// 	if q.orderCount() == 0 {
	// 		b.StopTimer()
	// 		break
	// 	}
	// }

	//b.Logf("after total counts: %d", q.orderCount())
}
