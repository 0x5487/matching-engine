package match

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/rs/xid"
	"github.com/shopspring/decimal"
)

const (
	start = 2000 // actual = start  * goprocs
	end   = 3000 // actual = end    * goprocs
	step  = 200
)

func BenchmarkPlaceOrders(b *testing.B) {
	goprocs := runtime.GOMAXPROCS(0)

	for i := start; i < end; i += step {
		ctx := context.Background()
		var errCount int64

		publishTrader := NewDiscardPublishLog()
		engine := NewMatchingEngine(publishTrader)

		b.Run(fmt.Sprintf("goroutines-%d", i*goprocs), func(b *testing.B) {
			b.SetParallelism(i)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					n, _ := rand.Int(rand.Reader, big.NewInt(100000))
					price := n.Int64() + 1

					order := &Order{
						ID:       xid.New().String(),
						MarketID: "BTC-USDT",
						Type:     Limit,
						Side:     Buy,
						Price:    decimal.NewFromInt(price),
						Size:     decimal.NewFromInt(1),
					}

					err := engine.AddOrder(ctx, order)
					if err != nil {
						atomic.AddInt64(&errCount, int64(1))
					}
				}
			})
		})

		bid := engine.OrderBook("BTC-USDT").bidQueue
		b.Logf("order count: %d", bid.orderCount())
		b.Logf("depth count: %d", bid.depthCount())
		b.Logf("error count: %d", errCount)
	}
}
