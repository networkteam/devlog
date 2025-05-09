package collector_test

import (
	"bytes"
	"encoding/json"
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

func TestHTTPServerCollector_BasicRequest(t *testing.T) {
	// Create a server collector
	serverCollector := collector.NewHTTPServerCollector(100)

	// Create a simple handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, World!"))
	})

	// Wrap the handler with our collector
	wrappedHandler := serverCollector.Middleware(handler)

	// Create a test server
	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	// Create a client and make a request
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, server.URL+"/test", nil)
	require.NoError(t, err)
	req.Header.Set("X-Test-Header", "test-value")

	// Send the request
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", string(body))

	// Get the collected requests
	requests := serverCollector.GetRequests(10)
	require.Len(t, requests, 1)

	// Verify the request details
	serverReq := requests[0]
	assert.Equal(t, http.MethodGet, serverReq.Method)
	assert.Equal(t, "/test", serverReq.Path)
	assert.Equal(t, http.StatusOK, serverReq.StatusCode)
	assert.Equal(t, "test-value", serverReq.RequestHeaders.Get("X-Test-Header"))
	assert.Equal(t, "text/plain", serverReq.ResponseHeaders.Get("Content-Type"))

	// Standard GET request with nil body should have nil RequestBody
	assert.Nil(t, serverReq.RequestBody, "Standard GET request should not have a captured request body")

	// Response body should be captured
	assert.NotNil(t, serverReq.ResponseBody)
	assert.Equal(t, "Hello, World!", serverReq.ResponseBody.String())
	assert.True(t, serverReq.Duration() > 0)
	assert.Equal(t, int64(13), serverReq.ResponseSize) // "Hello, World!" is 13 bytes
}

// Test for GET request with a body to ensure we capture it correctly
func TestHTTPServerCollector_GetRequestWithBody(t *testing.T) {
	// Create a server collector
	serverCollector := collector.NewHTTPServerCollector(100)

	// Create a handler that reads the request body even for GET
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Confirm we're dealing with a GET
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Echo the request body back with a prefix
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("You sent: " + string(body)))
	})

	// Wrap the handler with our collector
	wrappedHandler := serverCollector.Middleware(handler)

	// Create a test server
	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	// Create a client and make a GET request WITH a body
	client := &http.Client{}
	requestBody := "This is a GET request with a body"
	req, err := http.NewRequest(http.MethodGet, server.URL+"/get-with-body", strings.NewReader(requestBody))
	require.NoError(t, err)

	// Send the request
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read the response body
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "You sent: "+requestBody, string(respBody))

	// Get the collected requests
	requests := serverCollector.GetRequests(10)
	require.Len(t, requests, 1)

	// Verify the request details
	serverReq := requests[0]
	assert.Equal(t, http.MethodGet, serverReq.Method)
	assert.Equal(t, "/get-with-body", serverReq.Path)
	assert.Equal(t, http.StatusOK, serverReq.StatusCode)

	// GET request with a body should have the body captured
	assert.NotNil(t, serverReq.RequestBody, "GET request with body should have captured the request body")
	assert.Equal(t, requestBody, serverReq.RequestBody.String())

	// Response body should be captured
	assert.NotNil(t, serverReq.ResponseBody)
	assert.Equal(t, "You sent: "+requestBody, serverReq.ResponseBody.String())
}

