package collector

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// HTTPClientOptions configures the HTTP client collector
type HTTPClientOptions struct {
	// MaxBodyBufferPool is the maximum size in bytes of the buffer pool
	MaxBodyBufferPool int64

	// MaxBodySize is the maximum size in bytes of a single body
	MaxBodySize int64

	// CaptureRequestBody indicates whether to capture request bodies
	CaptureRequestBody bool

	// CaptureResponseBody indicates whether to capture response bodies
	CaptureResponseBody bool

	// Transformers are functions that transform/augment the HTTPClientRequest before adding it to the collector
	Transformers []HTTPClientRequestTransformer

	// NotifierOptions are options for notification about new requests
	NotifierOptions *NotifierOptions

	// EventCollector is an optional event collector for collecting requests as grouped events
	EventCollector *EventCollector
}

type HTTPClientRequestTransformer func(HTTPClientRequest) HTTPClientRequest

// DefaultHTTPClientOptions returns default options for the HTTP client collector
func DefaultHTTPClientOptions() HTTPClientOptions {
	return HTTPClientOptions{
		MaxBodyBufferPool:   DefaultBodyBufferPoolSize,
		MaxBodySize:         DefaultMaxBodySize,
		CaptureRequestBody:  true,
		CaptureResponseBody: true,
	}
}

// HTTPClientCollector collects outgoing HTTP requests
type HTTPClientCollector struct {
	buffer         *RingBuffer[HTTPClientRequest]
	bodyPool       *BodyBufferPool
	options        HTTPClientOptions
	notifier       *Notifier[HTTPClientRequest]
	eventCollector *EventCollector

	mu sync.RWMutex
}

// NewHTTPClientCollector creates a new collector for outgoing HTTP requests
func NewHTTPClientCollector(capacity uint64) *HTTPClientCollector {
	return NewHTTPClientCollectorWithOptions(capacity, DefaultHTTPClientOptions())
}

// NewHTTPClientCollectorWithOptions creates a new collector with specified options
func NewHTTPClientCollectorWithOptions(capacity uint64, options HTTPClientOptions) *HTTPClientCollector {
	notifierOptions := DefaultNotifierOptions()
	if options.NotifierOptions != nil {
		notifierOptions = *options.NotifierOptions
	}

	return &HTTPClientCollector{
		buffer:         NewRingBuffer[HTTPClientRequest](capacity),
		bodyPool:       NewBodyBufferPool(options.MaxBodyBufferPool, options.MaxBodySize),
		options:        options,
		notifier:       NewNotifierWithOptions[HTTPClientRequest](notifierOptions),
		eventCollector: options.EventCollector,
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
func (c *HTTPClientCollector) GetRequests(n uint64) []HTTPClientRequest {
	return c.buffer.GetRecords(n)
}

// Subscribe returns a channel that receives notifications of new log records
func (c *HTTPClientCollector) Subscribe(ctx context.Context) <-chan HTTPClientRequest {
	return c.notifier.Subscribe(ctx)
}

// Add adds an HTTP request to the collector
func (c *HTTPClientCollector) Add(req HTTPClientRequest) {
	c.buffer.Add(req)
	c.notifier.Notify(req)
}

// Close releases resources used by the collector
func (c *HTTPClientCollector) Close() {
	c.notifier.Close()
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
	httpReq := HTTPClientRequest{
		ID:             id,
		Method:         req.Method,
		URL:            req.URL.String(),
		RequestTime:    requestTime,
		RequestHeaders: req.Header.Clone(),
		Tags:           make(map[string]string),
	}

	// Track the original request body size
	if req.ContentLength > 0 {
		httpReq.RequestSize = req.ContentLength
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

	if t.collector.eventCollector != nil {
		newCtx := t.collector.eventCollector.StartEvent(req.Context())
		defer func(req *HTTPClientRequest) {
			t.collector.eventCollector.EndEvent(newCtx, *req)
		}(&httpReq)

		req = req.WithContext(newCtx)
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
			// Create a copy of the response to read the body even if the client doesn't
			originalRespBody := resp.Body

			// Wrap the body to capture it
			body := NewBody(originalRespBody, t.collector.bodyPool)

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

	// Transform the request if any transformers are provided
	for _, transformer := range t.collector.options.Transformers {
		httpReq = transformer(httpReq)
	}

	// Add the request to the collector
	t.collector.Add(httpReq)

	return resp, err
}

// Unwrap returns the underlying http.RoundTripper
func (t *httpClientTransport) Unwrap() http.RoundTripper {
	return t.next
}
