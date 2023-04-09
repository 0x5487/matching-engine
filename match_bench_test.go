package match

import (
	"fmt"
	"math/rand"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/rs/xid"
	"github.com/shopspring/decimal"
)

const (
	start = 10   // actual = start  * goprocs
	end   = 3000 // actual = end    * goprocs
	step  = 200
)

func BenchmarkPlaceOrders(b *testing.B) {
	goprocs := runtime.GOMAXPROCS(2)

	var errCount int64

	for i := start; i < end; i += step {
		engine := NewMatchingEngine()

		b.Run(fmt.Sprintf("goroutines-%d", i*goprocs), func(b *testing.B) {
			b.SetParallelism(i)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					id := rand.Intn(1000-1) + 1

					order := Order{
						ID:       xid.New().String(),
						MarketID: "BTC-USDT",
						Type:     Limit,
						Side:     Buy,
						Price:    decimal.NewFromInt(int64(id)),
						Size:     decimal.NewFromInt(1),
					}

					err := engine.PlaceOrder(&order)
					if err != nil {
						atomic.AddInt64(&errCount, int64(1))
					}
				}
			})
		})

		bid := engine.orderBook("BTC-USDT").bidQueue
		b.Logf("order count: %d", bid.orderCount())
		b.Logf("error count: %d", errCount)
	}
}
