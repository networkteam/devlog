package collector_test

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/networkteam/devlog/collector"
)

// CloseDetector is a helper for testing if a reader is closed
type CloseDetector struct {
	Reader io.Reader
	Closed bool
}

func (c *CloseDetector) Read(p []byte) (int, error) {
	return c.Reader.Read(p)
}

func (c *CloseDetector) Close() error {
	c.Closed = true
	return nil
}

func TestBody_ReadAll(t *testing.T) {
	// Create a small pool for testing
	pool := collector.NewBodyBufferPool(10*1024, 5*1024) // 10KB pool, 5KB max per body

	// Create test data
	testData := "This is test data for the body reader"
	testReader := io.NopCloser(strings.NewReader(testData))

	// Create a Body
	body := collector.NewBody(testReader, pool)

	// Read all data from the body
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	// Verify the data was read correctly
	assert.Equal(t, testData, string(data))

	// Verify the body was captured correctly
	assert.Equal(t, testData, body.String())
	assert.Equal(t, int64(len(testData)), body.Size())
	assert.True(t, body.IsFullyCaptured())
	assert.False(t, body.IsTruncated())
}

func TestBody_Close(t *testing.T) {
	// Create a small pool for testing
	pool := collector.NewBodyBufferPool(10*1024, 5*1024) // 10KB pool, 5KB max per body

	// Create a test reader that allows detecting if it's closed
	testReader := &CloseDetector{
		Reader: strings.NewReader("This is test data"),
	}

	// Create a Body with the test reader
	body := collector.NewBody(testReader, pool)

	// Close the body
	err := body.Close()
	require.NoError(t, err)

	// Verify the underlying reader was closed
	assert.True(t, testReader.Closed)
}

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
	assert.True(t, body.IsFullyCaptured())
}

func TestBody_Truncation(t *testing.T) {
	// Create a very small pool for testing
	pool := collector.NewBodyBufferPool(10*1024, 10) // 10KB pool, 10 byte max per body

	// Create test data that exceeds the max body size
	testData := "This is a test string that exceeds the max body size of 10 bytes"
	testReader := io.NopCloser(strings.NewReader(testData))

	// Create a Body
	body := collector.NewBody(testReader, pool)

	// Read all data from the body
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	// Verify the client got all the data
	assert.Equal(t, testData, string(data))

	// Verify the body was truncated in the capture
	assert.Equal(t, "This is a ", body.String()) // Only first 10 bytes
	assert.Equal(t, int64(10), body.Size())
	assert.True(t, body.IsTruncated())
	assert.True(t, body.IsFullyCaptured())
}

func TestBody_GarbageCollection(t *testing.T) {
	// Create a small pool for testing
	maxPoolSize := int64(100) // 100 bytes
	maxBodySize := int64(20)  // 20 bytes per body
	pool := collector.NewBodyBufferPool(maxPoolSize, maxBodySize)

	// Add bodies until we exceed the pool size
	bodies := make([]*collector.Body, 0)

	for i := 0; i < 10; i++ { // This should far exceed our pool size
		data := strings.Repeat(string([]byte{byte('a' + i)}), 20) // 20 bytes of the same letter
		reader := io.NopCloser(strings.NewReader(data))

		body := collector.NewBody(reader, pool)

		// Read all data from the body
		readData, err := io.ReadAll(body)
		require.NoError(t, err)

		// Verify the data was read correctly
		assert.Equal(t, data, string(readData))

		// Close the body to ensure it's registered with the pool
		err = body.Close()
		require.NoError(t, err)

		bodies = append(bodies, body)

		// Sleep a bit to ensure timestamps are different
		time.Sleep(10 * time.Millisecond)
	}

	// Verify that older bodies were garbage collected
	// The most recent bodies should still have their data
	// The oldest bodies should have lost their data

	// The most recent 5 bodies (assuming 5 * 20 bytes = 100 bytes, which is our max pool size)
	for i := 5; i < 10; i++ {
		body := bodies[i]
		expectedData := strings.Repeat(string([]byte{byte('a' + i)}), 20)
		assert.Equal(t, expectedData, body.String(), "Body %d should still have data", i)
	}
}

func TestBody_NilBody(t *testing.T) {
	// Create a pool
	pool := collector.NewBodyBufferPool(10*1024, 5*1024)

	// Create a Body with nil reader
	body := collector.NewBody(nil, pool)

	// Verify it's handled gracefully
	assert.Nil(t, body)

	// These operations should not panic
	var nilBody *collector.Body
	assert.Equal(t, "", nilBody.String())
	assert.Equal(t, int64(0), nilBody.Size())
	assert.False(t, nilBody.IsTruncated())
	assert.False(t, nilBody.IsFullyCaptured())
	assert.Nil(t, nilBody.Bytes())
	assert.Nil(t, nilBody.Close())
}

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

	// Should return an error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already closed")
}

