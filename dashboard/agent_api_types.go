package dashboard

import (
	"database/sql/driver"
	"log/slog"
	"net/http"
	"time"

	"github.com/networkteam/devlog/collector"
)

// EventType is the agent-API discriminator for an event's payload.
type EventType string

const (
	EventTypeHTTPServer EventType = "http_server"
	EventTypeHTTPClient EventType = "http_client"
	EventTypeDBQuery    EventType = "db_query"
	EventTypeLog        EventType = "log"
	EventTypeUnknown    EventType = "unknown"
)

// AgentRedactor is an embedder-supplied hook applied to each EventDetail before
// it is encoded for the agent API. It may mutate the detail in place and return
// it, or return nil to suppress the event entirely (omitted from lists, 404 on
// the detail endpoint).
type AgentRedactor func(*EventDetail) *EventDetail

// BodyMeta describes a captured request or response body without inlining its
// bytes. Raw bytes are served only by the dedicated body endpoints (Task 2).
type BodyMeta struct {
	Size        uint64 `json:"size"`
	ContentType string `json:"contentType,omitempty"`
	// Available is true when a body was captured and can be fetched.
	Available bool `json:"available"`
	// Redacted is true when redaction has suppressed the body (distinct from
	// Available=false because no body was captured).
	Redacted bool `json:"redacted"`
	// Truncated is true when the captured body is larger than the agent body
	// size cap, so a fetch via the body endpoint returns only the first cap bytes.
	Truncated bool `json:"truncated,omitempty"`
}

// DBArg is a single database query argument.
type DBArg struct {
	Name    string `json:"name,omitempty"`
	Ordinal int    `json:"ordinal,omitempty"`
	Value   any    `json:"value,omitempty"`
}

// EventSummary is the compact representation returned by the list endpoint.
type EventSummary struct {
	ID         string    `json:"id"`
	Type       EventType `json:"type"`
	Start      time.Time `json:"start"`
	DurationMs int64     `json:"durationMs"`

	// Per-type summary fields (only the relevant ones are populated).
	Method     string `json:"method,omitempty"`
	Path       string `json:"path,omitempty"`
	StatusCode int    `json:"statusCode,omitempty"`
	Query      string `json:"query,omitempty"`
	Level      string `json:"level,omitempty"`
	Message    string `json:"message,omitempty"`
	Error      string `json:"error,omitempty"`
}

// EventDetail is the full representation returned by the detail endpoint and used
// as the unit of redaction. Body bytes are never inlined; only metadata is shown.
type EventDetail struct {
	ID         string    `json:"id"`
	Type       EventType `json:"type"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	DurationMs int64     `json:"durationMs"`

	// HTTP (server and client)
	Method          string            `json:"method,omitempty"`
	Path            string            `json:"path,omitempty"`
	URL             string            `json:"url,omitempty"`
	StatusCode      int               `json:"statusCode,omitempty"`
	RemoteAddr      string            `json:"remoteAddr,omitempty"`
	RequestHeaders  http.Header       `json:"requestHeaders,omitempty"`
	ResponseHeaders http.Header       `json:"responseHeaders,omitempty"`
	RequestBody     *BodyMeta         `json:"requestBody,omitempty"`
	ResponseBody    *BodyMeta         `json:"responseBody,omitempty"`
	Tags            map[string]string `json:"tags,omitempty"`

	// DB query
	Query    string  `json:"query,omitempty"`
	Args     []DBArg `json:"args,omitempty"`
	Language string  `json:"language,omitempty"`

	// Log record
	Level   string         `json:"level,omitempty"`
	Message string         `json:"message,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`

	Error    string         `json:"error,omitempty"`
	Children []*EventDetail `json:"children,omitempty"`
}

// defaultRedactedHeaders are the header names whose values are masked by default,
// regardless of any configured redactor. Stored lowercased for case-insensitive
// matching. Set mirrors the otelhttptrace default set.
var defaultRedactedHeaders = []string{
	"authorization",
	"cookie",
	"set-cookie",
	"www-authenticate",
	"proxy-authenticate",
	"proxy-authorization",
}

// redactedHeaderValue is the sentinel used in place of a masked header value.
const redactedHeaderValue = "[REDACTED]"

