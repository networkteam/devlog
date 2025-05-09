package collector

import "sync"

// RingBuffer is a thread-safe ring buffer
type RingBuffer[T any] struct {
	buffer     []T
	size       uint64
	capacity   uint64
	writeIndex uint64
	mu         sync.RWMutex
}

// NewRingBuffer creates a new ring buffer with the given capacity
func NewRingBuffer[T any](capacity uint64) *RingBuffer[T] {
	if capacity == 0 {
		panic("capacity must be greater than 0")
	}

	return &RingBuffer[T]{
		buffer:     make([]T, capacity),
		capacity:   capacity,
		size:       0,
		writeIndex: 0,
	}
}

// Add adds an entry to the buffer
func (rb *RingBuffer[T]) Add(record T) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	// Write at current position
	rb.buffer[rb.writeIndex%rb.capacity] = record

	// Increment write index
	rb.writeIndex++

	// Update size (up to capacity)
	if rb.size < rb.capacity {
		rb.size++
	}
}

// GetRecords returns a slice of the most recent n records
func (rb *RingBuffer[T]) GetRecords(n uint64) []T {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	// Get the actual number of records to return
	count := min(n, rb.size)
	if count == 0 {
		return []T{}
	}

	result := make([]T, count)

	// Calculate the starting index
	startIdx := rb.writeIndex - count
	for i := uint64(0); i < count; i++ {
		// Use modulo to wrap around the buffer
		result[i] = rb.buffer[(startIdx+i)%rb.capacity]
	}

	return result
}

// Size returns the current number of records in the buffer
func (rb *RingBuffer[T]) Size() uint64 {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.size
}

// Capacity returns the maximum capacity of the buffer
func (rb *RingBuffer[T]) Capacity() uint64 {
	return rb.capacity
}
