package match

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	GoSize   = 1_000
	SchPerGo = 50
)

// TestEvent is a simple event type for benchmarking
type TestEvent struct {
	ID    int64
	Value int64
}

type CounterEventHandler[T any] struct {
	count  int32
	ts     time.Time
	stopCh chan struct{}
	goSize int
}

func (h *CounterEventHandler[T]) OnEvent(v T) {
	currentCount := atomic.AddInt32(&h.count, 1)
	if currentCount == 1 {
		h.ts = time.Now()
	}

	if currentCount == int32(h.goSize*SchPerGo) {
		tl := time.Since(h.ts)
		fmt.Printf("== Disruptor read time = %d ms, go_size: %d, counter: %d\n", tl.Milliseconds(), h.goSize, h.count)
	}
}

func BenchmarkDisruptor(b *testing.B) {
	var counter uint64

	handler := &CounterEventHandler[TestEvent]{
		goSize: b.N,
		stopCh: make(chan struct{}),
	}

	rb := NewRingBuffer[TestEvent](1024*1024, handler)
	rb.Start()

	b.ResetTimer()

	var wg sync.WaitGroup
	wg.Add(b.N)
	for i := 0; i < b.N; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < SchPerGo; j++ {
				id := atomic.AddUint64(&counter, 1)
				evt := TestEvent{ID: int64(id)}
				rb.Publish(evt)
			}
		}()
	}

	wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = rb.Shutdown(ctx)
}

func BenchmarkChannel(b *testing.B) {
	var (
		length   = 1024 * 1024
		numPerGo = SchPerGo
		counter  = uint64(0)
		wg       sync.WaitGroup
	)
	ch := make(chan TestEvent, length)

	wg.Add(1)
	go func() {
		defer wg.Done()
		var ts time.Time
		var count int32
		for {
			<-ch
			atomic.AddInt32(&count, 1)
			if count == 1 {
				ts = time.Now()
			}

			if count == int32(b.N*numPerGo) {
				tl := time.Since(ts)
				fmt.Printf("== Channel read time = %d ms, counter = %d\n", tl.Milliseconds(), count)
				break
			}
		}
	}()

	b.ResetTimer()
	wg.Add(b.N)
	for i := 0; i < b.N; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < SchPerGo; j++ {
				atomic.AddUint64(&counter, 1)
				evt := TestEvent{}
				ch <- evt
			}
		}()
	}
	wg.Wait()
}