func TestHTTPServerCollector_PostRequestWithBody(t *testing.T) {
	// Create a server collector
	serverCollector := collector.NewHTTPServerCollector(100)

	// Create a handler that echoes the request body
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body) // Echo back the request body
	})

	// Wrap the handler with our collector
	wrappedHandler := serverCollector.Middleware(handler)

	// Create a test server
	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	// Prepare request body (JSON)
	requestBody := map[string]interface{}{
		"name":  "John Doe",
		"email": "john@example.com",
		"age":   30,
	}
	jsonBody, err := json.Marshal(requestBody)
	require.NoError(t, err)

	// Create a client and make a request
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/users", bytes.NewReader(jsonBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	// Send the request
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read the response body
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, string(jsonBody), string(respBody))

	// Get the collected requests
	requests := serverCollector.GetRequests(10)
	require.Len(t, requests, 1)

	// Verify the request details
	serverReq := requests[0]
	assert.Equal(t, http.MethodPost, serverReq.Method)
	assert.Equal(t, "/api/users", serverReq.Path)
	assert.Equal(t, http.StatusOK, serverReq.StatusCode)
	assert.Equal(t, "application/json", serverReq.RequestHeaders.Get("Content-Type"))
	assert.Equal(t, "application/json", serverReq.ResponseHeaders.Get("Content-Type"))

	// Verify request body was captured
	assert.NotNil(t, serverReq.RequestBody)
	var capturedRequest map[string]interface{}
	err = json.Unmarshal([]byte(serverReq.RequestBody.String()), &capturedRequest)
	require.NoError(t, err)
	assert.Equal(t, "John Doe", capturedRequest["name"])
	assert.Equal(t, "john@example.com", capturedRequest["email"])
	assert.Equal(t, float64(30), capturedRequest["age"])

	// Verify response body was captured
	assert.NotNil(t, serverReq.ResponseBody)
	var capturedResponse map[string]interface{}
	err = json.Unmarshal([]byte(serverReq.ResponseBody.String()), &capturedResponse)
	require.NoError(t, err)
	assert.Equal(t, "John Doe", capturedResponse["name"])
	assert.Equal(t, "john@example.com", capturedResponse["email"])
	assert.Equal(t, float64(30), capturedResponse["age"])
}

func TestHTTPServerCollector_DifferentStatusCodes(t *testing.T) {
	// Create a server collector
	serverCollector := collector.NewHTTPServerCollector(100)

	// Create a handler that returns different status codes based on the path
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/status/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Extract status code from path
		statusCodeStr := strings.TrimPrefix(r.URL.Path, "/status/")
		var statusCode int
		if _, err := fmt.Sscanf(statusCodeStr, "%d", &statusCode); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(statusCode)
		w.Write([]byte(fmt.Sprintf("Status: %d", statusCode)))
	})

	// Wrap the handler with our collector
	wrappedHandler := serverCollector.Middleware(handler)

	// Create a test server
	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	// Create a client
	client := &http.Client{}

	// Test a variety of status codes
	statusCodes := []int{200, 201, 204, 400, 401, 403, 404, 500, 503}

	for _, statusCode := range statusCodes {
		t.Run(fmt.Sprintf("StatusCode_%d", statusCode), func(t *testing.T) {
			// Make request for this status code
			url := fmt.Sprintf("%s/status/%d", server.URL, statusCode)
			req, err := http.NewRequest(http.MethodGet, url, nil)
			require.NoError(t, err)

			// Send the request, allowing all status codes
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			// Verify the response
			assert.Equal(t, statusCode, resp.StatusCode)

			// Read the body even for status codes that typically don't have bodies
			if statusCode != 204 {
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				assert.Equal(t, fmt.Sprintf("Status: %d", statusCode), string(body))
			}

			// Get the collected requests
			requests := serverCollector.GetRequests(uint64(len(statusCodes) + 1))

			// Find the request for this status code
			var foundRequest *collector.HTTPServerRequest
			for i := range requests {
				if requests[i].StatusCode == statusCode && requests[i].Path == fmt.Sprintf("/status/%d", statusCode) {
					foundRequest = &requests[i]
					break
				}
			}

			require.NotNil(t, foundRequest, "Should have collected request with status code %d", statusCode)
			assert.Equal(t, statusCode, foundRequest.StatusCode)
		})
	}
}

func TestHTTPServerCollector_LargeResponseBody(t *testing.T) {
	// Create a server collector with a small max body size to test truncation
	options := collector.DefaultHTTPServerOptions()
	options.MaxBodySize = 100 // 100 bytes max
	serverCollector := collector.NewHTTPServerCollectorWithOptions(100, options)

	// Create a handler that returns a large response body
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)

		// Generate a 10 KB response body
		largeBody := strings.Repeat("abcdefghij", 1000) // 10K characters
		w.Write([]byte(largeBody))
	})

	// Wrap the handler with our collector
	wrappedHandler := serverCollector.Middleware(handler)

	// Create a test server
	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	// Create a client and make a request
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, server.URL+"/large", nil)
	require.NoError(t, err)

	// Send the request
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, 10000, len(body)) // 10 KB

	// Get the collected requests
	requests := serverCollector.GetRequests(10)
	require.Len(t, requests, 1)

	// Verify the request details
	serverReq := requests[0]
	assert.Equal(t, http.MethodGet, serverReq.Method)
	assert.Equal(t, "/large", serverReq.Path)
	assert.Equal(t, http.StatusOK, serverReq.StatusCode)

	// Verify the response body was captured but truncated
	assert.NotNil(t, serverReq.ResponseBody)
	assert.Equal(t, int64(100), serverReq.ResponseBody.Size()) // Should be truncated to 100 bytes
	assert.True(t, serverReq.ResponseBody.IsTruncated())
	assert.Equal(t, strings.Repeat("abcdefghij", 10), serverReq.ResponseBody.String())
}

