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

func TestHTTPClientCollector_BasicCapture(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"Hello, World!"}`))
	}))
	defer server.Close()

	// Create a collector
	httpCollector := collector.NewHTTPClientCollector(10)

	// Create a client with the collector's transport
	client := &http.Client{
		Transport: httpCollector.Transport(nil),
	}

	// Make a request
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, `{"message":"Hello, World!"}`, string(body))

	// Get the captured requests
	requests := httpCollector.GetRequests(10)
	require.Len(t, requests, 1)

	// Verify the captured request
	req := requests[0]
	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, server.URL, req.URL)
	assert.Equal(t, http.StatusOK, req.StatusCode)
	assert.Equal(t, "application/json", req.ResponseHeaders.Get("Content-Type"))
	assert.NotNil(t, req.ResponseBody)
	assert.Equal(t, `{"message":"Hello, World!"}`, req.ResponseBody.String())
	assert.True(t, req.ResponseBody.IsFullyCaptured())
	assert.False(t, req.ResponseBody.IsTruncated())
}

func TestHTTPClientCollector_PostRequest(t *testing.T) {
	// Create a test server that echoes back the request body
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
	defer server.Close()

	// Create a collector
	httpCollector := collector.NewHTTPClientCollector(10)

	// Create a client with the collector's transport
	client := &http.Client{
		Transport: httpCollector.Transport(nil),
	}

	// Request payload
	payload := []byte(`{"name":"John","age":30}`)

	// Create the request
	req, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	// Make the request
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, string(payload), string(body))

	// Get the captured requests
	requests := httpCollector.GetRequests(10)
	require.Len(t, requests, 1)

	// Verify the captured request
	captured := requests[0]
	assert.Equal(t, http.MethodPost, captured.Method)
	assert.Equal(t, server.URL, captured.URL)
	assert.Equal(t, http.StatusOK, captured.StatusCode)
	assert.Equal(t, "application/json", captured.RequestHeaders.Get("Content-Type"))
	assert.Equal(t, "application/json", captured.ResponseHeaders.Get("Content-Type"))

	// Verify request and response bodies were captured
	assert.NotNil(t, captured.RequestBody)
	assert.Equal(t, string(payload), captured.RequestBody.String())
	assert.True(t, captured.RequestBody.IsFullyCaptured())

	assert.NotNil(t, captured.ResponseBody)
	assert.Equal(t, string(payload), captured.ResponseBody.String())
	assert.True(t, captured.ResponseBody.IsFullyCaptured())
}

func TestHTTPClientCollector_ErrorResponse(t *testing.T) {
	// Create a test server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"Internal Server Error"}`))
	}))
	defer server.Close()

	// Create a collector
	httpCollector := collector.NewHTTPClientCollector(10)

	// Create a client with the collector's transport
	client := &http.Client{
		Transport: httpCollector.Transport(nil),
	}

	// Make a request
	resp, err := client.Get(server.URL)
	require.NoError(t, err) // Note: HTTP errors don't cause Go errors
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, `{"error":"Internal Server Error"}`, string(body))

	// Get the captured requests
	requests := httpCollector.GetRequests(10)
	require.Len(t, requests, 1)

	// Verify the captured request
	req := requests[0]
	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, server.URL, req.URL)
	assert.Equal(t, http.StatusInternalServerError, req.StatusCode)
	assert.Equal(t, "application/json", req.ResponseHeaders.Get("Content-Type"))
	assert.Nil(t, req.Error) // HTTP errors (500, etc.) don't cause Go errors
	assert.NotNil(t, req.ResponseBody)
	assert.Equal(t, `{"error":"Internal Server Error"}`, req.ResponseBody.String())
}

func TestHTTPClientCollector_RequestError(t *testing.T) {
	// Use a non-existent server URL to provoke a connection error
	nonExistentURL := "http://localhost:1" // Port 1 is not typically in use

	// Create a collector
	httpCollector := collector.NewHTTPClientCollector(10)

	// Create a client with the collector's transport and a short timeout
	client := &http.Client{
		Transport: httpCollector.Transport(nil),
		Timeout:   100 * time.Millisecond, // Short timeout to fail quickly
	}

	// Make a request that should fail
	_, err := client.Get(nonExistentURL)
	require.Error(t, err)

	// Wait a bit for the async processing to complete
	time.Sleep(50 * time.Millisecond)

	// Get the captured requests
	requests := httpCollector.GetRequests(10)
	require.Len(t, requests, 1)

	// Verify the captured request
	req := requests[0]
	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, nonExistentURL, req.URL)
	assert.NotNil(t, req.Error)
	assert.Contains(t, req.Error.Error(), "connection refused")
}

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
	// Don't read the body, just close it
	resp.Body.Close()

	// Get the captured requests
	requests := httpCollector.GetRequests(10)
	require.Len(t, requests, 1)

	// Verify the captured request - the collector should have read the body even though the application didn't
	req := requests[0]
	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, server.URL, req.URL)
	assert.Equal(t, http.StatusOK, req.StatusCode)
	assert.NotNil(t, req.ResponseBody)

	// Verify the body was captured even though it wasn't read by the client
	assert.Equal(t, largeResponse, req.ResponseBody.String())
	assert.True(t, req.ResponseBody.IsFullyCaptured())
}

