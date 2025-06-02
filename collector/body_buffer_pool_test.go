package collector_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/networkteam/devlog/collector"
)

func TestBodyBufferPool_GetBuffer(t *testing.T) {
	// Create a pool
	pool := collector.NewBodyBufferPool(10*1024, 5*1024)

	// Get a buffer
	buffer := pool.GetBuffer()

	// Verify we got a buffer
	assert.NotNil(t, buffer)
	assert.Equal(t, 0, buffer.Len())

	// Write to the buffer
	buffer.WriteString("test data")
	assert.Equal(t, 9, buffer.Len())
}

func TestBodyBufferPool_GarbageCollection(t *testing.T) {
	// Create a small pool for testing
	pool := collector.NewBodyBufferPool(50, 20) // 50 byte pool, 20 bytes max per buffer

	// Add buffers until we exceed the pool size
	var buffers []*collector.BodyBuffer

	// Add 3 buffers (20 bytes each, total 60 bytes which exceeds pool size)
	for i := 0; i < 3; i++ {
		buffer := pool.GetBuffer()
		buffer.WriteString(string(make([]byte, 20))) // 20 bytes of zero values
		buffers = append(buffers, buffer)

		// Sleep to ensure different timestamps
		time.Sleep(10 * time.Millisecond)
	}

	// Add one more buffer which should trigger garbage collection
	buffer := pool.GetBuffer()
	buffer.WriteString(string(make([]byte, 20))) // 20 bytes of zero values

	// Verify the newest buffers still have data
	// The exact behavior depends on the internal implementation details,
	// but at minimum the newest buffer should have its data
	assert.Equal(t, 20, buffer.Len(), "Newest buffer should still have data")
}

func TestBodyBufferPool_MaxBufferSize(t *testing.T) {
	// Create a pool with a small max buffer size
	maxBufferSize := int64(10)
	pool := collector.NewBodyBufferPool(100, maxBufferSize)

	// Get a buffer
	buffer := pool.GetBuffer()

	// Write data that exceeds max buffer size
	data := make([]byte, 20)
	for i := 0; i < 20; i++ {
		data[i] = byte('a' + i%26)
	}
	buffer.Write(data)

	// The buffer should contain all the data we wrote
	// (The max buffer size only applies when capturing via Body)
	assert.Equal(t, 20, buffer.Len())
}

func TestBodyBufferPool_MultiplePools(t *testing.T) {
	// Create multiple pools with different settings
	pool1 := collector.NewBodyBufferPool(100, 20)
	pool2 := collector.NewBodyBufferPool(200, 40)

	// Get buffers from each pool
	buffer1 := pool1.GetBuffer()
	buffer2 := pool2.GetBuffer()

	// Write different data to each
	buffer1.WriteString("data for pool 1")
	buffer2.WriteString("data for pool 2")

	// Each buffer should have its own data
	assert.Equal(t, "data for pool 1", buffer1.String())
	assert.Equal(t, "data for pool 2", buffer2.String())
}

func TestBodyBufferPool_ZeroSize(t *testing.T) {
	// Create pools with zero or negative sizes
	// Implementation should handle this gracefully

	// Zero max pool size
	pool1 := collector.NewBodyBufferPool(0, 10)
	buffer1 := pool1.GetBuffer()
	require.NotNil(t, buffer1)

	// Zero max buffer size
	pool2 := collector.NewBodyBufferPool(100, 0)
	buffer2 := pool2.GetBuffer()
	require.NotNil(t, buffer2)

	// Negative sizes (should be treated as zero or have a minimum size)
	pool3 := collector.NewBodyBufferPool(-100, -10)
	buffer3 := pool3.GetBuffer()
	require.NotNil(t, buffer3)
}

func TestBodyBufferPool_ConcurrentAccess(t *testing.T) {
	// Create a pool
	pool := collector.NewBodyBufferPool(1000, 100)

	// Number of concurrent goroutines
	numGoroutines := 10

	// Channel to signal completion
	done := make(chan bool)

	// Start goroutines to access the pool concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			// Each goroutine gets and registers multiple buffers
			for j := 0; j < 5; j++ {
				buffer := pool.GetBuffer()

				// Write some unique data
				buffer.WriteString(string(make([]byte, 50))) // 50 bytes

				// Small sleep to increase chance of concurrency issues
				time.Sleep(time.Duration(id) * time.Millisecond)
			}

			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// If we get here without panic or deadlock, the test passes
}

func TestBodyBufferPool_LargeNumberOfBuffers(t *testing.T) {
	// Skip in short mode as this test can be time-consuming
	if testing.Short() {
		t.Skip("Skipping large buffer test in short mode")
	}

	// Create a pool with a large capacity
	pool := collector.NewBodyBufferPool(10*1024*1024, 1024) // 10MB pool, 1KB per buffer

	// Create a large number of small buffers
	numBuffers := 1000
	buffers := make([]*collector.BodyBuffer, numBuffers)

	for i := 0; i < numBuffers; i++ {
		buffer := pool.GetBuffer()

		// Write a small amount of data
		buffer.WriteString("test")

		buffers[i] = buffer
	}

	// The most recent buffers should still have data
	for i := numBuffers - 10; i < numBuffers; i++ {
		assert.Equal(t, "test", buffers[i].String(), "Recent buffer %d should have data", i)
	}
}
