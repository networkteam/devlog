package dashboard

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/uuid"

	"github.com/networkteam/devlog/collector"
	"github.com/networkteam/devlog/dashboard/views"
)

// defaultAgentListLimit is the fallback number of events returned by the list
// endpoint when no explicit limit is requested (capped at truncateAfter).
const defaultAgentListLimit = 100

// agentListEvents handles GET /api/agent/v1/s/{sid}/events
func (h *Handler) agentListEvents(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := h.getSessionID(r)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid session id")
		return
	}

	storage := h.sessions.Get(sessionID)
	if storage == nil {
		writeJSONError(w, http.StatusNotFound, "no capture session for this id")
		return
	}

	// Keep the session alive while an agent polls it.
	h.sessions.UpdateActivity(sessionID)

	filters, err := parseEventFilters(r.URL.Query(), h.truncateAfter)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	events := storage.GetEvents(h.truncateAfter)
	slices.Reverse(events) // newest first

	summaries := make([]EventSummary, 0, len(events))
	for _, e := range events {
		detail := h.redactDetail(mapEventDetail(e))
		if detail == nil {
			continue // suppressed by redactor
		}
		if !filters.matches(detail) {
			continue
		}
		summaries = append(summaries, summaryFromDetail(detail))
		if len(summaries) >= filters.limit {
			break
		}
	}

	writeJSON(w, http.StatusOK, summaries)
}

