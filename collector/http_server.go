package collector

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// HTTPServerOptions configures the HTTP server collector
type HTTPServerOptions struct {
	// MaxBodySize is the maximum size in bytes of a single body
	MaxBodySize int

	// CaptureRequestBody indicates whether to capture request bodies
	CaptureRequestBody bool

	// CaptureResponseBody indicates whether to capture response bodies
	CaptureResponseBody bool

	// SkipPaths is a list of path prefixes to skip for request collection
	// Useful for excluding static files or the dashboard itself
	SkipPaths []string

	// Transformers are functions that transform/augment the HTTPServerRequest before adding it to the collector
	Transformers []HTTPServerRequestTransformer

	// NotifierOptions are options for notification about new requests
	NotifierOptions *NotifierOptions

	// EventCollector is an optional event collector for collecting requests as grouped events
	EventCollector *EventCollector
}

type HTTPServerRequestTransformer func(HTTPServerRequest) HTTPServerRequest

// DefaultHTTPServerOptions returns default options for the HTTP server collector
func DefaultHTTPServerOptions() HTTPServerOptions {
	return HTTPServerOptions{
		MaxBodySize:         DefaultMaxBodySize,
		CaptureRequestBody:  true,
		CaptureResponseBody: true,
		SkipPaths:           nil,
	}
}

// HTTPServerCollector collects incoming HTTP requests
type HTTPServerCollector struct {
	buffer *RingBuffer[HTTPServerRequest]

	options        HTTPServerOptions
	notifier       *Notifier[HTTPServerRequest]
	eventCollector *EventCollector

	mu sync.RWMutex
}

// NewHTTPServerCollector creates a new collector for incoming HTTP requests
func NewHTTPServerCollector(capacity uint64) *HTTPServerCollector {
	return NewHTTPServerCollectorWithOptions(capacity, DefaultHTTPServerOptions())
}

// NewHTTPServerCollectorWithOptions creates a new collector with specified options
func NewHTTPServerCollectorWithOptions(capacity uint64, options HTTPServerOptions) *HTTPServerCollector {
	notifierOptions := DefaultNotifierOptions()
	if options.NotifierOptions != nil {
		notifierOptions = *options.NotifierOptions
	}

	collector := &HTTPServerCollector{
		options:        options,
		notifier:       NewNotifierWithOptions[HTTPServerRequest](notifierOptions),
		eventCollector: options.EventCollector,
	}
	if capacity > 0 {
		collector.buffer = NewRingBuffer[HTTPServerRequest](capacity)
	}

	return collector
}

// GetRequests returns the most recent n HTTP server requests
func (c *HTTPServerCollector) GetRequests(n uint64) []HTTPServerRequest {
	if c.buffer == nil {
		return nil
	}
	return c.buffer.GetRecords(n)
}

// Subscribe returns a channel that receives notifications of new requests
func (c *HTTPServerCollector) Subscribe(ctx context.Context) <-chan HTTPServerRequest {
	return c.notifier.Subscribe(ctx)
}

// Add adds an HTTP server request to the collector
func (c *HTTPServerCollector) Add(req HTTPServerRequest) {
	if c.buffer != nil {
		c.buffer.Add(req)
	}
	c.notifier.Notify(req)
}

