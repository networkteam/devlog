package collector

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Constants for body capture
const (
	// DefaultBodyBufferPoolSize is the default size of the buffer pool (max size of all bodies combined to store)
	DefaultBodyBufferPoolSize = 100 * 1024 * 1024 // 100MB

	// DefaultMaxBodySize is the default maximum size for a single body
	DefaultMaxBodySize = 1 * 1024 * 1024 // 1MB
)

var (
	// ErrBodyClosed is returned when attempting to read from a closed body
	ErrBodyClosed = errors.New("body is already closed")
)

// BodyBufferPool manages byte buffers for capturing HTTP bodies
type BodyBufferPool struct {
	pool          sync.Pool
	maxPoolSize   int64
	currentSize   int64
	reservedSpace int64
	activeBuffers []*BodyBuffer
	maxBufferSize int64

	mu sync.Mutex
}

// BodyBuffer represents a captured body in the pool
type BodyBuffer struct {
	*bytes.Buffer
	timestamp int64 // unix timestamp when created
	finished  bool  // true if the buffer has been finalized (closed or completely read)
}

// NewBodyBufferPool creates a new buffer pool for body capturing
func NewBodyBufferPool(maxPoolSize, maxBufferSize int64) *BodyBufferPool {
	// Ensure positive sizes
	if maxPoolSize <= 0 {
		maxPoolSize = DefaultBodyBufferPoolSize
	}
	if maxBufferSize <= 0 {
		maxBufferSize = DefaultMaxBodySize
	}

	return &BodyBufferPool{
		pool: sync.Pool{
			New: func() any {
				return new(bytes.Buffer)
			},
		},
		maxPoolSize:   maxPoolSize,
		currentSize:   0,
		activeBuffers: make([]*BodyBuffer, 0),
		maxBufferSize: maxBufferSize,
	}
}

// GetBuffer returns a new buffer from the pool
func (p *BodyBufferPool) GetBuffer() *BodyBuffer {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Ensure pool has capacity by removing old buffers if needed
	p.ensureCapacity()

	// Reserve space for the new buffer until it is fully consumed / closed and we know the actual size
	p.reservedSpace += p.maxBufferSize

	// Create a new buffer
	buf := &BodyBuffer{
		Buffer:    p.pool.Get().(*bytes.Buffer),
		timestamp: time.Now().Unix(),
	}

	// Track this buffer
	p.activeBuffers = append(p.activeBuffers, buf)

	return buf
}

// ensureCapacity ensures the pool has enough capacity by removing old buffers
func (p *BodyBufferPool) ensureCapacity() {
	// If we have enough capacity, return
	needed := p.maxBufferSize
	if p.currentSize+p.reservedSpace+needed <= p.maxPoolSize {
		return
	}

	// Remove oldest buffers until we have enough capacity
	removedBuffers := 0
	for i := 0; i < len(p.activeBuffers) && p.currentSize+p.reservedSpace+needed > p.maxPoolSize; i++ {
		buf := p.activeBuffers[i]

		// Skip if the buffer is already empty or not finished
		if buf.Buffer == nil || !buf.finished || buf.Len() == 0 {
			continue
		}

		size := int64(buf.Len())
		// Subtract buffer size from pool size
		p.currentSize -= size

		// Remove buffer reference by putting it back to the sync.Pool and removing the reference
		buf.Buffer.Reset()
		p.pool.Put(buf.Buffer)
		buf.Buffer = nil
		removedBuffers++
	}

	// Remove nil buffers from the slice
	newBuffers := make([]*BodyBuffer, 0, len(p.activeBuffers)-removedBuffers)
	for _, buf := range p.activeBuffers {
		if buf.Buffer == nil {
			continue
		}
		newBuffers = append(newBuffers, buf)
	}
	p.activeBuffers = newBuffers
}

func (p *BodyBufferPool) GetCurrentSize() int64 {
	return atomic.LoadInt64(&p.currentSize)
}

// trackBodySize updates pool size with the captured size and remove reserved space
func (p *BodyBufferPool) trackBodySize(size int64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.currentSize += size
	p.reservedSpace -= p.maxBufferSize
}

// Body represents a captured HTTP request or response body
type Body struct {
	reader           io.ReadCloser // Original body reader
	buffer           *BodyBuffer   // Buffer to capture body
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

func (b *Body) Read(p []byte) (n int, err error) {
	if b == nil || b.reader == nil {
		return 0, io.EOF
	}

	if b.closed {
		return 0, ErrBodyClosed
	}

	// Read from the original reader
	n, err = b.reader.Read(p)

	if n > 0 {
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

			if b.isTruncated {
				b.buffer.finished = true
				b.pool.trackBodySize(b.size)
			}
		}
	}

	// If EOF, mark as fully consumed
	if err == io.EOF {
		if !b.isTruncated {
			b.consumedOriginal = true
			b.buffer.finished = true
			b.pool.trackBodySize(b.size)
		}

		// Remove original body
		b.reader = nil
	}

	return n, err
}

// Close closes the original body and finalizes the buffer.
// This will attempt to read any unread data from the original body up to the maximum size limit.
func (b *Body) Close() error {
	if b == nil || b.reader == nil {
		return nil
	}

	if b.closed {
		return nil
	}

	// Mark as closed before capturing remaining data to avoid potential recursive calls
	b.closed = true

	// Check state to determine if we need to read more data
	fullyConsumed := b.consumedOriginal
	maxSizeReached := b.size >= b.maxSize

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

	if !b.isTruncated {
		// Mark as fully captured
		b.isFullyCaptured = true
	}

	// Check if we have already finished reading the body (i.e. by calling Read to EOF)
	if !b.buffer.finished {
		b.pool.trackBodySize(b.size)
		b.buffer.finished = true
	}

	return err
}

// String returns the body content as a string
func (b *Body) String() string {
	if b == nil || b.buffer.Buffer == nil {
		return ""
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.buffer.Buffer.String()
}

// Bytes returns the body content as a byte slice
func (b *Body) Bytes() []byte {
	if b == nil || b.buffer.Buffer == nil {
		return nil
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.buffer.Buffer.Bytes()
}

// Size returns the captured size of the body
func (b *Body) Size() int64 {
	if b == nil || b.buffer.Buffer == nil {
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