// agentEventDetail handles GET /api/agent/v1/s/{sid}/events/{eventId}
func (h *Handler) agentEventDetail(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := h.getSessionID(r)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid session id")
		return
	}

	storage := h.sessions.Get(sessionID)
	if storage == nil {
		writeJSONError(w, http.StatusNotFound, "no capture session for this id")
		return
	}

	h.sessions.UpdateActivity(sessionID)

	eventID, err := uuid.FromString(r.PathValue("eventId"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid event id")
		return
	}

	event, exists := storage.GetEvent(eventID)
	if !exists {
		writeJSONError(w, http.StatusNotFound, "event not found")
		return
	}

	detail := h.redactDetail(mapEventDetail(event))
	if detail == nil {
		// Suppressed by redactor.
		writeJSONError(w, http.StatusNotFound, "event not found")
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

// redactDetail marks body truncation, applies default header masking (unless
// disabled), runs the embedder's redactor, then normalizes body suppression.
// A nil return suppresses the event.
func (h *Handler) redactDetail(d *EventDetail) *EventDetail {
	if d == nil {
		return nil
	}
	markBodyTruncationRecursive(d, h.agentMaxBodyBytes)
	if !h.agentInsecureHeaders {
		maskHeadersRecursive(d, h.agentRedactedHeaders)
	}
	if h.agentRedactor != nil {
		d = h.agentRedactor(d)
		if d == nil {
			return nil
		}
	}
	normalizeBodyRedactionRecursive(d)
	return d
}

// markBodyTruncationRecursive flags each captured body whose size exceeds the cap,
// so the detail view reflects what a body fetch would return.
func markBodyTruncationRecursive(d *EventDetail, maxBytes uint64) {
	if d == nil {
		return
	}
	for _, m := range []*BodyMeta{d.RequestBody, d.ResponseBody} {
		if m != nil && maxBytes > 0 && m.Size > maxBytes {
			m.Truncated = true
		}
	}
	for _, child := range d.Children {
		markBodyTruncationRecursive(child, maxBytes)
	}
}

// normalizeBodyRedactionRecursive reconciles the two ways a redactor can suppress
// a body: a non-nil BodyMeta with Available=false is treated as redacted, so the
// detail consistently shows redacted=true. (An absent body is represented by a
// nil BodyMeta, so this never mislabels "no body" as "redacted".)
func normalizeBodyRedactionRecursive(d *EventDetail) {
	if d == nil {
		return
	}
	for _, m := range []*BodyMeta{d.RequestBody, d.ResponseBody} {
		if m != nil && !m.Available {
			m.Redacted = true
		}
	}
	for _, child := range d.Children {
		normalizeBodyRedactionRecursive(child)
	}
}

// bodySide identifies which body a body endpoint serves.
type bodySide int

const (
	requestBodySide bodySide = iota
	responseBodySide
)

func (h *Handler) agentRequestBody(w http.ResponseWriter, r *http.Request) {
	h.agentBody(w, r, requestBodySide)
}

func (h *Handler) agentResponseBody(w http.ResponseWriter, r *http.Request) {
	h.agentBody(w, r, responseBodySide)
}

// agentBody serves the raw request or response body for an event, honoring
// redaction and the configured size cap.
func (h *Handler) agentBody(w http.ResponseWriter, r *http.Request, side bodySide) {
	sessionID, ok := h.getSessionID(r)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "invalid session id")
		return
	}

	storage := h.sessions.Get(sessionID)
	if storage == nil {
		writeJSONError(w, http.StatusNotFound, "no capture session for this id")
		return
	}

	h.sessions.UpdateActivity(sessionID)

	eventID, err := uuid.FromString(r.PathValue("eventId"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid event id")
		return
	}

	event, exists := storage.GetEvent(eventID)
	if !exists {
		writeJSONError(w, http.StatusNotFound, "event not found")
		return
	}

	// Run the full redaction pipeline; a suppressed event is invisible.
	detail := h.redactDetail(mapEventDetail(event))
	if detail == nil {
		writeJSONError(w, http.StatusNotFound, "event not found")
		return
	}

	body, contentType, isBodyType, hasBody := extractBody(event.Data, side)
	if !isBodyType {
		writeJSONError(w, http.StatusBadRequest, "event type does not have a body")
		return
	}
	if !hasBody {
		writeJSONError(w, http.StatusNotFound, "no body available")
		return
	}

	meta := detail.RequestBody
	if side == responseBodySide {
		meta = detail.ResponseBody
	}
	if meta != nil && (meta.Redacted || !meta.Available) {
		writeJSONError(w, http.StatusGone, "body redacted")
		return
	}

	if h.agentMaxBodyBytes > 0 && uint64(len(body)) > h.agentMaxBodyBytes {
		body = body[:h.agentMaxBodyBytes]
		w.Header().Set("X-Devlog-Truncated", "true")
	}

	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// maskHeadersRecursive replaces the values of redacted headers with the sentinel
// across an EventDetail and all of its children, preserving the keys.
func maskHeadersRecursive(d *EventDetail, redacted map[string]bool) {
	if d == nil {
		return
	}
	maskHeader(d.RequestHeaders, redacted)
	maskHeader(d.ResponseHeaders, redacted)
	for _, child := range d.Children {
		maskHeadersRecursive(child, redacted)
	}
}

func maskHeader(h http.Header, redacted map[string]bool) {
	for key := range h {
		if redacted[strings.ToLower(key)] {
			h[key] = []string{redactedHeaderValue}
		}
	}
}

// eventFilters holds the parsed list query parameters.
type eventFilters struct {
	eventType  string
	since      time.Time
	until      time.Time
	statusFrom int // inclusive, 0 = unset
	statusTo   int // exclusive, 0 = unset
	path       string
	limit      int
}

func parseEventFilters(q map[string][]string, truncateAfter uint64) (eventFilters, error) {
	get := func(key string) string {
		if vs := q[key]; len(vs) > 0 {
			return vs[0]
		}
		return ""
	}

	f := eventFilters{
		eventType: get("type"),
		path:      get("path"),
	}

	maxLimit := int(truncateAfter)
	if maxLimit <= 0 {
		maxLimit = defaultAgentListLimit
	}
	f.limit = defaultAgentListLimit
	if f.limit > maxLimit {
		f.limit = maxLimit
	}
	if s := get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return f, errInvalidParam("limit")
		}
		if n > 0 && n < maxLimit {
			f.limit = n
		} else {
			f.limit = maxLimit
		}
	}

	if s := get("since"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return f, errInvalidParam("since")
		}
		f.since = t
	}
	if s := get("until"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return f, errInvalidParam("until")
		}
		f.until = t
	}

	if s := get("status"); s != "" {
		from, to, err := parseStatusClass(s)
		if err != nil {
			return f, err
		}
		f.statusFrom, f.statusTo = from, to
	}

	return f, nil
}

