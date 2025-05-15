package collector

import (
	"net/http"
	"time"

	"github.com/gofrs/uuid"
)

// HTTPClientRequest represents a captured HTTP request/response pair
type HTTPClientRequest struct {
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
	// Tags are custom tags that can be used to categorize requests
	Tags  map[string]string
	Error error
}

// Duration returns the duration of the request
func (r HTTPClientRequest) Duration() time.Duration {
	return r.ResponseTime.Sub(r.RequestTime)
}

func generateID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}
