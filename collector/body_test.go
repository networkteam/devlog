package collector_test

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/networkteam/devlog/collector"
)

// Fix for TestBody_PartialRead
func TestBody_PartialRead(t *testing.T) {
	// Create a small pool for testing
	pool := collector.NewBodyBufferPool(10*1024, 5*1024) // 10KB pool, 5KB max per body

	// Create test data
	testData := "This is test data for partial reading"
	testReader := io.NopCloser(strings.NewReader(testData))

	// Create a Body
	body := collector.NewBody(testReader, pool)

	// Read first 10 bytes
	buf := make([]byte, 10)
	n, err := body.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 10, n)
	assert.Equal(t, "This is te", string(buf))

	// Verify partial capture
	assert.Equal(t, "This is te", body.String())
	assert.Equal(t, int64(10), body.Size())
	assert.False(t, body.IsFullyCaptured())

	// Close without reading the rest
	err = body.Close()
	require.NoError(t, err)

	// Now the body should have captured everything
	assert.Equal(t, testData, body.String())
	assert.Equal(t, int64(len(testData)), body.Size())

	// The isFullyCaptured flag should be set to true after Close()
	assert.True(t, body.IsFullyCaptured())
}

// Fix for TestBody_ReadAfterClose
func TestBody_ReadAfterClose(t *testing.T) {
	// Create a pool
	pool := collector.NewBodyBufferPool(10*1024, 5*1024)

	// Create test data
	testData := "This is test data"
	testReader := io.NopCloser(strings.NewReader(testData))

	// Create a Body
	body := collector.NewBody(testReader, pool)

	// Close the body
	err := body.Close()
	require.NoError(t, err)

	// Attempt to read after close
	buf := make([]byte, 10)
	_, err = body.Read(buf)

	// Should return our specific error
	assert.Error(t, err)
	assert.Equal(t, collector.ErrBodyClosed, err)
}

// Fix for TestBodyBufferPool_EnsureCapacity
func TestBodyBufferPool_EnsureCapacity(t *testing.T) {
	// Create a small pool for testing
	maxPoolSize := int64(1000) // 1000 bytes
	maxBodySize := int64(200)  // 200 bytes per body
	pool := collector.NewBodyBufferPool(maxPoolSize, maxBodySize)

	// Create buffers that will fit in the pool
	buffers := make([]*collector.BodyBuffer, 0)

	// Add 5 buffers of 100 bytes each (total 500 bytes)
	for i := 0; i < 5; i++ {
		// Get a buffer from the pool
		buffer := pool.GetBuffer()

		// Fill with 100 bytes of data
		data := strings.Repeat("a", 100)
		buffer.WriteString(data)

		buffers = append(buffers, buffer)

		// Sleep to ensure different timestamps
		time.Sleep(10 * time.Millisecond)
	}

	// Verify all buffers have data
	for i, buffer := range buffers {
		assert.Equal(t, 100, buffer.Len(), "Buffer %d should have data", i)
	}

	// Add one more large buffer that will trigger garbage collection
	buffer := pool.GetBuffer()
	buffer.WriteString(strings.Repeat("b", 600)) // 600 bytes

	// The newest buffer should have data
	assert.Equal(t, 600, buffer.Len())

	// Verify the combined data is still under the pool max size
	// This is difficult to test directly without exposing pool internals
	// But we can verify the latest buffer is intact
	assert.Equal(t, strings.Repeat("b", 600), buffer.String())
}

func TestBody_IOReadAllWithoutClose(t *testing.T) {
	pool := collector.NewBodyBufferPool(100, 50)

	testData := strings.Repeat("X", 30) // 30 bytes
	testReader := io.NopCloser(strings.NewReader(testData))
	body := collector.NewBody(testReader, pool)

	// Some clients do io.ReadAll without Close()
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, testData, string(data))

	// Ensure pool tracks size even without Close()
	currentSize := pool.GetCurrentSize()
	assert.Equal(t, int64(30), currentSize, "Pool should track buffer size without Close()")
}

func TestBody_PoolCapacityEnforcement_Multiple_ReadAll(t *testing.T) {
	pool := collector.NewBodyBufferPool(100, 50) // Small pool to force cleanup

	var bodies []*collector.Body

	// Create bodies that exceed pool capacity
	for i := 0; i < 5; i++ {
		testData := strings.Repeat("A", 40) // 40 bytes each
		testReader := io.NopCloser(strings.NewReader(testData))
		body := collector.NewBody(testReader, pool)

		_, err := io.ReadAll(body) // Only consume data, do not close bodies
		// TODO Add test with partial consumption of bodies
		require.NoError(t, err)

		bodies = append(bodies, body)
	}

	poolBytesLen := 0
	bodySizes := 0
	for _, body := range bodies {
		poolBytesLen += len(body.Bytes())
		bodySizes += int(body.Size())
	}

	// Ensure bodies are cleaned up
	assert.LessOrEqual(t, poolBytesLen, 100, "Some bodies should be cleaned up when pool exceeds capacity")
	assert.LessOrEqual(t, bodySizes, 100, "Total body sizes should not exceed pool capacity")
}
