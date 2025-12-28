package match

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
)

// ErrDisruptorTimeout is returned when shutdown times out
var ErrDisruptorTimeout = errors.New("disruptor: shutdown timeout")

// EventHandler 是事件處理器介面
type EventHandler[T any] interface {
	OnEvent(event T)
}

// RingBuffer 是 MPSC 的環狀緩衝區
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

	// Published slice to indicate ready slots
	published []int64

	// Event handler
	handler EventHandler[T]

	// Shutdown flag
	isShutdown atomic.Bool
}

// NewRingBuffer 創建新的 MPSC RingBuffer
// size 必須是 2 的冪次方
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

	// 初始化 published 為 -1
	for i := range rb.published {
		atomic.StoreInt64(&rb.published[i], -1)
	}

	return rb
}

// Publish 發布事件到 ring buffer (多 producer 安全)
func (rb *RingBuffer[T]) Publish(event T) {
	// 檢查是否已關機
	if rb.isShutdown.Load() {
		return
	}

	var nextSeq int64
	for {
		// 步驟 1: 原子性地申請一個序號 (Claim a sequence)
		currentProducerSeq := rb.producerSequence.Load()
		nextSeq = currentProducerSeq + 1

		// 檢查是否有足夠空間
		// producer 序號不能超過 consumer 序號一個 buffer 的大小
		wrapPoint := nextSeq - rb.capacity
		consumerSeq := rb.consumerSequence.Load()

		if wrapPoint > consumerSeq {
			// Buffer 已滿，等待 consumer 處理
			runtime.Gosched()
			continue
		}

		// 嘗試原子性地更新 producer sequence
		if rb.producerSequence.CompareAndSwap(currentProducerSeq, nextSeq) {
			// 成功獲取序列號
			break
		}
		// CAS 失敗，重試
		runtime.Gosched()
	}

	// 步驟 2: 將資料寫入申請到的位置 (Write data to the slot)
	index := nextSeq & rb.bufferMask
	rb.buffer[index] = event

	// 步驟 3: 發布 (publish) 此次寫入，讓 consumer 可見
	// 直接設置 published[index] = nextSeq (無自旋)
	atomic.StoreInt64(&rb.published[index], nextSeq)
}

// Start 啟動 consumer worker
func (rb *RingBuffer[T]) Start() {
	go rb.consumerLoop()
}

// Shutdown 停止 disruptor
func (rb *RingBuffer[T]) Shutdown(ctx context.Context) error {
	// 設置關機標誌，阻止新的 publish
	rb.isShutdown.Store(true)

	for {
		select {
		case <-ctx.Done():
			return ErrDisruptorTimeout
		default:
			if rb.ConsumerSequence() >= rb.ProducerSequence() {
				// 確保 consumer 已處理完所有 claimed 事件
				return nil
			}
			runtime.Gosched()
		}
	}
}

// consumerLoop consumer 主循環
func (rb *RingBuffer[T]) consumerLoop() {
	nextConsumerSeq := rb.consumerSequence.Load() + 1

	for {
		// 獲取當前可用的最大序號 (producer 已 claim 的 max)
		availableSeq := rb.producerSequence.Load()

		if rb.isShutdown.Load() {
			// 關機時處理剩餘事件
			rb.processRemainingEvents(nextConsumerSeq)
			return
		}

		processed := false
		for nextConsumerSeq <= availableSeq {
			index := nextConsumerSeq & rb.bufferMask

			// 自旋等待該槽位被發布 (檢查 published == nextSeq)
			for atomic.LoadInt64(&rb.published[index]) != nextConsumerSeq {
				runtime.Gosched()
			}

			// 處理事件
			event := rb.buffer[index]
			rb.handler.OnEvent(event)

			// 更新 consumer sequence
			rb.consumerSequence.Store(nextConsumerSeq)
			nextConsumerSeq++
			processed = true
		}

		if !processed {
			// 沒有事件，讓出 CPU
			runtime.Gosched()
		}
	}
}

// processRemainingEvents 處理關機時的剩餘事件
func (rb *RingBuffer[T]) processRemainingEvents(nextConsumerSeq int64) {
	availableSeq := rb.producerSequence.Load()

	for nextConsumerSeq <= availableSeq {
		index := nextConsumerSeq & rb.bufferMask

		// 自旋等待該槽位被發布
		for atomic.LoadInt64(&rb.published[index]) != nextConsumerSeq {
			runtime.Gosched()
		}

		event := rb.buffer[index]
		rb.handler.OnEvent(event)

		rb.consumerSequence.Store(nextConsumerSeq)
		nextConsumerSeq++
	}
}

// ConsumerSequence 獲取當前 consumer sequence (用於監控)
func (rb *RingBuffer[T]) ConsumerSequence() int64 {
	return rb.consumerSequence.Load()
}

// ProducerSequence 獲取當前 producer sequence (用於監控)
func (rb *RingBuffer[T]) ProducerSequence() int64 {
	return rb.producerSequence.Load()
}

// GetPendingEvents 獲取待處理事件數量 (用於監控)
func (rb *RingBuffer[T]) GetPendingEvents() int64 {
	producerSeq := rb.producerSequence.Load()
	consumerSeq := rb.consumerSequence.Load()
	return producerSeq - consumerSeq
}