func TestHTTPServerCollector_SkipPaths(t *testing.T) {
	// Create a server collector with path skipping
	options := collector.DefaultHTTPServerOptions()
	options.SkipPaths = []string{"/skip/", "/assets/"}
	serverCollector := collector.NewHTTPServerCollectorWithOptions(100, options)

	// Create a simple handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Response from " + r.URL.Path))
	})

	// Wrap the handler with our collector
	wrappedHandler := serverCollector.Middleware(handler)

	// Create a test server
	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	// Create a client
	client := &http.Client{}

	// Test cases for different paths
	testCases := []struct {
		path      string
		shouldLog bool
	}{
		{"/api/users", true},
		{"/skip/this", false},
		{"/assets/style.css", false},
		{"/api/products", true},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Path_%s", tc.path), func(t *testing.T) {
			// Make request for this path
			url := server.URL + tc.path
			req, err := http.NewRequest(http.MethodGet, url, nil)
			require.NoError(t, err)

			// Send the request
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			// Verify the response
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, "Response from "+tc.path, string(body))
		})
	}

	// Get the collected requests
	requests := serverCollector.GetRequests(10)

	// Should only have collected the non-skipped paths
	assert.Equal(t, 2, len(requests), "Should have collected exactly 2 requests (the non-skipped ones)")

	// Verify we have the expected paths and don't have the skipped ones
	capturedPaths := make(map[string]bool)
	for _, req := range requests {
		capturedPaths[req.Path] = true
	}

	assert.True(t, capturedPaths["/api/users"], "Should have captured /api/users")
	assert.True(t, capturedPaths["/api/products"], "Should have captured /api/products")
	assert.False(t, capturedPaths["/skip/this"], "Should not have captured /skip/this")
	assert.False(t, capturedPaths["/assets/style.css"], "Should not have captured /assets/style.css")
}

func TestHTTPServerCollector_StreamingResponse(t *testing.T) {
	// Create a server collector
	serverCollector := collector.NewHTTPServerCollector(100)

	// Create a handler that streams a response
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support Flush")
		}

		// Send data in chunks
		for i := 0; i < 5; i++ {
			fmt.Fprintf(w, "Chunk %d\n", i)
			flusher.Flush()
			time.Sleep(50 * time.Millisecond)
		}
	})

	// Wrap the handler with our collector
	wrappedHandler := serverCollector.Middleware(handler)

	// Create a test server
	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	// Create a client and make a request
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, server.URL+"/streaming", nil)
	require.NoError(t, err)

	// Send the request
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read the response body chunk by chunk
	var receivedData strings.Builder
	buf := make([]byte, 64)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			receivedData.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}

	// Expected response
	expectedResponse := "Chunk 0\nChunk 1\nChunk 2\nChunk 3\nChunk 4\n"
	assert.Equal(t, expectedResponse, receivedData.String())

	// Get the collected requests
	requests := serverCollector.GetRequests(10)
	require.Len(t, requests, 1)

	// Verify the request details
	serverReq := requests[0]
	assert.Equal(t, http.MethodGet, serverReq.Method)
	assert.Equal(t, "/streaming", serverReq.Path)
	assert.Equal(t, http.StatusOK, serverReq.StatusCode)

	// Verify the response body was captured
	assert.NotNil(t, serverReq.ResponseBody)
	assert.Equal(t, expectedResponse, serverReq.ResponseBody.String())
}