// mapEventDetail maps a collector.Event (and its children, recursively) to an
// EventDetail. It does not perform redaction; see Handler.redactDetail.
func mapEventDetail(e *collector.Event) *EventDetail {
	if e == nil {
		return nil
	}

	d := &EventDetail{
		ID:         e.ID.String(),
		Start:      e.Start,
		End:        e.End,
		DurationMs: durationMs(e.Start, e.End),
		Type:       EventTypeUnknown,
	}

	switch data := e.Data.(type) {
	case collector.HTTPServerRequest:
		d.Type = EventTypeHTTPServer
		d.Method = data.Method
		d.Path = data.Path
		d.URL = data.URL
		d.StatusCode = data.StatusCode
		d.RemoteAddr = data.RemoteAddr
		d.RequestHeaders = cloneHeader(data.RequestHeaders)
		d.ResponseHeaders = cloneHeader(data.ResponseHeaders)
		d.RequestBody = bodyMeta(data.RequestBody, data.RequestHeaders.Get("Content-Type"))
		d.ResponseBody = bodyMeta(data.ResponseBody, data.ResponseHeaders.Get("Content-Type"))
		d.Tags = data.Tags
		if data.Error != nil {
			d.Error = data.Error.Error()
		}
	case collector.HTTPClientRequest:
		d.Type = EventTypeHTTPClient
		d.Method = data.Method
		d.URL = data.URL
		d.StatusCode = data.StatusCode
		d.RequestHeaders = cloneHeader(data.RequestHeaders)
		d.ResponseHeaders = cloneHeader(data.ResponseHeaders)
		d.RequestBody = bodyMeta(data.RequestBody, data.RequestHeaders.Get("Content-Type"))
		d.ResponseBody = bodyMeta(data.ResponseBody, data.ResponseHeaders.Get("Content-Type"))
		d.Tags = data.Tags
		if data.Error != nil {
			d.Error = data.Error.Error()
		}
	case collector.DBQuery:
		d.Type = EventTypeDBQuery
		d.Query = data.Query
		d.Language = data.Language
		d.Args = mapDBArgs(data.Args)
		if data.Error != nil {
			d.Error = data.Error.Error()
		}
	case slog.Record:
		d.Type = EventTypeLog
		d.Level = data.Level.String()
		d.Message = data.Message
		d.Attrs = mapLogAttrs(data)
	}

	for _, child := range e.Children {
		if cd := mapEventDetail(child); cd != nil {
			d.Children = append(d.Children, cd)
		}
	}

	return d
}

// summaryFromDetail derives the compact list representation from a (redacted)
// detail, so the list cannot bypass redaction.
func summaryFromDetail(d *EventDetail) EventSummary {
	return EventSummary{
		ID:         d.ID,
		Type:       d.Type,
		Start:      d.Start,
		DurationMs: d.DurationMs,
		Method:     d.Method,
		Path:       summaryPath(d),
		StatusCode: d.StatusCode,
		Query:      d.Query,
		Level:      d.Level,
		Message:    d.Message,
		Error:      d.Error,
	}
}

// summaryPath returns the path-ish field for a summary: Path for server events,
// URL for client events, empty otherwise.
func summaryPath(d *EventDetail) string {
	if d.Path != "" {
		return d.Path
	}
	return d.URL
}

func mapDBArgs(args []driver.NamedValue) []DBArg {
	if len(args) == 0 {
		return nil
	}
	out := make([]DBArg, 0, len(args))
	for _, a := range args {
		out = append(out, DBArg{Name: a.Name, Ordinal: a.Ordinal, Value: a.Value})
	}
	return out
}

func mapLogAttrs(record slog.Record) map[string]any {
	if record.NumAttrs() == 0 {
		return nil
	}
	attrs := make(map[string]any, record.NumAttrs())
	record.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		return true
	})
	return attrs
}

// extractBody pulls the raw bytes and content type for the requested side of an
// event. isBodyType is false for event types that have no bodies (DB, log);
// hasBody is false when the event type supports bodies but none was captured.
func extractBody(data any, side bodySide) (body []byte, contentType string, isBodyType bool, hasBody bool) {
	switch d := data.(type) {
	case collector.HTTPServerRequest:
		if side == requestBodySide {
			return bodyBytes(d.RequestBody), d.RequestHeaders.Get("Content-Type"), true, d.RequestBody != nil
		}
		return bodyBytes(d.ResponseBody), d.ResponseHeaders.Get("Content-Type"), true, d.ResponseBody != nil
	case collector.HTTPClientRequest:
		if side == requestBodySide {
			return bodyBytes(d.RequestBody), d.RequestHeaders.Get("Content-Type"), true, d.RequestBody != nil
		}
		return bodyBytes(d.ResponseBody), d.ResponseHeaders.Get("Content-Type"), true, d.ResponseBody != nil
	default:
		return nil, "", false, false
	}
}

func bodyBytes(b *collector.Body) []byte {
	if b == nil {
		return nil
	}
	return b.Bytes()
}

func bodyMeta(b *collector.Body, contentType string) *BodyMeta {
	if b == nil {
		return nil
	}
	return &BodyMeta{
		Size:        b.Size(),
		ContentType: contentType,
		Available:   true,
		Redacted:    false,
	}
}

func cloneHeader(h http.Header) http.Header {
	if h == nil {
		return nil
	}
	return h.Clone()
}

func durationMs(start, end time.Time) int64 {
	if end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}
