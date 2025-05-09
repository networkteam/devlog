package collector

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gofrs/uuid"
)

// HTTPClientOptions configures the HTTP client collector
type HTTPClientOptions struct {
	// MaxBodyBufferPool is the maximum size in bytes of the buffer pool
	// Default: 64MB
	MaxBodyBufferPool int64

	// MaxBodySize is the maximum size in bytes of a single body
	// Default: 1MB
	MaxBodySize int64

	// CaptureRequestBody indicates whether to capture request bodies
	CaptureRequestBody bool

	// CaptureResponseBody indicates whether to capture response bodies
	CaptureResponseBody bool
}

// DefaultHTTPClientOptions returns default options for the HTTP client collector
func DefaultHTTPClientOptions() HTTPClientOptions {
	return HTTPClientOptions{
		MaxBodyBufferPool:   defaultBodyBufferSize,
		MaxBodySize:         defaultMaxBodySize,
		CaptureRequestBody:  true,
		CaptureResponseBody: true,
	}
}

// HTTPClientCollector collects outgoing HTTP requests
type HTTPClientCollector struct {
	buffer   *RingBuffer[HTTPRequest]
	bodyPool *BodyBufferPool
	options  HTTPClientOptions

	mu sync.RWMutex
}

// NewHTTPClientCollector creates a new collector for outgoing HTTP requests
func NewHTTPClientCollector(capacity uint64) *HTTPClientCollector {
	return NewHTTPClientCollectorWithOptions(capacity, DefaultHTTPClientOptions())
}

// NewHTTPClientCollectorWithOptions creates a new collector with specified options
func NewHTTPClientCollectorWithOptions(capacity uint64, options HTTPClientOptions) *HTTPClientCollector {
	return &HTTPClientCollector{
		buffer:   NewRingBuffer[HTTPRequest](capacity),
		bodyPool: NewBodyBufferPool(options.MaxBodyBufferPool, options.MaxBodySize),
		options:  options,
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

// Duration returns the duration of the request
func (r HTTPRequest) Duration() time.Duration {
	return r.ResponseTime.Sub(r.RequestTime)
}

// httpClientTransport is an http.RoundTripper that captures HTTP request/response data
type httpClientTransport struct {
	next      http.RoundTripper
	collector *HTTPClientCollector
}

func (t *httpClientTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Generate a unique ID for this request
	id := generateID()

	// Record start time
	requestTime := time.Now()

	// Create a request record
	httpReq := HTTPRequest{
		ID:             id,
		Method:         req.Method,
		URL:            req.URL.String(),
		RequestTime:    requestTime,
		RequestHeaders: req.Header.Clone(),
	}

	// Capture request body if present and configured to do so
	if req.Body != nil && t.collector.options.CaptureRequestBody {
		// Wrap the body to capture it
		body := NewBody(req.Body, t.collector.bodyPool)

		// Store the body in the request record
		httpReq.RequestBody = body

		// Replace the request body with our wrapper
		req.Body = body
	}

	// Perform the actual request
	resp, err := t.next.RoundTrip(req)

	// Record end time
	responseTime := time.Now()
	httpReq.ResponseTime = responseTime

	// Capture response data if present
	if resp != nil {
		httpReq.StatusCode = resp.StatusCode
		httpReq.ResponseHeaders = resp.Header.Clone()

		// Calculate content length from header if available
		if contentLength := resp.Header.Get("Content-Length"); contentLength != "" {
			if length, err := strconv.ParseInt(contentLength, 10, 64); err == nil {
				httpReq.ResponseSize = length
			}
		}

		// Capture response body if present and configured to do so
		if resp.Body != nil && t.collector.options.CaptureResponseBody {
			// Wrap the body to capture it
			body := NewBody(resp.Body, t.collector.bodyPool)

			// Store the body in the request record
			httpReq.ResponseBody = body

			// Replace the response body with our wrapper
			resp.Body = body
		}
	}

	// Record error if present
	if err != nil {
		httpReq.Error = err
	}

	// Add the request to the collector
	t.collector.Add(httpReq)

	return resp, err
}

func generateID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}
