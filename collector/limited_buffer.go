package collector

import (
	"bytes"
)

// LimitedBuffer is a buffer that only writes up to a certain size
// and marks itself as truncated if the size is exceeded.
type LimitedBuffer struct {
	*bytes.Buffer
	limit     int
	truncated bool
}

// NewLimitedBuffer creates a new LimitedBuffer with the given size limit.
func NewLimitedBuffer(limit int) *LimitedBuffer {
	return &LimitedBuffer{
		Buffer: new(bytes.Buffer),
		limit:  limit,
	}
}

// Write implements io.Writer interface.
// It writes data to the buffer up to the limit and marks the buffer as truncated
// if the limit is exceeded.
func (b *LimitedBuffer) Write(p []byte) (n int, err error) {
	if b.truncated {
		return len(p), nil
	}

	remaining := b.limit - b.Buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}

	if len(p) > remaining {
		n, err = b.Buffer.Write(p[:remaining])
		b.truncated = true
		return len(p), err
	}

	return b.Buffer.Write(p)
}

// IsTruncated returns true if the buffer was truncated due to size limit.
func (b *LimitedBuffer) IsTruncated() bool {
	return b.truncated
}

// Reset resets the buffer to be empty and not truncated.
func (b *LimitedBuffer) Reset() {
	b.Buffer.Reset()
	b.truncated = false
}

// String returns the contents of the buffer as a string.
// If the buffer was truncated, it will not include the truncated data.
func (b *LimitedBuffer) String() string {
	return b.Buffer.String()
}

// Bytes returns a slice of length b.Len() holding the unread portion of the buffer.
// If the buffer was truncated, it will not include the truncated data.
func (b *LimitedBuffer) Bytes() []byte {
	return b.Buffer.Bytes()
}