func TestBody_MultipleClose(t *testing.T) {
	// Create a pool
	pool := collector.NewBodyBufferPool(10*1024, 5*1024)

	// Create a test reader that allows detecting if it's closed
	testReader := &CloseDetector{
		Reader: strings.NewReader("This is test data"),
	}

	// Create a Body
	body := collector.NewBody(testReader, pool)

	// Close the body multiple times
	err := body.Close()
	require.NoError(t, err)

	// Second close should be a no-op
	err = body.Close()
	require.NoError(t, err)

	// The underlying reader should only be closed once
	assert.True(t, testReader.Closed)
}

func TestBody_ChunkedReads(t *testing.T) {
	// Create a pool
	pool := collector.NewBodyBufferPool(10*1024, 5*1024)

	// Create test data
	testData := "This is test data for chunked reading. It's long enough to require multiple reads."
	testReader := io.NopCloser(strings.NewReader(testData))

	// Create a Body
	body := collector.NewBody(testReader, pool)

	// Read in small chunks
	buf := make([]byte, 10)
	var result bytes.Buffer

	for {
		n, err := body.Read(buf)
		if n > 0 {
			result.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}

	// Verify all data was read correctly
	assert.Equal(t, testData, result.String())

	// Verify the body was captured correctly
	assert.Equal(t, testData, body.String())
	assert.Equal(t, int64(len(testData)), body.Size())
	assert.True(t, body.IsFullyCaptured())
	assert.False(t, body.IsTruncated())
}

func TestBodyBufferPool_EnsureCapacity(t *testing.T) {
	// Create a small pool for testing
	maxPoolSize := int64(1000) // 1000 bytes
	maxBodySize := int64(200)  // 200 bytes per body
	pool := collector.NewBodyBufferPool(maxPoolSize, maxBodySize)

	// Create 10 bodies that will fit in the pool (10 * 100 = 1000 bytes)
	bodies := make([]*collector.Body, 0)

	for i := 0; i < 10; i++ {
		data := strings.Repeat("a", 100) // 100 bytes
		reader := io.NopCloser(strings.NewReader(data))

		body := collector.NewBody(reader, pool)

		// Read all data
		_, err := io.ReadAll(body)
		require.NoError(t, err)

		// Close the body
		err = body.Close()
		require.NoError(t, err)

		bodies = append(bodies, body)

		// Sleep to ensure different timestamps
		time.Sleep(10 * time.Millisecond)
	}

	// Verify all bodies have data (pool is at capacity)
	for i, body := range bodies {
		assert.Equal(t, 100, len(body.String()), "Body %d should have data", i)
	}

	// Add one more body that will cause the oldest to be garbage collected
	data := strings.Repeat("b", 200) // 200 bytes
	reader := io.NopCloser(strings.NewReader(data))

	// This should trigger garbage collection of at least two oldest bodies (to free 200 bytes)
	body := collector.NewBody(reader, pool)
	_, err := io.ReadAll(body)
	require.NoError(t, err)
	err = body.Close()
	require.NoError(t, err)

	// The newest body should have its data
	assert.Equal(t, 200, len(body.String()))

	// At least the oldest body should have been garbage collected
	// Can't reliably test this in a unit test without exposing internal pool state
}

func TestBody_EmptyBody(t *testing.T) {
	// Create a pool
	pool := collector.NewBodyBufferPool(10*1024, 5*1024)

	// Create an empty body
	emptyReader := io.NopCloser(strings.NewReader(""))

	// Create a Body
	body := collector.NewBody(emptyReader, pool)

	// Read all data
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	// Verify empty data was read correctly
	assert.Equal(t, "", string(data))

	// Verify the body was captured correctly
	assert.Equal(t, "", body.String())
	assert.Equal(t, int64(0), body.Size())
	assert.True(t, body.IsFullyCaptured())
	assert.False(t, body.IsTruncated())
}

func TestBody_Concurrent(t *testing.T) {
	// Create a pool
	pool := collector.NewBodyBufferPool(10*1024, 5*1024)

	// Create test data
	testData := "This is test data for concurrent access"
	testReader := io.NopCloser(strings.NewReader(testData))

	// Create a Body
	body := collector.NewBody(testReader, pool)

	// Concurrently read and access body properties
	done := make(chan bool)

	// Start a goroutine to read the body
	go func() {
		data, err := io.ReadAll(body)
		assert.NoError(t, err)
		assert.Equal(t, testData, string(data))
		done <- true
	}()

	// While reading, access properties from the main goroutine
	for i := 0; i < 10; i++ {
		// These operations should be safe to call concurrently
		_ = body.Size()
		_ = body.IsTruncated()
		_ = body.IsFullyCaptured()
		time.Sleep(5 * time.Millisecond)
	}

	// Wait for read to complete
	<-done

	// Close the body
	err := body.Close()
	require.NoError(t, err)

	// Final verification
	assert.Equal(t, testData, body.String())
	assert.Equal(t, int64(len(testData)), body.Size())
	assert.True(t, body.IsFullyCaptured())
	assert.False(t, body.IsTruncated())
}
