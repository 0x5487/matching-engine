package match

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
)

// ErrDisruptorTimeout is returned when shutdown times out.
var ErrDisruptorTimeout = errors.New("disruptor: shutdown timeout")

// EventHandler is the interface for processing events from the RingBuffer.
type EventHandler[T any] interface {
	OnEvent(event *T)
}

// RingBuffer is a lock-free MPSC (Multi-Producer Single-Consumer) ring buffer.
// It provides high-throughput, zero-allocation event passing between goroutines.
type RingBuffer[T any] struct {
	// Cache line padding to avoid false sharing
	_                [56]byte
	producerSequence atomic.Int64
	_                [56]byte
	consumerSequence atomic.Int64
	_                [56]byte

	// Ring buffer core
	buffer     []T
	bufferMask int64
	capacity   int64

	// Published array to track slot write completion
	published []int64

	// Event handler
	handler EventHandler[T]

	// Shutdown flag
	isShutdown atomic.Bool
}

// NewRingBuffer creates a new MPSC RingBuffer.
// The capacity must be a power of 2.
func NewRingBuffer[T any](capacity int64, handler EventHandler[T]) *RingBuffer[T] {
	if capacity <= 0 || (capacity&(capacity-1)) != 0 {
		panic("size must be a power of 2")
	}

	rb := &RingBuffer[T]{
		buffer:     make([]T, capacity),
		published:  make([]int64, capacity),
		capacity:   capacity,
		bufferMask: capacity - 1,
		handler:    handler,
	}

	rb.producerSequence.Store(-1)
	rb.consumerSequence.Store(-1)

	// Initialize published array to -1
	for i := range rb.published {
		atomic.StoreInt64(&rb.published[i], -1)
	}

	return rb
}

// Publish publishes an event to the ring buffer (multi-producer safe).
// This is a convenience method that wraps Claim() and Commit().
func (rb *RingBuffer[T]) Publish(event T) {
	seq, slot := rb.Claim()
	if seq == -1 {
		return
	}
	*slot = event
	rb.Commit(seq)
}

// Claim atomically claims a sequence and returns a pointer to the slot.
// Returns (-1, nil) if the RingBuffer is shut down.
// The caller should write to the slot and then call Commit(seq).
func (rb *RingBuffer[T]) Claim() (int64, *T) {
	// Check if shutdown
	if rb.isShutdown.Load() {
		return -1, nil
	}

	var nextSeq int64
	for {
		currentProducerSeq := rb.producerSequence.Load()
		nextSeq = currentProducerSeq + 1

		// Check if there is enough space
		wrapPoint := nextSeq - rb.capacity
		consumerSeq := rb.consumerSequence.Load()

		if wrapPoint > consumerSeq {
			// Buffer is full, wait for consumer to catch up
			runtime.Gosched()
			continue
		}

		// Try to atomically update producer sequence
		if rb.producerSequence.CompareAndSwap(currentProducerSeq, nextSeq) {
			// Successfully claimed the sequence
			return nextSeq, &rb.buffer[nextSeq&rb.bufferMask]
		}
		// CAS failed, retry
		runtime.Gosched()
	}
}

// Commit marks the slot as published, making it visible to the consumer.
func (rb *RingBuffer[T]) Commit(seq int64) {
	atomic.StoreInt64(&rb.published[seq&rb.bufferMask], seq)
}

// Start starts the consumer worker goroutine.
func (rb *RingBuffer[T]) Start() {
	go rb.consumerLoop()
}

// Run starts the consumer loop in the calling goroutine (blocking).
// This allows the caller to control the goroutine and OS thread,
// enabling runtime.LockOSThread() for CPU affinity scenarios.
func (rb *RingBuffer[T]) Run() {
	rb.consumerLoop()
}

// Shutdown gracefully stops the disruptor, ensuring all pending events are processed.
// It blocks until all events are processed or the context is cancelled.
func (rb *RingBuffer[T]) Shutdown(ctx context.Context) error {
	// Set shutdown flag to block new publishes
	rb.isShutdown.Store(true)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if rb.ConsumerSequence() >= rb.ProducerSequence() {
				// Consumer has processed all claimed events
				return nil
			}
			runtime.Gosched()
		}
	}
}

// consumerLoop is the main consumer loop.
func (rb *RingBuffer[T]) consumerLoop() {
	nextConsumerSeq := rb.consumerSequence.Load() + 1

	for {
		// Get the maximum available sequence (producer's last claimed)
		availableSeq := rb.producerSequence.Load()

		if rb.isShutdown.Load() {
			// Process remaining events during shutdown
			rb.processRemainingEvents(nextConsumerSeq)
			return
		}

		processed := false
		for nextConsumerSeq <= availableSeq {
			index := nextConsumerSeq & rb.bufferMask

			// Spin-wait for the slot to be published
			for atomic.LoadInt64(&rb.published[index]) != nextConsumerSeq {
				runtime.Gosched()
			}

			rb.handler.OnEvent(&rb.buffer[index])

			// Update consumer sequence
			rb.consumerSequence.Store(nextConsumerSeq)
			nextConsumerSeq++
			processed = true
		}

		if !processed {
			// No events available, yield CPU
			runtime.Gosched()
		}
	}
}

// processRemainingEvents processes any remaining events during shutdown.
func (rb *RingBuffer[T]) processRemainingEvents(nextConsumerSeq int64) {
	availableSeq := rb.producerSequence.Load()

	for nextConsumerSeq <= availableSeq {
		index := nextConsumerSeq & rb.bufferMask

		// Spin-wait for the slot to be published
		for atomic.LoadInt64(&rb.published[index]) != nextConsumerSeq {
			runtime.Gosched()
		}

		rb.handler.OnEvent(&rb.buffer[index])

		rb.consumerSequence.Store(nextConsumerSeq)
		nextConsumerSeq++
	}
}

// ConsumerSequence returns the current consumer sequence (for monitoring).
func (rb *RingBuffer[T]) ConsumerSequence() int64 {
	return rb.consumerSequence.Load()
}

// ProducerSequence returns the current producer sequence (for monitoring).
func (rb *RingBuffer[T]) ProducerSequence() int64 {
	return rb.producerSequence.Load()
}

// GetPendingEvents returns the number of pending events (for monitoring).
func (rb *RingBuffer[T]) GetPendingEvents() int64 {
	producerSeq := rb.producerSequence.Load()
	consumerSeq := rb.consumerSequence.Load()
	return producerSeq - consumerSeq
}