// parseStatusClass parses a status filter that is either a class like "5xx" or an
// exact code like "404", returning an inclusive-from/exclusive-to range.
func parseStatusClass(s string) (int, int, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) == 3 && strings.HasSuffix(s, "xx") {
		switch s[0] {
		case '1', '2', '3', '4', '5':
			base := int(s[0]-'0') * 100
			return base, base + 100, nil
		}
		return 0, 0, errInvalidParam("status")
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 100 || n > 599 {
		return 0, 0, errInvalidParam("status")
	}
	return n, n + 1, nil
}

func (f eventFilters) matches(d *EventDetail) bool {
	if f.eventType != "" && string(d.Type) != f.eventType {
		return false
	}
	if !f.since.IsZero() && d.Start.Before(f.since) {
		return false
	}
	if !f.until.IsZero() && d.Start.After(f.until) {
		return false
	}
	if f.statusTo != 0 {
		if d.StatusCode < f.statusFrom || d.StatusCode >= f.statusTo {
			return false
		}
	}
	if f.path != "" {
		hay := d.Path
		if hay == "" {
			hay = d.URL
		}
		if !strings.Contains(hay, f.path) {
			return false
		}
	}
	return true
}

// agentCaptureStatusResponse is the JSON body for the capture control endpoints.
type agentCaptureStatusResponse struct {
	Active     bool   `json:"active"`
	Mode       string `json:"mode"`
	EventCount int    `json:"eventCount"`
}

// agentCaptureStatus handles GET /api/agent/v1/s/{sid}/capture/status.
//
// Capture is started and stopped by the user in the dashboard UI; the agent only
// observes a user-managed session, so there is intentionally no agent start/stop.
func (h *Handler) agentCaptureStatus(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := h.getSessionID(r)
	if !ok {
		writeJSON(w, http.StatusOK, inactiveCaptureStatus())
		return
	}
	storage := h.sessions.Get(sessionID)
	if storage == nil {
		writeJSON(w, http.StatusOK, inactiveCaptureStatus())
		return
	}

	writeJSON(w, http.StatusOK, agentCaptureStatusResponse{
		Active:     storage.IsCapturing(),
		Mode:       storage.CaptureMode().String(),
		EventCount: countEvents(storage),
	})
}

// agentSessionInfo is one entry in the sessions listing.
type agentSessionInfo struct {
	SessionID       string `json:"sessionId"`
	Mode            string `json:"mode"`
	Capturing       bool   `json:"capturing"`
	EventCount      int    `json:"eventCount"`
	LastActiveMsAgo int64  `json:"lastActiveMsAgo"`
}

// agentListSessions handles GET /api/agent/v1/sessions - lists active capture
// sessions so an agent relay can attach to one rather than creating its own.
func (h *Handler) agentListSessions(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	infos := h.sessions.List()
	out := make([]agentSessionInfo, 0, len(infos))
	for _, s := range infos {
		out = append(out, agentSessionInfo{
			SessionID:       s.SessionID.String(),
			Mode:            s.Mode.String(),
			Capturing:       s.Capturing,
			EventCount:      s.EventCount,
			LastActiveMsAgo: now.Sub(s.LastActive).Milliseconds(),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// agentStats handles GET /api/agent/v1/stats
func (h *Handler) agentStats(w http.ResponseWriter, r *http.Request) {
	stats := h.eventAggregator.CalculateStats()
	writeJSON(w, http.StatusOK, StatsResponse{
		MemoryBytes:     stats.TotalMemory,
		MemoryFormatted: views.FormatBytes(stats.TotalMemory),
		SessionCount:    h.sessions.SessionCount(),
		MaxSessions:     h.sessions.MaxSessions(),
		EventCount:      stats.EventCount,
	})
}

func inactiveCaptureStatus() agentCaptureStatusResponse {
	return agentCaptureStatusResponse{Active: false, Mode: collector.CaptureModeSession.String()}
}

func countEvents(s *collector.CaptureStorage) int {
	return len(s.GetEvents(^uint64(0)))
}

// jsonError is the consistent error body for all agent endpoints.
type jsonError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, jsonError{Error: msg})
}

type paramError struct{ name string }

func (e paramError) Error() string { return "invalid parameter: " + e.name }

func errInvalidParam(name string) error { return paramError{name: name} }
