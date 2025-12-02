package collector

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// HTTPClientOptions configures the HTTP client collector
type HTTPClientOptions struct {
	// MaxBodySize is the maximum size in bytes of a single body
	MaxBodySize int

	// CaptureRequestBody indicates whether to capture request bodies
	CaptureRequestBody bool

	// CaptureResponseBody indicates whether to capture response bodies
	CaptureResponseBody bool

	// Transformers are functions that transform/augment the HTTPClientRequest before adding it to the collector
	Transformers []HTTPClientRequestTransformer

	// NotifierOptions are options for notification about new requests
	NotifierOptions *NotifierOptions

	// EventAggregator is the aggregator for collecting requests as grouped events
	EventAggregator *EventAggregator
}

type HTTPClientRequestTransformer func(HTTPClientRequest) HTTPClientRequest

// DefaultHTTPClientOptions returns default options for the HTTP client collector
func DefaultHTTPClientOptions() HTTPClientOptions {
	return HTTPClientOptions{
		MaxBodySize:         DefaultMaxBodySize,
		CaptureRequestBody:  true,
		CaptureResponseBody: true,
	}
}

// HTTPClientCollector collects outgoing HTTP requests
type HTTPClientCollector struct {
	options         HTTPClientOptions
	notifier        *Notifier[HTTPClientRequest]
	eventAggregator *EventAggregator
}

// NewHTTPClientCollector creates a new collector for outgoing HTTP requests
func NewHTTPClientCollector() *HTTPClientCollector {
	return NewHTTPClientCollectorWithOptions(DefaultHTTPClientOptions())
}

// NewHTTPClientCollectorWithOptions creates a new collector with specified options
func NewHTTPClientCollectorWithOptions(options HTTPClientOptions) *HTTPClientCollector {
	notifierOptions := DefaultNotifierOptions()
	if options.NotifierOptions != nil {
		notifierOptions = *options.NotifierOptions
	}

	return &HTTPClientCollector{
		options:         options,
		notifier:        NewNotifierWithOptions[HTTPClientRequest](notifierOptions),
		eventAggregator: options.EventAggregator,
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

// Subscribe returns a channel that receives notifications of new HTTP requests
func (c *HTTPClientCollector) Subscribe(ctx context.Context) <-chan HTTPClientRequest {
	return c.notifier.Subscribe(ctx)
}

// Add adds an HTTP request to the collector and notifies subscribers
func (c *HTTPClientCollector) Add(req HTTPClientRequest) {
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
	ctx := req.Context()

	// Check if we should capture this request (using EventAggregator)
	// Default to true for backward compatibility when neither aggregator nor collector is set
	shouldCapture := true
	if t.collector.eventAggregator != nil {
		shouldCapture = t.collector.eventAggregator.ShouldCapture(ctx)
	}
	// Note: EventCollector (deprecated) is always-capture, so no change needed

	// Early bailout if not capturing - just pass through
	if !shouldCapture {
		return t.next.RoundTrip(req)
	}

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
		body := NewBody(req.Body, t.collector.options.MaxBodySize)

		// Store the body in the request record
		httpReq.RequestBody = body

		// Replace the request body with our wrapper
		req.Body = body
	}

	// Start event tracking with EventAggregator
	if t.collector.eventAggregator != nil {
		newCtx := t.collector.eventAggregator.StartEvent(ctx)
		defer func(req *HTTPClientRequest) {
			t.collector.eventAggregator.EndEvent(newCtx, *req)
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
			body := NewBody(originalRespBody, t.collector.options.MaxBodySize)

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
