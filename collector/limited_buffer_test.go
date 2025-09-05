package collector_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/networkteam/devlog/collector"
)

func TestLimitedBuffer_WriteWithinLimit(t *testing.T) {
	buffer := collector.NewLimitedBuffer(100)
	data := []byte("hello world") // 11 bytes, well within limit

	n, err := buffer.Write(data)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, string(data), buffer.String())
	assert.Equal(t, len(data), buffer.Len())
	assert.False(t, buffer.IsTruncated())
}

func TestLimitedBuffer_WriteExceedsLimit(t *testing.T) {
	buffer := collector.NewLimitedBuffer(10)
	data := []byte("this is a very long string that exceeds the limit") // 50 bytes > 10 limit

	n, err := buffer.Write(data)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)                  // Should return full length even when truncated
	assert.Equal(t, "this is a ", buffer.String()) // Only first 10 bytes
	assert.Equal(t, 10, buffer.Len())
	assert.True(t, buffer.IsTruncated())
}

func TestLimitedBuffer_WriteExactLimit(t *testing.T) {
	buffer := collector.NewLimitedBuffer(10)
	data := []byte("1234567890") // Exactly 10 bytes

	n, err := buffer.Write(data)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, string(data), buffer.String())
	assert.Equal(t, 10, buffer.Len())
	assert.False(t, buffer.IsTruncated()) // Should NOT be truncated
}

// Debug wrapper to log all Write calls
type debugLimitedBuffer struct {
	*collector.LimitedBuffer
	t *testing.T
}

func (d *debugLimitedBuffer) Write(p []byte) (n int, err error) {
	d.t.Logf("Write called with %d bytes, buffer len before: %d, truncated before: %v",
		len(p), d.LimitedBuffer.Len(), d.LimitedBuffer.IsTruncated())
	n, err = d.LimitedBuffer.Write(p)
	d.t.Logf("Write returned n=%d, err=%v, buffer len after: %d, truncated after: %v",
		n, err, d.LimitedBuffer.Len(), d.LimitedBuffer.IsTruncated())
	return n, err
}

// Check if io.CopyN is using ReadFrom instead of Write
func (d *debugLimitedBuffer) ReadFrom(r io.Reader) (n int64, err error) {
	d.t.Logf("ReadFrom called! This bypasses Write() entirely")
	// Don't call the embedded ReadFrom - force it to use Write instead
	return 0, errors.New("ReadFrom disabled for debugging")
}

// This is the critical test for our PreReadBody use case
func TestLimitedBuffer_CopyNWithLimitPlus1(t *testing.T) {
	buffer := collector.NewLimitedBuffer(100)
	data := strings.Repeat("x", 200) // 200 bytes of data
	reader := strings.NewReader(data)

	t.Log("Starting io.CopyN with limit=100, copying 101 bytes")

	// This is exactly what PreReadBody does: copy limit+1 bytes
	// When ReadFrom fails, io.CopyN should fall back to using Write() method
	n, err := io.CopyN(buffer, reader, int64(101)) // limit+1

	// The behavior when ReadFrom returns an error is that io.CopyN falls back to Write
	// So we should get successful copy but with proper truncation
	require.NoError(t, err)
	assert.Equal(t, int64(101), n) // io.CopyN reports copying 101 bytes

	// Critical assertions: our Write method should have enforced the limit
	t.Logf("Final: Buffer length: %d", buffer.Len())
	t.Logf("Final: Buffer truncated: %v", buffer.IsTruncated())
	t.Logf("Final: Buffer content length: %d", len(buffer.String()))

	// What we EXPECT should happen with the ReadFrom disabled:
	assert.Equal(t, 100, buffer.Len(), "Buffer should contain only 100 bytes")
	assert.True(t, buffer.IsTruncated(), "Buffer should be marked as truncated")
	assert.Equal(t, strings.Repeat("x", 100), buffer.String(), "Buffer should contain first 100 chars")
}

func TestLimitedBuffer_MultipleWrites(t *testing.T) {
	buffer := collector.NewLimitedBuffer(10)

	// First write: 5 bytes
	n, err := buffer.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.False(t, buffer.IsTruncated())

	// Second write: 3 bytes (total 8, still within limit)
	n, err = buffer.Write([]byte(" wo"))
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.False(t, buffer.IsTruncated())

	// Third write: 5 bytes (would make total 13, exceeds limit of 10)
	n, err = buffer.Write([]byte("rld!!"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)                          // Should return full length
	assert.True(t, buffer.IsTruncated())           // Should be truncated
	assert.Equal(t, "hello worl", buffer.String()) // First 10 chars total (8 + 2 from "rld!!")
	assert.Equal(t, 10, buffer.Len())
}

func TestLimitedBuffer_WriteAfterTruncation(t *testing.T) {
	buffer := collector.NewLimitedBuffer(5)

	// First write exceeds limit
	n, err := buffer.Write([]byte("hello world"))
	require.NoError(t, err)
	assert.Equal(t, 11, n)
	assert.True(t, buffer.IsTruncated())
	assert.Equal(t, "hello", buffer.String())

	// Subsequent writes should be ignored
	n, err = buffer.Write([]byte(" more"))
	require.NoError(t, err)
	assert.Equal(t, 5, n) // Returns length as if written
	assert.True(t, buffer.IsTruncated())
	assert.Equal(t, "hello", buffer.String()) // Unchanged
	assert.Equal(t, 5, buffer.Len())
}

func TestLimitedBuffer_ZeroLimit(t *testing.T) {
	buffer := collector.NewLimitedBuffer(0)

	n, err := buffer.Write([]byte("test"))
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.True(t, buffer.IsTruncated())
	assert.Equal(t, "", buffer.String())
	assert.Equal(t, 0, buffer.Len())
}

func TestLimitedBuffer_EmptyWrite(t *testing.T) {
	buffer := collector.NewLimitedBuffer(10)

	n, err := buffer.Write([]byte{})
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.False(t, buffer.IsTruncated())
	assert.Equal(t, "", buffer.String())
	assert.Equal(t, 0, buffer.Len())
}

func TestLimitedBuffer_Reset(t *testing.T) {
	buffer := collector.NewLimitedBuffer(5)

	// Write data that exceeds limit
	n, err := buffer.Write([]byte("hello world"))
	require.NoError(t, err)
	assert.Equal(t, 11, n)
	assert.True(t, buffer.IsTruncated())
	assert.Equal(t, "hello", buffer.String())

	// Reset should clear everything
	buffer.Reset()
	assert.False(t, buffer.IsTruncated())
	assert.Equal(t, "", buffer.String())
	assert.Equal(t, 0, buffer.Len())

	// Should work normally after reset
	n, err = buffer.Write([]byte("new"))
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.False(t, buffer.IsTruncated())
	assert.Equal(t, "new", buffer.String())
}
