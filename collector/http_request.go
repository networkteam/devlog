package collector

import (
	"net/http"
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

// Duration returns the duration of the request
func (r HTTPRequest) Duration() time.Duration {
	return r.ResponseTime.Sub(r.RequestTime)
}

func generateID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}
