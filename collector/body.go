package collector

import (
	"bytes"
	"errors"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Constants for body capture
const (
	// DefaultBodyBufferSize is the default size of the buffer pool
	DefaultBodyBufferSize = 100 * 1024 * 1024 // 100MB

	// DefaultMaxBodySize is the default maximum size for a single body
	DefaultMaxBodySize = 1 * 1024 * 1024 // 1MB
)

var (
	// ErrBodyClosed is returned when attempting to read from a closed body
	ErrBodyClosed = errors.New("body is already closed")
)

// BodyBufferPool manages byte buffers for capturing HTTP bodies
type BodyBufferPool struct {
	mu            sync.Mutex
	maxPoolSize   int64
	currentSize   int64
	buffers       []*bodyBuffer
	maxBufferSize int64
}

// bodyBuffer represents a captured body in the pool
type bodyBuffer struct {
	buffer    *bytes.Buffer
	timestamp int64 // unix timestamp when created
	size      int64
}

// NewBodyBufferPool creates a new buffer pool for body capturing
func NewBodyBufferPool(maxPoolSize, maxBufferSize int64) *BodyBufferPool {
	// Ensure positive sizes
	if maxPoolSize <= 0 {
		maxPoolSize = DefaultBodyBufferSize
	}
	if maxBufferSize <= 0 {
		maxBufferSize = DefaultMaxBodySize
	}

	return &BodyBufferPool{
		maxPoolSize:   maxPoolSize,
		currentSize:   0,
		buffers:       make([]*bodyBuffer, 0),
		maxBufferSize: maxBufferSize,
	}
}

// GetBuffer returns a new buffer from the pool
func (p *BodyBufferPool) GetBuffer() *bytes.Buffer {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Ensure pool has capacity by removing old buffers if needed
	p.ensureCapacity(p.maxBufferSize)

	// Create a new buffer
	buf := &bytes.Buffer{}

	// Track this buffer
	p.buffers = append(p.buffers, &bodyBuffer{
		buffer:    buf,
		timestamp: time.Now().Unix(),
		size:      0,
	})

	return buf
}

// RegisterBuffer adds a buffer to the pool's tracking
func (p *BodyBufferPool) RegisterBuffer(buf *bytes.Buffer, size int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Update pool size
	atomic.AddInt64(&p.currentSize, size)

	// Find if the buffer is already tracked
	for i, b := range p.buffers {
		if b.buffer == buf {
			// Update existing entry
			p.buffers[i].size = size
			p.buffers[i].timestamp = time.Now().Unix()
			return
		}
	}

	// Add buffer to tracking if not found
	p.buffers = append(p.buffers, &bodyBuffer{
		buffer:    buf,
		timestamp: time.Now().Unix(),
		size:      size,
	})
}

// ensureCapacity ensures the pool has enough capacity by removing old buffers
func (p *BodyBufferPool) ensureCapacity(needed int64) {
	// If we have enough capacity, return
	if atomic.LoadInt64(&p.currentSize)+needed <= p.maxPoolSize {
		return
	}

	// Sort buffers by timestamp (oldest first)
	sort.Slice(p.buffers, func(i, j int) bool {
		return p.buffers[i].timestamp < p.buffers[j].timestamp
	})

	// Remove oldest buffers until we have enough capacity
	for i := 0; i < len(p.buffers) && atomic.LoadInt64(&p.currentSize)+needed > p.maxPoolSize; i++ {
		// Skip if the buffer is already empty
		if p.buffers[i].buffer.Len() == 0 {
			continue
		}

		// Subtract buffer size from pool size
		size := p.buffers[i].size
		atomic.AddInt64(&p.currentSize, -size)

		// Clear buffer to free memory
		p.buffers[i].buffer.Reset()
		p.buffers[i].size = 0
	}
}

// Body represents a captured HTTP request or response body
type Body struct {
	reader           io.ReadCloser // Original body reader
	buffer           *bytes.Buffer // Buffer to capture body
	pool             *BodyBufferPool
	maxSize          int64
	size             int64
	isTruncated      bool
	isFullyCaptured  bool
	mu               sync.RWMutex
	closed           bool
	consumedOriginal bool // True if the original body has been completely read
}

// NewBody creates a new Body wrapper for capturing HTTP body content
func NewBody(rc io.ReadCloser, pool *BodyBufferPool) *Body {
	if rc == nil {
		return nil
	}

	// Get a buffer from the pool
	buf := pool.GetBuffer()

	return &Body{
		reader:           rc,
		buffer:           buf,
		pool:             pool,
		maxSize:          pool.maxBufferSize,
		size:             0,
		isTruncated:      false,
		isFullyCaptured:  false,
		closed:           false,
		consumedOriginal: false,
	}
}

// Read reads from the original body while also capturing to the buffer
func (b *Body) Read(p []byte) (n int, err error) {
	if b == nil || b.reader == nil {
		return 0, io.EOF
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return 0, ErrBodyClosed
	}
	b.mu.Unlock()

	// Read from the original reader
	n, err = b.reader.Read(p)

	if n > 0 {
		b.mu.Lock()
		// Only write to buffer if we haven't exceeded max size
		if b.size < b.maxSize {
			// Determine how much we can write without exceeding max size
			toWrite := n
			if b.size+int64(n) > b.maxSize {
				toWrite = int(b.maxSize - b.size)
				b.isTruncated = true
			}

			if toWrite > 0 {
				// Write to the buffer
				b.buffer.Write(p[:toWrite])
				b.size += int64(toWrite)
			}
		}
		b.mu.Unlock()
	}

	// If EOF, mark as fully consumed
	if err == io.EOF {
		b.mu.Lock()
		b.consumedOriginal = true
		b.mu.Unlock()
	}

	return n, err
}

// Close closes the original body and finalizes the buffer.
// This will attempt to read any unread data from the original body up to the maximum size limit.
func (b *Body) Close() error {
	if b == nil || b.reader == nil {
		return nil
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}

	// Mark as closed before capturing remaining data to avoid potential recursive calls
	b.closed = true

	// Check state to determine if we need to read more data
	fullyConsumed := b.consumedOriginal
	maxSizeReached := b.size >= b.maxSize

	b.mu.Unlock()

	// If the body wasn't fully read and we have room to store more data,
	// read the rest of it into our buffer
	if !fullyConsumed && !maxSizeReached {
		// Create a buffer for reading
		buf := make([]byte, 32*1024) // 32KB chunks

		// Try to read more data
		for {
			var n int
			var readErr error
			n, readErr = b.reader.Read(buf)

			if n > 0 {
				b.mu.Lock()
				// Check if we've exceeded max size since last read
				if b.size < b.maxSize {
					// Determine how much we can write without exceeding max size
					toWrite := n
					if b.size+int64(n) > b.maxSize {
						toWrite = int(b.maxSize - b.size)
						b.isTruncated = true
					}

					if toWrite > 0 {
						// Write to the buffer
						b.buffer.Write(buf[:toWrite])
						b.size += int64(toWrite)
					}

					// If we've reached max size, no need to read more
					maxSizeReached = b.size >= b.maxSize
				} else {
					maxSizeReached = true
				}
				b.mu.Unlock()

				// If we've reached max size, stop reading
				if maxSizeReached {
					break
				}
			}

			if readErr != nil {
				// We've read all we can
				break
			}
		}
	}

	// Now close the original reader - its implementation should handle any cleanup
	err := b.reader.Close()

	// Mark as fully captured
	b.mu.Lock()
	b.isFullyCaptured = true
	b.mu.Unlock()

	// Update pool with final buffer size
	b.pool.RegisterBuffer(b.buffer, b.size)

	return err
}

// String returns the body content as a string
func (b *Body) String() string {
	if b == nil || b.buffer == nil {
		return ""
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.buffer.String()
}

// Bytes returns the body content as a byte slice
func (b *Body) Bytes() []byte {
	if b == nil || b.buffer == nil {
		return nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.buffer.Bytes()
}

// Size returns the captured size of the body
func (b *Body) Size() int64 {
	if b == nil {
		return 0
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.size
}

// IsTruncated returns true if the body was truncated
func (b *Body) IsTruncated() bool {
	if b == nil {
		return false
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.isTruncated
}

// IsFullyCaptured returns true if the body was fully captured
func (b *Body) IsFullyCaptured() bool {
	if b == nil {
		return false
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.isFullyCaptured
}
