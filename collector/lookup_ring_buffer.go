package collector

import "sync"

type Identifieable[T comparable] interface {
	Identity() T
}

// LookupRingBuffer is a thread-safe ring buffer with lookup functionality
type LookupRingBuffer[T Identifieable[S], S comparable] struct {
	buffer     []T
	lookup     map[S]*T
	size       uint64
	capacity   uint64
	writeIndex uint64
	mu         sync.RWMutex
}

// NewLookupRingBuffer creates a new ring buffer with the given capacity
func NewLookupRingBuffer[T Identifieable[S], S comparable](capacity uint64) *LookupRingBuffer[T, S] {
	if capacity == 0 {
		panic("capacity must be greater than 0")
	}

	return &LookupRingBuffer[T, S]{
		buffer:     make([]T, capacity),
		lookup:     make(map[S]*T, capacity),
		capacity:   capacity,
		size:       0,
		writeIndex: 0,
	}
}

// Add adds an entry to the buffer
func (rb *LookupRingBuffer[T, S]) Add(record T) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	index := rb.writeIndex % rb.capacity

	lostRecord := rb.buffer[index]

	// Write at current position
	rb.buffer[index] = record

	// Increment write index
	rb.writeIndex++

	// Update size (up to capacity)
	if rb.size < rb.capacity {
		rb.size++
	} else {
		// Remove the old record from the lookup map
		delete(rb.lookup, lostRecord.Identity())
	}

	// Add the new record to the lookup map
	rb.lookup[record.Identity()] = &rb.buffer[index]
}

// GetRecords returns a slice of the most recent n records
func (rb *LookupRingBuffer[T, S]) GetRecords(n uint64) []T {
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

func (rb *LookupRingBuffer[T, S]) Lookup(identity S) (T, bool) {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	record, found := rb.lookup[identity]
	if found {
		return *record, true
	}

	var empty T
	return empty, false

}

// Size returns the current number of records in the buffer
func (rb *LookupRingBuffer[T, S]) Size() uint64 {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.size
}

// Capacity returns the maximum capacity of the buffer
func (rb *LookupRingBuffer[T, S]) Capacity() uint64 {
	return rb.capacity
}
