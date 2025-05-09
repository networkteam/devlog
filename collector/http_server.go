package collector

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gofrs/uuid"
)

// HTTPServerOptions configures the HTTP server collector
type HTTPServerOptions struct {
	// MaxBodyBufferPool is the maximum size in bytes of the buffer pool
	MaxBodyBufferPool int64

	// MaxBodySize is the maximum size in bytes of a single body
	MaxBodySize int64

	// CaptureRequestBody indicates whether to capture request bodies
	CaptureRequestBody bool

	// CaptureResponseBody indicates whether to capture response bodies
	CaptureResponseBody bool

	// SkipPaths is a list of path prefixes to skip for request collection
	// Useful for excluding static files or the dashboard itself
	SkipPaths []string
}

// DefaultHTTPServerOptions returns default options for the HTTP server collector
func DefaultHTTPServerOptions() HTTPServerOptions {
	return HTTPServerOptions{
		MaxBodyBufferPool:   DefaultBodyBufferSize,
		MaxBodySize:         DefaultMaxBodySize,
		CaptureRequestBody:  true,
		CaptureResponseBody: true,
		SkipPaths:           nil,
	}
}

// HTTPServerRequest represents a captured HTTP server request/response pair
type HTTPServerRequest struct {
	ID              uuid.UUID
	Method          string
	Path            string
	URL             string
	RemoteAddr      string
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
func (r HTTPServerRequest) Duration() time.Duration {
	return r.ResponseTime.Sub(r.RequestTime)
}

// HTTPServerCollector collects incoming HTTP requests
type HTTPServerCollector struct {
	buffer   *RingBuffer[HTTPServerRequest]
	mu       sync.RWMutex
	bodyPool *BodyBufferPool
	options  HTTPServerOptions
}

// NewHTTPServerCollector creates a new collector for incoming HTTP requests
func NewHTTPServerCollector(capacity uint64) *HTTPServerCollector {
	return NewHTTPServerCollectorWithOptions(capacity, DefaultHTTPServerOptions())
}

// NewHTTPServerCollectorWithOptions creates a new collector with specified options
func NewHTTPServerCollectorWithOptions(capacity uint64, options HTTPServerOptions) *HTTPServerCollector {
	return &HTTPServerCollector{
		buffer:   NewRingBuffer[HTTPServerRequest](capacity),
		bodyPool: NewBodyBufferPool(options.MaxBodyBufferPool, options.MaxBodySize),
		options:  options,
	}
}

// GetRequests returns the most recent n HTTP server requests
func (c *HTTPServerCollector) GetRequests(n uint64) []HTTPServerRequest {
	return c.buffer.GetRecords(n)
}

// Add adds an HTTP server request to the collector
func (c *HTTPServerCollector) Add(req HTTPServerRequest) {
	c.buffer.Add(req)
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
		}

		// Capture the request body if present and configured to do so
		// Only check if the body is the special NoBody sentinel value (empty body)
		var requestBody *Body
		if r.Body != nil && r.Body != http.NoBody && c.options.CaptureRequestBody {
			// Save the original body
			originalBody := r.Body

			// Create a body wrapper
			requestBody = NewBody(originalBody, c.bodyPool)

			// Replace the request body with our wrapper
			r.Body = requestBody

			// Store in our request record
			httpReq.RequestBody = requestBody
		}

		// Create a response writer wrapper to capture the response
		crw := &captureResponseWriter{
			ResponseWriter: w,
			bodyPool:       c.bodyPool,
			captureBody:    c.options.CaptureResponseBody,
		}

		// Call the next handler
		next.ServeHTTP(crw, r)

		// Record end time
		responseTime := time.Now()
		httpReq.ResponseTime = responseTime

		// Capture response data
		httpReq.StatusCode = crw.statusCode
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

		// Add to the collector
		c.Add(httpReq)
	})
}

// captureResponseWriter is a wrapper for http.ResponseWriter that captures the response
type captureResponseWriter struct {
	http.ResponseWriter
	statusCode    int
	body          *Body
	bodyPool      *BodyBufferPool
	captureBody   bool
	wroteHeader   bool
	bodyCapturing bool
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
	if crw.captureBody && !crw.bodyCapturing {
		// Create a buffer to capture the response body
		buf := crw.bodyPool.GetBuffer()
		crw.body = &Body{
			buffer:          buf,
			pool:            crw.bodyPool,
			maxSize:         crw.bodyPool.maxBufferSize,
			isFullyCaptured: true, // Since we're capturing directly, not via a reader
		}
		crw.bodyCapturing = true
	}

	// First write to the original response writer
	n, err := crw.ResponseWriter.Write(b)
	if err != nil {
		return n, err
	}

	// If we're capturing the body, store a copy in our buffer
	if crw.captureBody && crw.bodyCapturing && crw.body != nil {
		crw.body.mu.Lock()
		// Only write to buffer if we haven't exceeded max size
		if crw.body.size < crw.body.maxSize {
			// Determine how much we can write without exceeding max size
			toWrite := n
			if crw.body.size+int64(n) > crw.body.maxSize {
				toWrite = int(crw.body.maxSize - crw.body.size)
				crw.body.isTruncated = true
			}

			if toWrite > 0 {
				// Write to the buffer directly
				crw.body.buffer.Write(b[:toWrite])
				crw.body.size += int64(toWrite)
			}
		}
		crw.body.mu.Unlock()
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
