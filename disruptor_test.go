package match

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	GoSize   = 1_000
	SchPerGo = 50
)

// TestEvent is a simple event type for testing.
type TestEvent struct {
	ID    int64
	Value int64
}

// CounterEventHandler counts processed events for benchmarking.
type CounterEventHandler[T any] struct {
	count  int32
	ts     time.Time
	stopCh chan struct{}
	goSize int
}

func (h *CounterEventHandler[T]) OnEvent(v *T) {
	currentCount := atomic.AddInt32(&h.count, 1)
	if currentCount == 1 {
		h.ts = time.Now()
	}

	if currentCount == int32(h.goSize*SchPerGo) {
		tl := time.Since(h.ts)
		fmt.Printf("== Disruptor read time = %d ms, go_size: %d, counter: %d\n", tl.Milliseconds(), h.goSize, h.count)
	}
}

// --- Unit Tests ---

func TestRingBuffer_BasicOperations(t *testing.T) {
	var processed []int64
	var mu sync.Mutex

	handler := &simpleHandler[TestEvent]{
		fn: func(e *TestEvent) {
			mu.Lock()
			processed = append(processed, e.ID)
			mu.Unlock()
		},
	}

	rb := NewRingBuffer[TestEvent](16, handler)
	go rb.Run()

	// Publish 10 events
	for i := int64(1); i <= 10; i++ {
		rb.Publish(TestEvent{ID: i})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := rb.Shutdown(ctx)
	require.NoError(t, err)

	// Verify all events were processed in order
	assert.Len(t, processed, 10)
	for i := int64(1); i <= 10; i++ {
		assert.Equal(t, i, processed[i-1])
	}
}

func TestRingBuffer_ClaimCommit(t *testing.T) {
	var processed []TestEvent
	var mu sync.Mutex

	handler := &simpleHandler[TestEvent]{
		fn: func(e *TestEvent) {
			mu.Lock()
			processed = append(processed, *e)
			mu.Unlock()
		},
	}

	rb := NewRingBuffer[TestEvent](16, handler)
	go rb.Run()

	// Use Claim/Commit pattern
	seq, slot := rb.Claim()
	require.NotEqual(t, int64(-1), seq)
	slot.ID = 42
	slot.Value = 100
	rb.Commit(seq)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := rb.Shutdown(ctx)
	require.NoError(t, err)

	require.Len(t, processed, 1)
	assert.Equal(t, int64(42), processed[0].ID)
	assert.Equal(t, int64(100), processed[0].Value)
}

func TestRingBuffer_ClaimAfterShutdown(t *testing.T) {
	handler := &simpleHandler[TestEvent]{fn: func(e *TestEvent) {}}
	rb := NewRingBuffer[TestEvent](16, handler)
	go rb.Run()

	// Shutdown first
	ctx := context.Background()
	_ = rb.Shutdown(ctx)

	// Claim should return -1 after shutdown
	seq, slot := rb.Claim()
	assert.Equal(t, int64(-1), seq)
	assert.Nil(t, slot)
}

func TestRingBuffer_GetPendingEvents(t *testing.T) {
	// Create a handler that blocks until signaled
	blockCh := make(chan struct{})
	handler := &simpleHandler[TestEvent]{
		fn: func(e *TestEvent) {
			<-blockCh // Wait until unblocked
		},
	}

	rb := NewRingBuffer[TestEvent](16, handler)
	go rb.Run()

	// Publish 5 events (they will be pending because handler is blocked)
	for i := 0; i < 5; i++ {
		rb.Publish(TestEvent{ID: int64(i)})
	}

	// Allow some time for producer sequence to update
	time.Sleep(10 * time.Millisecond)

	// Check pending events
	pending := rb.GetPendingEvents()
	assert.GreaterOrEqual(t, pending, int64(4)) // At least 4 pending

	// Unblock all handlers
	close(blockCh)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = rb.Shutdown(ctx)

	// After shutdown, no pending events
	assert.Equal(t, int64(0), rb.GetPendingEvents())
}

func TestRingBuffer_SequenceMonitoring(t *testing.T) {
	handler := &simpleHandler[TestEvent]{fn: func(e *TestEvent) {}}
	rb := NewRingBuffer[TestEvent](16, handler)

	// Initial sequences should be -1
	assert.Equal(t, int64(-1), rb.ProducerSequence())
	assert.Equal(t, int64(-1), rb.ConsumerSequence())

	go rb.Run()

	// Publish some events
	for i := 0; i < 3; i++ {
		rb.Publish(TestEvent{ID: int64(i)})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = rb.Shutdown(ctx)

	// After processing, both sequences should be at 2 (0-indexed)
	assert.Equal(t, int64(2), rb.ProducerSequence())
	assert.Equal(t, int64(2), rb.ConsumerSequence())
}

func TestRingBuffer_ShutdownTimeout(t *testing.T) {
	// Create a handler that never completes
	handler := &simpleHandler[TestEvent]{
		fn: func(e *TestEvent) {
			time.Sleep(10 * time.Second) // Block forever (for test purposes)
		},
	}

	rb := NewRingBuffer[TestEvent](16, handler)
	go rb.Run()

	// Publish an event
	rb.Publish(TestEvent{ID: 1})

	// Shutdown with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := rb.Shutdown(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRingBuffer_ConcurrentPublish(t *testing.T) {
	var count atomic.Int64

	handler := &simpleHandler[TestEvent]{
		fn: func(e *TestEvent) {
			count.Add(1)
		},
	}

	rb := NewRingBuffer[TestEvent](1024, handler)
	go rb.Run()

	// Concurrent publishers
	const numPublishers = 10
	const eventsPerPublisher = 100

	var wg sync.WaitGroup
	wg.Add(numPublishers)

	for i := 0; i < numPublishers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerPublisher; j++ {
				rb.Publish(TestEvent{ID: int64(id*eventsPerPublisher + j)})
			}
		}(i)
	}

	wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rb.Shutdown(ctx)
	require.NoError(t, err)

	assert.Equal(t, int64(numPublishers*eventsPerPublisher), count.Load())
}

func TestRingBuffer_PowerOf2Validation(t *testing.T) {
	handler := &simpleHandler[TestEvent]{fn: func(e *TestEvent) {}}

	// Should panic for non-power-of-2
	assert.Panics(t, func() {
		NewRingBuffer[TestEvent](15, handler)
	})

	assert.Panics(t, func() {
		NewRingBuffer[TestEvent](0, handler)
	})

	assert.Panics(t, func() {
		NewRingBuffer[TestEvent](-1, handler)
	})

	// Should not panic for valid power-of-2
	assert.NotPanics(t, func() {
		NewRingBuffer[TestEvent](16, handler)
	})
}

// simpleHandler is a test helper that wraps a function.
type simpleHandler[T any] struct {
	fn func(*T)
}

func (h *simpleHandler[T]) OnEvent(e *T) {
	h.fn(e)
}

// --- Benchmarks ---

func BenchmarkDisruptor(b *testing.B) {
	var counter uint64

	handler := &CounterEventHandler[TestEvent]{
		goSize: b.N,
		stopCh: make(chan struct{}),
	}

	rb := NewRingBuffer[TestEvent](1024*1024, handler)
	go rb.Run()

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
			for j := 0; j < numPerGo; j++ {
				atomic.AddUint64(&counter, 1)
				evt := TestEvent{}
				ch <- evt
			}
		}()
	}
	wg.Wait()
}
