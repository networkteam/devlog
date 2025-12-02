package collector

import (
	"net/http"
	"time"

	"github.com/gofrs/uuid"
)

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
	RequestSize     uint64
	ResponseSize    uint64
	RequestHeaders  http.Header
	ResponseHeaders http.Header
	RequestBody     *Body
	ResponseBody    *Body
	// Tags are custom tags that can be used to categorize requests
	Tags  map[string]string
	Error error
}

// Duration returns the duration of the request
func (r HTTPServerRequest) Duration() time.Duration {
	return r.ResponseTime.Sub(r.RequestTime)
}

// Size returns the estimated memory size of this request in bytes
func (r HTTPServerRequest) Size() uint64 {
	size := uint64(200) // base struct overhead
	size += uint64(len(r.URL) + len(r.Path) + len(r.Method) + len(r.RemoteAddr))
	size += headersSize(r.RequestHeaders)
	size += headersSize(r.ResponseHeaders)
	if r.RequestBody != nil {
		size += r.RequestBody.Size()
	}
	if r.ResponseBody != nil {
		size += r.ResponseBody.Size()
	}
	for k, v := range r.Tags {
		size += uint64(len(k) + len(v))
	}
	return size
}

func headersSize(h http.Header) uint64 {
	var size uint64
	for k, vs := range h {
		size += uint64(len(k))
		for _, v := range vs {
			size += uint64(len(v))
		}
	}
	return size
}
