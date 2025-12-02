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
	assert.Equal(t, uint64(10), body.Size())
	assert.False(t, body.IsFullyCaptured())

	// Close without reading the rest
	err = body.Close()
	require.NoError(t, err)

	// Now the body should have captured everything
	assert.Equal(t, testData, body.String())
	assert.Equal(t, uint64(len(testData)), body.Size())

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