func TestHTTPClientCollector_LargeResponseBodyTruncation(t *testing.T) {
	// Create a test server with a large response exceeding default max body size
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Generate a response larger than the default max body size (1MB)
		largeResponse := strings.Repeat("a", 2*1024*1024) // 2MB
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(largeResponse)))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(largeResponse))
	}))
	defer server.Close()

	// Create a collector with default options (which has a 1MB max body size)
	httpCollector := collector.NewHTTPClientCollector(10)

	// Create a client with the collector's transport
	client := &http.Client{
		Transport: httpCollector.Transport(nil),
	}

	// Make a request
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read the full response body (application consuming it)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, 2*1024*1024, len(body))

	// Get the captured requests
	requests := httpCollector.GetRequests(10)
	require.Len(t, requests, 1)

	// Verify the captured request
	req := requests[0]
	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, server.URL, req.URL)
	assert.Equal(t, http.StatusOK, req.StatusCode)
	assert.NotNil(t, req.ResponseBody)

	// The body should have been truncated to max size (1MB)
	assert.True(t, req.ResponseBody.IsTruncated())
	assert.Equal(t, int64(1*1024*1024), req.ResponseBody.Size()) // 1MB

	// Verify the captured portion matches the start of the original response
	assert.Equal(t, string(body[:1*1024*1024]), req.ResponseBody.String())
}

func TestHTTPClientCollector_StreamingResponse(t *testing.T) {
	// Create a test server that streams a response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
	defer server.Close()

	// Create a collector
	httpCollector := collector.NewHTTPClientCollector(10)

	// Create a client with the collector's transport
	client := &http.Client{
		Transport: httpCollector.Transport(nil),
	}

	// Make a request
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read the response body in chunks
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

	// Get the captured requests
	requests := httpCollector.GetRequests(10)
	require.Len(t, requests, 1)

	// Verify the captured request
	req := requests[0]
	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, server.URL, req.URL)
	assert.Equal(t, http.StatusOK, req.StatusCode)
	assert.NotNil(t, req.ResponseBody)

	// The collector should have captured the full response
	assert.Equal(t, expectedResponse, req.ResponseBody.String())
	assert.True(t, req.ResponseBody.IsFullyCaptured())
}

func TestHTTPClientCollector_CustomOptions(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, World!"))
	}))
	defer server.Close()

	// Create a collector with custom options
	options := collector.DefaultHTTPClientOptions()
	options.CaptureRequestBody = false  // Disable request body capture
	options.CaptureResponseBody = false // Disable response body capture

	httpCollector := collector.NewHTTPClientCollectorWithOptions(10, options)

	// Create a client with the collector's transport
	client := &http.Client{
		Transport: httpCollector.Transport(nil),
	}

	// Create a request with a body
	reqBody := strings.NewReader("Request payload")
	req, err := http.NewRequest(http.MethodPost, server.URL, reqBody)
	require.NoError(t, err)

	// Make the request
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", string(body))

	// Get the captured requests
	requests := httpCollector.GetRequests(10)
	require.Len(t, requests, 1)

	// Verify the captured request
	capturedReq := requests[0]
	assert.Equal(t, http.MethodPost, capturedReq.Method)
	assert.Equal(t, server.URL, capturedReq.URL)
	assert.Equal(t, http.StatusOK, capturedReq.StatusCode)

	// Since we disabled body capturing, these should be nil
	assert.Nil(t, capturedReq.RequestBody)
	assert.Nil(t, capturedReq.ResponseBody)
}

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

	// And they should be the most recent 5 (requests 5-9)
	for i := 0; i < 5; i++ {
		expectedPath := fmt.Sprintf("/request/%d", i+5)
		assert.Equal(t, server.URL+expectedPath, requests[4-i].URL)
		assert.Equal(t, expectedPath, requests[4-i].ResponseBody.String())
	}
}

func TestHTTPClientCollector_JsonRequest(t *testing.T) {
	// Create a test server that handles JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Parse the request JSON
		var data map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&data)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Add a field to the response
		data["processed"] = true

		// Send the response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(data)
	}))
	defer server.Close()

	// Create a collector
	httpCollector := collector.NewHTTPClientCollector(10)

	// Create a client with the collector's transport
	client := &http.Client{
		Transport: httpCollector.Transport(nil),
	}

	// Create a JSON request
	reqData := map[string]interface{}{
		"name": "John Doe",
		"age":  30,
		"addresses": []string{
			"123 Main St",
			"456 Elm St",
		},
	}

	reqBody, err := json.Marshal(reqData)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(reqBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	// Make the request
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read the response body
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Parse the response JSON
	var respData map[string]interface{}
	err = json.Unmarshal(respBody, &respData)
	require.NoError(t, err)

	// Verify the response
	assert.Equal(t, "John Doe", respData["name"])
	assert.Equal(t, float64(30), respData["age"])
	assert.Equal(t, true, respData["processed"])

	// Get the captured requests
	requests := httpCollector.GetRequests(10)
	require.Len(t, requests, 1)

	// Verify the captured request
	capturedReq := requests[0]
	assert.Equal(t, http.MethodPost, capturedReq.Method)
	assert.Equal(t, server.URL, capturedReq.URL)
	assert.Equal(t, http.StatusOK, capturedReq.StatusCode)
	assert.Equal(t, "application/json", capturedReq.RequestHeaders.Get("Content-Type"))
	assert.Equal(t, "application/json", capturedReq.ResponseHeaders.Get("Content-Type"))

	// Verify the request body was captured correctly
	var capturedReqData map[string]interface{}
	err = json.Unmarshal([]byte(capturedReq.RequestBody.String()), &capturedReqData)
	require.NoError(t, err)
	assert.Equal(t, "John Doe", capturedReqData["name"])
	assert.Equal(t, float64(30), capturedReqData["age"])

	// Verify the response body was captured correctly
	var capturedRespData map[string]interface{}
	err = json.Unmarshal([]byte(capturedReq.ResponseBody.String()), &capturedRespData)
	require.NoError(t, err)
	assert.Equal(t, "John Doe", capturedRespData["name"])
	assert.Equal(t, float64(30), capturedRespData["age"])
	assert.Equal(t, true, capturedRespData["processed"])
}
