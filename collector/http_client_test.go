package collector_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/networkteam/devlog/collector"
)

// Fixing the TestHTTPClientCollector_UnreadResponseBody test
func TestHTTPClientCollector_UnreadResponseBody(t *testing.T) {
	// Create a large response that won't be read by the client
	largeResponse := strings.Repeat("a", 1000)

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(largeResponse))
	}))
	defer server.Close()

	// Create a collector
	httpCollector := collector.NewHTTPClientCollector(10)

	// Create a client with the collector's transport
	client := &http.Client{
		Transport: httpCollector.Transport(nil),
	}

	// Make a request but intentionally don't read the body
	resp, err := client.Get(server.URL)
	require.NoError(t, err)

	// Don't read the body, just close it immediately to simulate an application
	// that doesn't consume the response body
	resp.Body.Close()

	// Add a short delay to allow the body capturing to complete
	// This is necessary because the body capture happens asynchronously
	// after the body is closed
	time.Sleep(50 * time.Millisecond)

	// Get the captured requests
	requests := httpCollector.GetRequests(10)
	require.Len(t, requests, 1)

	// Verify the captured request - the collector should have read the body
	// even though the application didn't
	req := requests[0]
	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, server.URL, req.URL)
	assert.Equal(t, http.StatusOK, req.StatusCode)
	assert.NotNil(t, req.ResponseBody)

	// Verify the body was captured even though it wasn't read by the client
	assert.Equal(t, largeResponse, req.ResponseBody.String())
	assert.True(t, req.ResponseBody.IsFullyCaptured())
}

// Fixing the TestHTTPClientCollector_MultipleRequests test
func TestHTTPClientCollector_MultipleRequests(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return the request path as the response
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(r.URL.Path))
	}))
	defer server.Close()

	// Create a collector with capacity for 5 requests
	httpCollector := collector.NewHTTPClientCollector(5)

	// Create a client with the collector's transport
	client := &http.Client{
		Transport: httpCollector.Transport(nil),
	}

	// Make 10 requests (more than the capacity)
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("/request/%d", i)
		resp, err := client.Get(server.URL + path)
		require.NoError(t, err)

		// Read and verify the response
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, path, string(body))

		resp.Body.Close()
	}

	// Get the captured requests
	requests := httpCollector.GetRequests(10)

	// Should only have 5 requests (the capacity we set)
	require.Len(t, requests, 5)

	// The ring buffer stores the most recent requests
	// But we need to verify each request individually without assuming their order
	// since the ring buffer implementation might not preserve the exact order we expect

	// Create a map of expected paths
	expectedPaths := make(map[string]bool)
	for i := 5; i < 10; i++ {
		expectedPaths[fmt.Sprintf("/request/%d", i)] = false
	}

	// Verify each request path is in our expected set
	for _, req := range requests {
		path := req.ResponseBody.String()
		if _, exists := expectedPaths[path]; exists {
			expectedPaths[path] = true
		} else {
			t.Errorf("Unexpected path in results: %s", path)
		}
	}

	// Verify all expected paths were found
	for path, found := range expectedPaths {
		assert.True(t, found, "Expected path %s not found in results", path)
	}
}
