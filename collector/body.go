package collector

import (
	"bytes"
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

// PreReadBody creates a new Body wrapper that immediately pre-reads data from the body.
// This ensures body content is captured even if the underlying connection is closed early.
// It returns a Body with an io.MultiReader that combines the pre-read buffer with the original reader.
func PreReadBody(rc io.ReadCloser, limit int) *Body {
	if rc == nil {
		return NewBody(rc, limit)
	}

	b := &Body{}

	var preReadBuffer = new(bytes.Buffer)

	// Pre-read up to limit bytes into our capture buffer
	n, err := io.CopyN(preReadBuffer, rc, int64(limit)+1) // +1 to check for truncation

	truncated := n > int64(limit)

	if err == io.EOF {
		// We've read everything (body was smaller than limit).
		b.consumedOriginal = true
		b.isFullyCaptured = !truncated
	}

	multiReader := io.MultiReader(preReadBuffer, rc)

	// Wrap in a readCloser to maintain the Close capability
	b.reader = &preReadBodyWrapper{
		Reader: multiReader,
		closer: rc,
	}

	// Set up the buffer with pre-read data but only up to the limit
	preReadBytes := preReadBuffer.Bytes()
	if len(preReadBytes) > limit {
		preReadBytes = preReadBytes[:limit]
	}
	b.buffer = &LimitedBuffer{
		Buffer:    bytes.NewBuffer(preReadBytes),
		limit:     limit,
		truncated: truncated,
	}

	return b
}

// preReadBodyWrapper wraps an io.Reader with Close functionality
type preReadBodyWrapper struct {
	io.Reader
	closer io.Closer
}

func (w *preReadBodyWrapper) Close() error {
	if w.closer != nil {
		return w.closer.Close()
	}
	return nil
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

	// Only write to buffer if it's not a preReadBodyWrapper
	// (preReadBodyWrapper means we already captured the data in PreReadBody)
	if n > 0 {
		if _, isPreRead := b.reader.(*preReadBodyWrapper); !isPreRead {
			_, _ = b.buffer.Write(p[:n])
		}
	}

	// If EOF, mark as fully consumed
	if err == io.EOF {
		b.consumedOriginal = true
		b.isFullyCaptured = !b.buffer.IsTruncated()

		// Remove original body
		b.reader = nil
	}

	return n, err
}

// Close closes the original body and finalizes the buffer.
func (b *Body) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b == nil || b.reader == nil {
		return nil
	}

	if b.closed {
		return nil
	}

	// Mark as closed
	b.closed = true

	// For PreReadBody cases (identified by preReadBodyWrapper),
	// the data is already captured, just close
	if _, isPreRead := b.reader.(*preReadBodyWrapper); isPreRead {
		return b.reader.Close()
	}

	// For legacy NewBody usage (when not using PreReadBody),
	// we still need to try to read remaining data
	if !b.consumedOriginal {
		_, _ = io.Copy(b.buffer, b.reader)
	}

	// Close the original reader
	err := b.reader.Close()

	if !b.buffer.IsTruncated() {
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
func (b *Body) Size() int64 {
	if b == nil || b.buffer == nil {
		return 0
	}

	return int64(b.buffer.Len())
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
