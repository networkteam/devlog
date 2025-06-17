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
	RequestSize     int64
	ResponseSize    int64
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

func (r HTTPServerRequest) free() {
	if r.RequestBody != nil {
		r.RequestBody.free()
	}
	if r.ResponseBody != nil {
		r.ResponseBody.free()
	}
}
