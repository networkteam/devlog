package collector

import (
	"net/http"
	"sync"
	"time"

	"github.com/gofrs/uuid"
)

// HTTPRequest represents a captured HTTP request/response pair
type HTTPRequest struct {
	ID              uuid.UUID
	Method          string
	URL             string
	RequestTime     time.Time
	ResponseTime    time.Time
	StatusCode      int
	RequestSize     int64
	ResponseSize    int64
	RequestHeaders  http.Header
	ResponseHeaders http.Header
	RequestBody     *Body
	ResponseBody    *Body
	Error           error
}

// HTTPClientCollector collects outgoing HTTP requests
type HTTPClientCollector struct {
	buffer *RingBuffer[HTTPRequest]
	mu     sync.RWMutex
}

// NewHTTPClientCollector creates a new collector for outgoing HTTP requests
func NewHTTPClientCollector(capacity uint64) *HTTPClientCollector {
	return &HTTPClientCollector{
		buffer: NewRingBuffer[HTTPRequest](capacity),
	}
}

// Transport returns an http.RoundTripper that captures request/response data
func (c *HTTPClientCollector) Transport(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}

	return &httpClientTransport{
		next:      next,
		collector: c,
	}
}

// GetRequests returns the most recent n HTTP requests
func (c *HTTPClientCollector) GetRequests(n uint64) []HTTPRequest {
	return c.buffer.GetRecords(n)
}

// Add adds an HTTP request to the collector
func (c *HTTPClientCollector) Add(req HTTPRequest) {
	c.buffer.Add(req)
}

// httpClientTransport is an http.RoundTripper that captures HTTP request/response data
type httpClientTransport struct {
	next      http.RoundTripper
	collector *HTTPClientCollector
}

func (t *httpClientTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Generate a unique ID for this request
	id := generateID()

	// Capture request data
	requestTime := time.Now()

	// Capture request body if needed (limited size)
	// requestBody := captureRequestBody(req)

	// Call the next transport
	resp, err := t.next.RoundTrip(req)

	// Capture response data
	responseTime := time.Now()

	// Create the HTTP request record
	httpReq := HTTPRequest{
		ID:             id,
		Method:         req.Method,
		URL:            req.URL.String(),
		RequestTime:    requestTime,
		ResponseTime:   responseTime,
		RequestHeaders: req.Header.Clone(),
	}

	// Add additional data if we have a response
	if resp != nil {
		httpReq.StatusCode = resp.StatusCode
		httpReq.ResponseHeaders = resp.Header.Clone()
		// httpReq.ResponseBody = captureResponseBody(resp)
	}

	// If there was an error, record it
	if err != nil {
		httpReq.Error = err
	}

	// Add to the collector
	t.collector.Add(httpReq)

	return resp, err
}

func generateID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}
