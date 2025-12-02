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

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/networkteam/devlog/collector"
)

func TestHTTPServerCollector_BasicRequest(t *testing.T) {
	// Create a server collector
	serverCollector := collector.NewHTTPServerCollector()

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

	// Start collecting before making request
	collect := Collect(t, serverCollector.Subscribe)

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

	requests := collect.Stop()

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
	assert.Equal(t, uint64(13), serverReq.ResponseSize) // "Hello, World!" is 13 bytes
}

// Test for GET request with a body to ensure we capture it correctly
func TestHTTPServerCollector_GetRequestWithBody(t *testing.T) {
	// Create a server collector
	serverCollector := collector.NewHTTPServerCollector()

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

	// Start collecting before making request
	collect := Collect(t, serverCollector.Subscribe)

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

	requests := collect.Stop()

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
	serverCollector := collector.NewHTTPServerCollector()

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

	// Start collecting before making request
	collect := Collect(t, serverCollector.Subscribe)

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

	requests := collect.Stop()

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
	// Test a variety of status codes
	statusCodes := []int{200, 201, 204, 400, 401, 403, 404, 500, 503}

	for _, statusCode := range statusCodes {
		t.Run(fmt.Sprintf("StatusCode_%d", statusCode), func(t *testing.T) {
			// Create a server collector for each subtest
			serverCollector := collector.NewHTTPServerCollector()

			// Create a handler that returns different status codes based on the path
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/status/") {
					w.WriteHeader(http.StatusNotFound)
					return
				}

				// Extract status code from path
				statusCodeStr := strings.TrimPrefix(r.URL.Path, "/status/")
				var sc int
				if _, err := fmt.Sscanf(statusCodeStr, "%d", &sc); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}

				w.WriteHeader(sc)
				w.Write([]byte(fmt.Sprintf("Status: %d", sc)))
			})

			// Wrap the handler with our collector
			wrappedHandler := serverCollector.Middleware(handler)

			// Create a test server
			server := httptest.NewServer(wrappedHandler)
			defer server.Close()

			// Start collecting before making request
			collect := Collect(t, serverCollector.Subscribe)

			// Create a client
			client := &http.Client{}

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

			requests := collect.Stop()
			require.Len(t, requests, 1, "Should have collected request with status code %d", statusCode)
			assert.Equal(t, statusCode, requests[0].StatusCode)
		})
	}
}

func TestHTTPServerCollector_LargeResponseBody(t *testing.T) {
	// Create a server collector with a small max body size to test truncation
	options := collector.DefaultHTTPServerOptions()
	options.MaxBodySize = 100 // 100 bytes max
	serverCollector := collector.NewHTTPServerCollectorWithOptions(options)

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

	// Start collecting before making request
	collect := Collect(t, serverCollector.Subscribe)

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

	requests := collect.Stop()

	// Verify the request details
	serverReq := requests[0]
	assert.Equal(t, http.MethodGet, serverReq.Method)
	assert.Equal(t, "/large", serverReq.Path)
	assert.Equal(t, http.StatusOK, serverReq.StatusCode)

	// Verify the response body was captured but truncated
	assert.NotNil(t, serverReq.ResponseBody)
	assert.Equal(t, uint64(100), serverReq.ResponseBody.Size()) // Should be truncated to 100 bytes
	assert.True(t, serverReq.ResponseBody.IsTruncated())
	assert.Equal(t, strings.Repeat("abcdefghij", 10), serverReq.ResponseBody.String())
}

func TestHTTPServerCollector_SkipPaths(t *testing.T) {
	// Create a server collector with path skipping
	options := collector.DefaultHTTPServerOptions()
	options.SkipPaths = []string{"/skip/", "/assets/"}
	serverCollector := collector.NewHTTPServerCollectorWithOptions(options)

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

	// Start collecting before making requests
	collect := Collect(t, serverCollector.Subscribe)

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

	// Collect all the requests
	requests := collect.Stop()

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
	serverCollector := collector.NewHTTPServerCollector()

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

	// Start collecting before making request
	collect := Collect(t, serverCollector.Subscribe)

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

	requests := collect.Stop()

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
	serverCollector := collector.NewHTTPServerCollectorWithOptions(options)

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

	// Start collecting before making request
	collect := Collect(t, serverCollector.Subscribe)

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

	requests := collect.Stop()

	// Verify the request details
	serverReq := requests[0]
	assert.Equal(t, http.MethodPost, serverReq.Method)
	assert.Equal(t, "/api/test", serverReq.Path)
	assert.Equal(t, http.StatusOK, serverReq.StatusCode)

	// Bodies should be nil since capturing was disabled
	assert.Nil(t, serverReq.RequestBody)
	assert.Nil(t, serverReq.ResponseBody)
}

