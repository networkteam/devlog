package collector

import (
	"errors"
	"io"
	"sync"
)

// Constants for body capture
const (
	// DefaultMaxBodySize is the default maximum size for a single body
	DefaultMaxBodySize = 1 * 1024 * 1024 // 1MB
)

var (
	// ErrBodyClosed is returned when attempting to read from a closed body
	ErrBodyClosed = errors.New("body is already closed")
)

// Body represents a captured HTTP request or response body
type Body struct {
	reader           io.ReadCloser  // Original body reader
	buffer           *LimitedBuffer // Buffer to capture body
	isFullyCaptured  bool
	mu               sync.RWMutex
	closed           bool
	consumedOriginal bool // True if the original body has been completely read
}

// NewBody creates a new Body wrapper for capturing HTTP body content.
// If the provided io.ReadCloser is nil, the body is considered fully captured (it is explicitly written to).
func NewBody(rc io.ReadCloser, limit int) *Body {
	b := &Body{
		reader: rc,
		buffer: NewLimitedBuffer(limit),
	}
	return b
}

func (b *Body) Read(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b == nil || b.reader == nil {
		return 0, io.EOF
	}

	if b.closed {
		return 0, ErrBodyClosed
	}

	// Read from the original reader
	n, err = b.reader.Read(p)

	if n > 0 {
		b.buffer.Write(p[:n])
	}

	// If EOF, mark as fully consumed
	if err == io.EOF {
		b.consumedOriginal = true

		// Remove original body
		b.reader = nil
	}

	return n, err
}

// Close closes the original body and finalizes the buffer.
// This will attempt to read any unread data from the original body up to the maximum size limit.
func (b *Body) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

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

	// If the body wasn't fully read, read the rest of it into our buffer
	if !fullyConsumed {
		// Create a buffer for reading
		buf := make([]byte, 32*1024) // 32KB chunks

		// Try to read more data
		for {
			var n int
			var readErr error
			n, readErr = b.reader.Read(buf)

			if n > 0 {
				b.buffer.Write(buf[:n])
			}

			if readErr != nil {
				// We've read all we can
				break
			}
		}
	}

	// Now close the original reader - its implementation should handle any cleanup
	err := b.reader.Close()

	if !b.buffer.IsTruncated() {
		// Mark as fully captured
		b.isFullyCaptured = true
	}

	return err
}

// String returns the body content as a string
func (b *Body) String() string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b == nil || b.buffer == nil {
		return ""
	}

	return b.buffer.String()
}

// Bytes returns the body content as a byte slice
func (b *Body) Bytes() []byte {
	if b == nil || b.buffer == nil {
		return nil
	}

	return b.buffer.Bytes()
}

// Size returns the captured size of the body
func (b *Body) Size() uint64 {
	if b == nil || b.buffer == nil {
		return 0
	}

	return uint64(b.buffer.Len())
}

// IsTruncated returns true if the body was truncated
func (b *Body) IsTruncated() bool {
	if b == nil {
		return false
	}

	return b.buffer.IsTruncated()
}

// IsFullyCaptured returns true if the body was fully captured
func (b *Body) IsFullyCaptured() bool {
	if b == nil {
		return false
	}

	return b.isFullyCaptured
}