// Middleware returns an http.Handler middleware that captures request/response data
func (c *HTTPServerCollector) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if this path should be skipped
		for _, prefix := range c.options.SkipPaths {
			if len(prefix) > 0 && len(r.URL.Path) >= len(prefix) && r.URL.Path[:len(prefix)] == prefix {
				// Skip this path
				next.ServeHTTP(w, r)
				return
			}
		}

		// Generate a unique ID for this request
		id := generateID()

		// Record start time
		requestTime := time.Now()

		// Create a request record
		httpReq := HTTPServerRequest{
			ID:             id,
			Method:         r.Method,
			Path:           r.URL.Path,
			URL:            r.URL.String(),
			RemoteAddr:     r.RemoteAddr,
			RequestTime:    requestTime,
			RequestHeaders: cloneHeader(r.Header),
			Tags:           make(map[string]string),
		}

		// Capture the request body if present and configured to do so
		// Only check if the body is the special NoBody sentinel value (empty body)
		var requestBody *Body
		if r.Body != nil && r.Body != http.NoBody && c.options.CaptureRequestBody {
			// Save the original body
			originalBody := r.Body

			// Pre-read the body to ensure capturing bodies even if the handler writes a large response (Go net/http will close the request body then)
			requestBody = PreReadBody(originalBody, c.options.MaxBodySize)

			// Replace the request body with our wrapper
			r.Body = requestBody

			// Store in our request record
			httpReq.RequestBody = requestBody
		}

		// Create a response writer wrapper to capture the response
		crw := &captureResponseWriter{
			ResponseWriter: w,
			collector:      c,
		}

		if c.eventCollector != nil {
			newCtx := c.eventCollector.StartEvent(r.Context())
			defer func(req *HTTPServerRequest) {
				c.eventCollector.EndEvent(newCtx, *req)
			}(&httpReq)

			r = r.WithContext(newCtx)
		}

		// Call the next handler
		next.ServeHTTP(crw, r)

		// Close the request body to make sure we capture request bodies even if they are not read
		if requestBody != nil {
			_ = requestBody.Close()
		}

		// Record end time
		responseTime := time.Now()
		httpReq.ResponseTime = responseTime

		// Capture response data
		httpReq.StatusCode = crw.statusCode
		if httpReq.StatusCode == 0 {
			httpReq.StatusCode = http.StatusOK
		}

		httpReq.ResponseHeaders = crw.Header()
		httpReq.ResponseBody = crw.body

		// Add request size if available
		if requestBody != nil {
			httpReq.RequestSize = requestBody.Size()
		}

		// Add response size if available
		if crw.body != nil {
			httpReq.ResponseSize = crw.body.Size()
		}

		// Transform the request if any transformers are provided
		for _, transformer := range c.options.Transformers {
			httpReq = transformer(httpReq)
		}

		// Add to the collector
		c.Add(httpReq)
	})
}

// Close releases resources used by the collector
func (c *HTTPServerCollector) Close() {
	c.notifier.Close()
	c.buffer = nil
}

// captureResponseWriter is a wrapper for http.ResponseWriter that captures the response
type captureResponseWriter struct {
	http.ResponseWriter
	statusCode    int
	body          *Body
	wroteHeader   bool
	bodyCapturing bool
	collector     *HTTPServerCollector
}

// WriteHeader implements http.ResponseWriter
func (crw *captureResponseWriter) WriteHeader(statusCode int) {
	if crw.wroteHeader {
		return
	}
	crw.wroteHeader = true
	crw.statusCode = statusCode
	crw.ResponseWriter.WriteHeader(statusCode)
}

// Write implements http.ResponseWriter
func (crw *captureResponseWriter) Write(b []byte) (int, error) {
	if !crw.wroteHeader {
		crw.WriteHeader(http.StatusOK)
	}

	// If we're capturing the body and haven't set up the body capture yet
	if crw.collector.options.CaptureResponseBody && !crw.bodyCapturing {

		// Create a buffer to capture the response body
		crw.body = NewBody(nil, crw.collector.options.MaxBodySize)
		crw.bodyCapturing = true
	}

	// First write to the original response writer
	n, err := crw.ResponseWriter.Write(b)
	if err != nil {
		return n, err
	}

	// If we're capturing the body, store a copy in our buffer
	if crw.collector.options.CaptureResponseBody && crw.bodyCapturing && crw.body != nil {
		crw.body.buffer.Write(b[:n])
	}

	return n, nil
}

// Flush implements http.Flusher if the original response writer implements it
func (crw *captureResponseWriter) Flush() {
	if flusher, ok := crw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack implements http.Hijacker if the original response writer implements it
func (crw *captureResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := crw.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, fmt.Errorf("response writer does not implement http.Hijacker")
}

// Push implements http.Pusher if the original response writer implements it
func (crw *captureResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := crw.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return fmt.Errorf("response writer does not implement http.Pusher")
}

// Helper to clone an http.Header, similar to Header.Clone() in newer Go versions
func cloneHeader(h http.Header) http.Header {
	h2 := make(http.Header, len(h))
	for k, vv := range h {
		vv2 := make([]string, len(vv))
		copy(vv2, vv)
		h2[k] = vv2
	}
	return h2
}
