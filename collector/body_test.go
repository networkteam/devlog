package collector_test

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/networkteam/devlog/collector"
)

// Fix for TestBody_PartialRead
func TestBody_PartialRead(t *testing.T) {
	// Create test data
	testData := "This is test data for partial reading"
	testReader := io.NopCloser(strings.NewReader(testData))

	// Create a Body
	body := collector.NewBody(testReader, 100)

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
	// Create test data
	testData := "This is test data"
	testReader := io.NopCloser(strings.NewReader(testData))

	// Create a Body
	body := collector.NewBody(testReader, 100)

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

// Test PreReadBody with small body that handler doesn't read
func TestPreReadBody_SmallBodyUnread(t *testing.T) {
	data := "small test data"
	reader := io.NopCloser(strings.NewReader(data))

	body := collector.PreReadBody(reader, 1024) // limit > data size

	// Close without reading
	err := body.Close()
	require.NoError(t, err)

	// Captured data should be available
	assert.Equal(t, data, body.String())
	assert.Equal(t, []byte(data), body.Bytes())
	assert.Equal(t, int64(len(data)), body.Size())
	assert.True(t, body.IsFullyCaptured())
	assert.False(t, body.IsTruncated())
}

// Test PreReadBody with small body that handler fully reads
func TestPreReadBody_SmallBodyRead(t *testing.T) {
	data := "small test data for reading"
	reader := io.NopCloser(strings.NewReader(data))

	body := collector.PreReadBody(reader, 1024) // limit > data size

	// Handler reads the entire body
	handlerData, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, data, string(handlerData))

	// Close the body
	err = body.Close()
	require.NoError(t, err)

	// Captured data should STILL be available after reading + closing
	assert.Equal(t, data, body.String())
	assert.Equal(t, []byte(data), body.Bytes())
	assert.Equal(t, int64(len(data)), body.Size())
	assert.True(t, body.IsFullyCaptured())
	assert.False(t, body.IsTruncated())
}

// Test PreReadBody with large body that handler doesn't read
func TestPreReadBody_LargeBodyUnread(t *testing.T) {
	data := strings.Repeat("x", 2000) // Large data
	reader := io.NopCloser(strings.NewReader(data))

	body := collector.PreReadBody(reader, 100) // limit < data size

	// Close without reading
	err := body.Close()
	require.NoError(t, err)

	// Only first 100 bytes should be captured
	expectedCaptured := data[:100]
	assert.Equal(t, expectedCaptured, body.String())
	assert.Equal(t, []byte(expectedCaptured), body.Bytes())
	assert.Equal(t, int64(100), body.Size())
	assert.False(t, body.IsFullyCaptured())
	assert.True(t, body.IsTruncated())
}

// Test PreReadBody with large body that handler fully reads - CRITICAL TEST
func TestPreReadBody_LargeBodyRead(t *testing.T) {
	data := strings.Repeat("y", 2000) // Large data
	reader := io.NopCloser(strings.NewReader(data))

	body := collector.PreReadBody(reader, 100) // limit < data size

	// Handler reads the entire body (pre-read + remaining)
	handlerData, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, data, string(handlerData)) // Handler should get full data

	// Close the body
	err = body.Close()
	require.NoError(t, err)

	// Captured portion should STILL be available after reading
	expectedCaptured := data[:100]
	assert.Equal(t, expectedCaptured, body.String())
	assert.Equal(t, []byte(expectedCaptured), body.Bytes())
	assert.Equal(t, int64(100), body.Size())
	assert.False(t, body.IsFullyCaptured()) // Only partial capture
	assert.True(t, body.IsTruncated())
}

// Test closing behavior - close without reading small body
func TestPreReadBody_CloseWithoutReading_SmallBody(t *testing.T) {
	data := "test data"
	reader := io.NopCloser(strings.NewReader(data))

	body := collector.PreReadBody(reader, 1024)

	// Close immediately without any reading
	err := body.Close()
	require.NoError(t, err)

	// Captured data should be preserved
	assert.Equal(t, data, body.String())
	assert.True(t, body.IsFullyCaptured())
}

// Test closing behavior - close without reading large body
func TestPreReadBody_CloseWithoutReading_LargeBody(t *testing.T) {
	data := strings.Repeat("z", 500)
	reader := io.NopCloser(strings.NewReader(data))

	body := collector.PreReadBody(reader, 100)

	// Close immediately without any reading
	err := body.Close()
	require.NoError(t, err)

	// Captured portion should be preserved
	expectedCaptured := data[:100]
	assert.Equal(t, expectedCaptured, body.String())
	assert.True(t, body.IsTruncated())
}

// Test closing behavior - close after partial reading
func TestPreReadBody_CloseAfterPartialReading(t *testing.T) {
	data := strings.Repeat("a", 500)
	reader := io.NopCloser(strings.NewReader(data))

	body := collector.PreReadBody(reader, 100)

	// Handler reads only part of the body
	buf := make([]byte, 50)
	n, err := body.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 50, n)

	// Close the body
	err = body.Close()
	require.NoError(t, err)

	// Captured data should still be available
	expectedCaptured := data[:100]
	assert.Equal(t, expectedCaptured, body.String())
	assert.True(t, body.IsTruncated())
}

// Test double close safety
func TestPreReadBody_DoubleClose(t *testing.T) {
	data := "test data"
	reader := io.NopCloser(strings.NewReader(data))

	body := collector.PreReadBody(reader, 1024)

	// First close
	err := body.Close()
	require.NoError(t, err)

	// Second close should be safe
	err = body.Close()
	require.NoError(t, err)

	// Captured data should remain available
	assert.Equal(t, data, body.String())
}

// Test read after close
func TestPreReadBody_ReadAfterClose(t *testing.T) {
	data := "test data"
	reader := io.NopCloser(strings.NewReader(data))

	body := collector.PreReadBody(reader, 1024)

	// Close first
	err := body.Close()
	require.NoError(t, err)

	// Try to read after close
	buf := make([]byte, 10)
	_, err = body.Read(buf)

	// Should return ErrBodyClosed
	assert.Error(t, err)
	assert.Equal(t, collector.ErrBodyClosed, err)

	// Captured data should still be accessible
	assert.Equal(t, data, body.String())
	assert.Equal(t, []byte(data), body.Bytes())
}
