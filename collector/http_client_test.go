package collector_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/networkteam/devlog/collector"
)

func TestHTTPClientCollector_UnreadResponseBody(t *testing.T) {
	// Create a smaller response to make debugging easier
	largeResponse := strings.Repeat("a", 100)

	// Create a server that tracks whether the body was read
	bodyReadTracker := &BodyReadTracker{data: largeResponse}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(largeResponse)))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(largeResponse))
		bodyReadTracker.serverWroteBody = true
	}))
	defer server.Close()

	// Create a collector with specific options for testing
	options := collector.DefaultHTTPClientOptions()
	options.MaxBodySize = 1024 // Ensure it's large enough for our test data
	httpCollector := collector.NewHTTPClientCollectorWithOptions(options)

	// Start collecting before making request
	collect := Collect(t, httpCollector.Subscribe)

	// Create a client with the collector's transport
	client := &http.Client{
		Transport: httpCollector.Transport(nil),
	}

	// Make a request but intentionally don't read the body
	resp, err := client.Get(server.URL)
	require.NoError(t, err)

	// Verify we got a response
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, strconv.Itoa(len(largeResponse)), resp.Header.Get("Content-Length"))

	// Don't read the body, just close it immediately to simulate an application
	// that doesn't consume the response body
	resp.Body.Close()

	requests := collect.Stop()

	// Verify the captured request details
	req := requests[0]
	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, server.URL, req.URL)
	assert.Equal(t, http.StatusOK, req.StatusCode)
	assert.NotNil(t, req.ResponseBody)

	// Verify the body was captured even though it wasn't read by the client
	captured := req.ResponseBody.String()
	assert.Equal(t, largeResponse, captured)
	assert.True(t, req.ResponseBody.IsFullyCaptured())
}

// BodyReadTracker tracks if a response body was read
type BodyReadTracker struct {
	data            string
	serverWroteBody bool
	clientReadBody  bool
}

func (b *BodyReadTracker) Read(p []byte) (int, error) {
	if len(p) > len(b.data) {
		copy(p, b.data)
		b.clientReadBody = true
		return len(b.data), io.EOF
	}

	copy(p, b.data[:len(p)])
	b.data = b.data[len(p):]
	b.clientReadBody = true
	return len(p), nil
}

func (b *BodyReadTracker) Close() error {
	return nil
}
