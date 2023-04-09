package match

import (
	"fmt"
	"math/rand"
	"runtime"
	"strconv"
	"testing"

	"github.com/shopspring/decimal"
)

const (
	start = 10   // actual = start  * goprocs
	end   = 3000 // actual = end    * goprocs
	step  = 200
)

func BenchmarkPlaceOrders(b *testing.B) {
	goprocs := runtime.GOMAXPROCS(0)

	for i := start; i < end; i += step {
		engine := NewMatchingEngine()

		b.Run(fmt.Sprintf("goroutines-%d", i*goprocs), func(b *testing.B) {
			b.SetParallelism(i)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					id := rand.Intn(1000-1) + 1

					order := Order{
						ID:       strconv.Itoa(i),
						MarketID: "BTC-USDT",
						Type:     Limit,
						Side:     Buy,
						Price:    decimal.NewFromInt(int64(id)),
						Size:     decimal.NewFromInt(1),
					}

					engine.PlaceOrder(&order)
				}
			})
		})
	}
}