func TestHTTPServerCollector_UnreadRequestBodyCapture(t *testing.T) {
	// This test verifies that request bodies are captured even when handlers don't read them

	testCases := []struct {
		name           string
		path           string
		expectedStatus int
		body           string
		contentType    string
	}{
		{
			name:           "404_with_form_data",
			path:           "/nonexistent",
			expectedStatus: http.StatusNotFound,
			body:           "foo=bar&name=test&data=important",
			contentType:    "application/x-www-form-urlencoded",
		},
		{
			name:           "404_with_json_data",
			path:           "/missing",
			expectedStatus: http.StatusNotFound,
			body:           `{"important":"data","should":"be_captured"}`,
			contentType:    "application/json",
		},
		{
			name:           "200_with_unread_data",
			path:           "/exists",
			expectedStatus: http.StatusOK,
			body:           "handler=doesnt&read=this&but=should&capture=it",
			contentType:    "application/x-www-form-urlencoded",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a fresh collector for each subtest
			serverCollector := collector.NewHTTPServerCollector()

			mux := http.NewServeMux()
			mux.HandleFunc("/exists", func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("OK"))
			})

			// Wrap the handler with our collector
			wrappedHandler := serverCollector.Middleware(mux)

			// Create a test server
			server := httptest.NewServer(wrappedHandler)
			defer server.Close()

			// Start collecting before making request
			collect := Collect(t, serverCollector.Subscribe)

			// Create a client and make a POST request with a body that won't be read
			client := &http.Client{}
			req, err := http.NewRequest(http.MethodPost, server.URL+tc.path, strings.NewReader(tc.body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", tc.contentType)

			// Send the request
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			// Verify expected status
			assert.Equal(t, tc.expectedStatus, resp.StatusCode)

			requests := collect.Stop()

			// Get the request (should be the only one)
			serverReq := requests[0]

			// Verify the request details
			assert.Equal(t, http.MethodPost, serverReq.Method)
			assert.Equal(t, tc.path, serverReq.Path)
			assert.Equal(t, tc.expectedStatus, serverReq.StatusCode)
			assert.Equal(t, tc.contentType, serverReq.RequestHeaders.Get("Content-Type"))

			assert.NotNil(t, serverReq.RequestBody, "Request body should be captured even when handler doesn't read it")
			if serverReq.RequestBody != nil {
				capturedBody := serverReq.RequestBody.String()
				assert.Equal(t, tc.body, capturedBody, "Should capture the exact body content even when unread")
				assert.True(t, serverReq.RequestBody.IsFullyCaptured(), "Body should be marked as fully captured")
				assert.Equal(t, uint64(len(tc.body)), serverReq.RequestBody.Size(), "Should capture the full body size")
			}
		})
	}
}

func TestHTTPServerCollector_MultipleHandlers(t *testing.T) {
	// Create a server collector
	serverCollector := collector.NewHTTPServerCollector()

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

	// Start collecting before making requests
	collect := Collect(t, serverCollector.Subscribe)

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

	// Get all requests
	requests := collect.Stop()
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

func TestHTTPServerCollector_WithEventAggregator_GlobalMode(t *testing.T) {
	// Create an EventAggregator with a GlobalMode storage
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	aggregator.RegisterStorage(storage)

	// Create a server collector with the EventAggregator
	options := collector.DefaultHTTPServerOptions()
	options.EventAggregator = aggregator
	serverCollector := collector.NewHTTPServerCollectorWithOptions(options)

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

	// Make a request (without session cookie - GlobalMode should capture anyway)
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, server.URL+"/test", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", string(body))

	// Verify the event was captured in the storage
	events := storage.GetEvents(10)
	require.Len(t, events, 1)

	// The event data should be an HTTPServerRequest
	httpReq, ok := events[0].Data.(collector.HTTPServerRequest)
	require.True(t, ok, "Event data should be HTTPServerRequest")
	assert.Equal(t, http.MethodGet, httpReq.Method)
	assert.Equal(t, "/test", httpReq.Path)
	assert.Equal(t, http.StatusOK, httpReq.StatusCode)
}

func TestHTTPServerCollector_WithEventAggregator_SessionMode_NoMatch(t *testing.T) {
	// Create an EventAggregator with a SessionMode storage
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeSession)
	aggregator.RegisterStorage(storage)

	// Create a server collector with the EventAggregator
	options := collector.DefaultHTTPServerOptions()
	options.EventAggregator = aggregator
	serverCollector := collector.NewHTTPServerCollectorWithOptions(options)

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

	// Make a request without a session cookie (SessionMode should NOT capture)
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, server.URL+"/test", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", string(body))

	// Verify no events were captured (session doesn't match)
	events := storage.GetEvents(10)
	assert.Len(t, events, 0)
}

func TestHTTPServerCollector_WithEventAggregator_SessionMode_Match(t *testing.T) {
	// Create an EventAggregator with a SessionMode storage
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeSession)
	aggregator.RegisterStorage(storage)

	// Create a server collector with the EventAggregator
	options := collector.DefaultHTTPServerOptions()
	options.EventAggregator = aggregator
	serverCollector := collector.NewHTTPServerCollectorWithOptions(options)

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

	// Make a request WITH the session cookie (SessionMode should capture)
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, server.URL+"/test", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{
		Name:  collector.SessionCookiePrefix + sessionID.String(),
		Value: "1",
	})

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", string(body))

	// Verify the event was captured
	events := storage.GetEvents(10)
	require.Len(t, events, 1)

	// The event data should be an HTTPServerRequest
	httpReq, ok := events[0].Data.(collector.HTTPServerRequest)
	require.True(t, ok, "Event data should be HTTPServerRequest")
	assert.Equal(t, http.MethodGet, httpReq.Method)
	assert.Equal(t, "/test", httpReq.Path)
}

func TestHTTPServerCollector_WithEventAggregator_NoStorage(t *testing.T) {
	// Create an EventAggregator with NO storage registered
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	// Create a server collector with the EventAggregator
	options := collector.DefaultHTTPServerOptions()
	options.EventAggregator = aggregator
	serverCollector := collector.NewHTTPServerCollectorWithOptions(options)

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

	// Start collecting before making request
	collect := Collect(t, serverCollector.Subscribe)

	// Make a request (no storage means ShouldCapture returns false, early bailout)
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, server.URL+"/test", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", string(body))

	// Small delay to ensure any notification would have been received
	time.Sleep(10 * time.Millisecond)

	// Verify no requests were captured (early bailout should prevent capture)
	requests := collect.Stop()
	assert.Len(t, requests, 0)
}