func TestHTTPServerCollector_DisabledBodyCapture(t *testing.T) {
	// Create a server collector with body capture disabled
	options := collector.DefaultHTTPServerOptions()
	options.CaptureRequestBody = false
	options.CaptureResponseBody = false
	serverCollector := collector.NewHTTPServerCollectorWithOptions(100, options)

	// Create a simple POST handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the request body (should still work)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body) // Echo back the request body
	})

	// Wrap the handler with our collector
	wrappedHandler := serverCollector.Middleware(handler)

	// Create a test server
	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	// Prepare request body
	requestBody := `{"message":"This body should not be captured"}`

	// Create a client and make a request
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/test", strings.NewReader(requestBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	// Send the request
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read the response body
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, requestBody, string(respBody))

	// Get the collected requests
	requests := serverCollector.GetRequests(10)
	require.Len(t, requests, 1)

	// Verify the request details
	serverReq := requests[0]
	assert.Equal(t, http.MethodPost, serverReq.Method)
	assert.Equal(t, "/api/test", serverReq.Path)
	assert.Equal(t, http.StatusOK, serverReq.StatusCode)

	// Bodies should be nil since capturing was disabled
	assert.Nil(t, serverReq.RequestBody)
	assert.Nil(t, serverReq.ResponseBody)
}

func TestHTTPServerCollector_RingBufferCapacity(t *testing.T) {
	// Create a server collector with a small capacity
	capacity := uint64(3)
	serverCollector := collector.NewHTTPServerCollector(capacity)

	// Create a simple handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Path: " + r.URL.Path))
	})

	// Wrap the handler with our collector
	wrappedHandler := serverCollector.Middleware(handler)

	// Create a test server
	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	// Create a client
	client := &http.Client{}

	// Make 5 requests (more than our capacity)
	for i := 0; i < 5; i++ {
		path := fmt.Sprintf("/test-%d", i)
		req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		require.NoError(t, err)

		// Send the request
		resp, err := client.Do(req)
		require.NoError(t, err)

		// Verify the response
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "Path: "+path, string(body))

		resp.Body.Close()
	}

	// Get the collected requests
	requests := serverCollector.GetRequests(10)

	// Should only have collected the most recent 3 requests
	assert.Equal(t, int(capacity), len(requests), "Should have collected exactly %d requests (limited by capacity)", capacity)

	// Verify we have the most recent requests
	expectedPaths := make(map[string]bool)
	for i := 2; i < 5; i++ { // Should have requests 2, 3, and 4
		expectedPaths[fmt.Sprintf("/test-%d", i)] = false
	}

	for _, req := range requests {
		if _, exists := expectedPaths[req.Path]; exists {
			expectedPaths[req.Path] = true
		} else {
			t.Errorf("Unexpected path in results: %s", req.Path)
		}
	}

	// All expected paths should have been found
	for path, found := range expectedPaths {
		assert.True(t, found, "Expected path %s not found in results", path)
	}
}

func TestHTTPServerCollector_MultipleHandlers(t *testing.T) {
	// Create a server collector
	serverCollector := collector.NewHTTPServerCollector(100)

	// Create handlers for different routes
	mux := http.NewServeMux()

	// API handler
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","path":"` + r.URL.Path + `"}`))
	})

	// Web handler
	mux.HandleFunc("/web/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body><h1>" + r.URL.Path + "</h1></body></html>"))
	})

	// Wrap the mux with our collector
	wrappedHandler := serverCollector.Middleware(mux)

	// Create a test server
	server := httptest.NewServer(wrappedHandler)
	defer server.Close()

	// Create a client
	client := &http.Client{}

	// Test both handlers
	testCases := []struct {
		path         string
		contentType  string
		bodyContains string
	}{
		{"/api/users", "application/json", `{"status":"ok","path":"/api/users"}`},
		{"/web/index", "text/html", "<html><body><h1>/web/index</h1></body></html>"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Path_%s", tc.path), func(t *testing.T) {
			// Make request for this path
			url := server.URL + tc.path
			req, err := http.NewRequest(http.MethodGet, url, nil)
			require.NoError(t, err)

			// Send the request
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			// Verify the response
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, tc.contentType, resp.Header.Get("Content-Type"))

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, tc.bodyContains, string(body))
		})
	}

	// Get the collected requests
	requests := serverCollector.GetRequests(10)
	assert.Equal(t, 2, len(requests), "Should have collected 2 requests")

	// Verify we have both paths
	capturedPaths := make(map[string]bool)
	for _, req := range requests {
		capturedPaths[req.Path] = true

		// Verify content types were captured correctly
		if req.Path == "/api/users" {
			assert.Equal(t, "application/json", req.ResponseHeaders.Get("Content-Type"))
		} else if req.Path == "/web/index" {
			assert.Equal(t, "text/html", req.ResponseHeaders.Get("Content-Type"))
		}
	}

	assert.True(t, capturedPaths["/api/users"], "Should have captured /api/users")
	assert.True(t, capturedPaths["/web/index"], "Should have captured /web/index")
}
