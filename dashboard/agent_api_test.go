package dashboard

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gofrs/uuid"

	"github.com/networkteam/devlog/collector"
)

// newAgentTestHandler builds a Handler with the agent API enabled (plus any extra
// options) and a single capture session, returning the handler and the session id.
func newAgentTestHandler(t *testing.T, opts ...HandlerOption) (*Handler, uuid.UUID) {
	t.Helper()

	aggregator := collector.NewEventAggregator()
	all := append([]HandlerOption{WithAgentAPI(), WithStorageCapacity(100)}, opts...)
	h := NewHandler(aggregator, all...)
	t.Cleanup(h.Close)

	sid := uuid.Must(uuid.NewV4())
	if _, _, err := h.sessions.GetOrCreate(sid, collector.CaptureModeGlobal); err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	return h, sid
}

func addEvent(t *testing.T, h *Handler, sid uuid.UUID, e *collector.Event) {
	t.Helper()
	storage := h.sessions.Get(sid)
	if storage == nil {
		t.Fatal("no storage for session")
	}
	storage.Add(e)
}

// --- fixture builders ---

func httpServerEvent(start time.Time, method, path string, status int, reqHeaders http.Header) *collector.Event {
	return &collector.Event{
		ID:    uuid.Must(uuid.NewV4()),
		Start: start,
		End:   start.Add(10 * time.Millisecond),
		Data: collector.HTTPServerRequest{
			ID:             uuid.Must(uuid.NewV4()),
			Method:         method,
			Path:           path,
			URL:            "http://example.test" + path,
			StatusCode:     status,
			RequestHeaders: reqHeaders,
		},
	}
}

func dbQueryEvent(start time.Time, query string) *collector.Event {
	return &collector.Event{
		ID:    uuid.Must(uuid.NewV4()),
		Start: start,
		End:   start.Add(2 * time.Millisecond),
		Data: collector.DBQuery{
			Query:    query,
			Language: "postgresql",
		},
	}
}

// bodyWith builds a fully-captured collector.Body containing data, with a capture
// limit large enough to avoid capture-time truncation.
func bodyWith(data []byte) *collector.Body {
	b := collector.NewBody(io.NopCloser(bytes.NewReader(data)), len(data)+16)
	b.Close() // reads the reader into the buffer
	return b
}

// httpServerEventWithReqBody builds an HTTP server event carrying a request body.
func httpServerEventWithReqBody(start time.Time, path, contentType string, body []byte) *collector.Event {
	headers := http.Header{}
	if contentType != "" {
		headers.Set("Content-Type", contentType)
	}
	return &collector.Event{
		ID:    uuid.Must(uuid.NewV4()),
		Start: start,
		End:   start.Add(5 * time.Millisecond),
		Data: collector.HTTPServerRequest{
			ID:             uuid.Must(uuid.NewV4()),
			Method:         "POST",
			Path:           path,
			URL:            "http://example.test" + path,
			StatusCode:     200,
			RequestHeaders: headers,
			RequestBody:    bodyWith(body),
		},
	}
}

func logEvent(start time.Time, level slog.Level, msg string) *collector.Event {
	rec := slog.NewRecord(start, level, msg, 0)
	rec.AddAttrs(slog.String("key", "value"), slog.Int("count", 3))
	return &collector.Event{
		ID:    uuid.Must(uuid.NewV4()),
		Start: start,
		End:   start,
		Data:  rec,
	}
}

// --- request helpers ---

func doGET(t *testing.T, h *Handler, path string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

func decodeSummaries(t *testing.T, resp *http.Response) []EventSummary {
	t.Helper()
	var out []EventSummary
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode summaries: %v", err)
	}
	return out
}

func decodeDetail(t *testing.T, resp *http.Response) EventDetail {
	t.Helper()
	var out EventDetail
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	return out
}

func eventsURL(sid uuid.UUID) string {
	return "/api/agent/v1/s/" + sid.String() + "/events"
}

func capturePath(sid uuid.UUID, action string) string {
	return "/api/agent/v1/s/" + sid.String() + "/capture/" + action
}

func decodeCaptureStatus(t *testing.T, resp *http.Response) agentCaptureStatusResponse {
	t.Helper()
	var out agentCaptureStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode capture status: %v", err)
	}
	return out
}

// --- tests ---

func TestAgentAPI_Disabled_ByDefault(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	h := NewHandler(aggregator) // no WithAgentAPI
	defer h.Close()

	sid := uuid.Must(uuid.NewV4())
	resp := doGET(t, h, eventsURL(sid))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 when agent API disabled, got %d", resp.StatusCode)
	}
}

func TestAgentAPI_Events_EmptySession(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	h := NewHandler(aggregator, WithAgentAPI())
	defer h.Close()

	// Valid but unknown session id.
	sid := uuid.Must(uuid.NewV4())
	resp := doGET(t, h, eventsURL(sid))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown session, got %d", resp.StatusCode)
	}
	var body jsonError
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestAgentAPI_Events_ListsSummaries(t *testing.T) {
	h, sid := newAgentTestHandler(t)
	base := time.Now().Add(-time.Minute)

	addEvent(t, h, sid, httpServerEvent(base, "GET", "/a", 200, nil))
	addEvent(t, h, sid, dbQueryEvent(base.Add(time.Second), "SELECT 1"))
	addEvent(t, h, sid, logEvent(base.Add(2*time.Second), slog.LevelInfo, "hello"))

	resp := doGET(t, h, eventsURL(sid))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	summaries := decodeSummaries(t, resp)
	if len(summaries) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(summaries))
	}
	// Newest first: log, db, http.
	wantTypes := []EventType{EventTypeLog, EventTypeDBQuery, EventTypeHTTPServer}
	for i, want := range wantTypes {
		if summaries[i].Type != want {
			t.Errorf("summary[%d]: want type %s, got %s", i, want, summaries[i].Type)
		}
	}
}

func TestAgentAPI_Events_FilterByType(t *testing.T) {
	h, sid := newAgentTestHandler(t)
	base := time.Now().Add(-time.Minute)
	addEvent(t, h, sid, httpServerEvent(base, "GET", "/a", 200, nil))
	addEvent(t, h, sid, dbQueryEvent(base.Add(time.Second), "SELECT 1"))
	addEvent(t, h, sid, logEvent(base.Add(2*time.Second), slog.LevelInfo, "hello"))

	resp := doGET(t, h, eventsURL(sid)+"?type=db_query")
	summaries := decodeSummaries(t, resp)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 db_query summary, got %d", len(summaries))
	}
	if summaries[0].Type != EventTypeDBQuery {
		t.Errorf("expected db_query, got %s", summaries[0].Type)
	}
}

func TestAgentAPI_Events_FilterByTimeAndLimit(t *testing.T) {
	h, sid := newAgentTestHandler(t)
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	addEvent(t, h, sid, httpServerEvent(base, "GET", "/old", 200, nil))
	addEvent(t, h, sid, httpServerEvent(base.Add(10*time.Minute), "GET", "/mid", 200, nil))
	addEvent(t, h, sid, httpServerEvent(base.Add(20*time.Minute), "GET", "/new", 200, nil))

	// since excludes the oldest; limit caps to a single (newest) result.
	since := base.Add(5 * time.Minute).Format(time.RFC3339)
	resp := doGET(t, h, eventsURL(sid)+"?since="+url.QueryEscape(since)+"&limit=1")
	summaries := decodeSummaries(t, resp)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Path != "/new" {
		t.Errorf("expected newest /new, got %s", summaries[0].Path)
	}
}

func TestAgentAPI_EventDetail_HTTPServerWithChildren(t *testing.T) {
	h, sid := newAgentTestHandler(t)
	base := time.Now().Add(-time.Minute)

	parent := httpServerEvent(base, "POST", "/checkout", 200, nil)
	child := dbQueryEvent(base.Add(time.Millisecond), "SELECT * FROM orders")
	parent.Children = []*collector.Event{child}
	// Give the server event a request body so metadata appears.
	srv := parent.Data.(collector.HTTPServerRequest)
	srv.RequestHeaders = http.Header{"Content-Type": []string{"application/json"}}
	srv.RequestBody = collector.NewBody(nil, 1024)
	parent.Data = srv
	addEvent(t, h, sid, parent)

	resp := doGET(t, h, eventsURL(sid)+"/"+parent.ID.String())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	detail := decodeDetail(t, resp)
	if detail.Type != EventTypeHTTPServer {
		t.Fatalf("expected http_server, got %s", detail.Type)
	}
	if len(detail.Children) != 1 || detail.Children[0].Type != EventTypeDBQuery {
		t.Fatalf("expected one db_query child, got %+v", detail.Children)
	}
	if detail.Children[0].Query == "" {
		t.Error("expected child query present")
	}
	if detail.DurationMs <= 0 {
		t.Errorf("expected positive durationMs, got %d", detail.DurationMs)
	}
	if detail.RequestBody == nil || !detail.RequestBody.Available {
		t.Error("expected request body metadata to be available")
	}
}

func TestAgentAPI_EventDetail_LogRecord(t *testing.T) {
	h, sid := newAgentTestHandler(t)
	e := logEvent(time.Now().Add(-time.Second), slog.LevelWarn, "something happened")
	addEvent(t, h, sid, e)

	resp := doGET(t, h, eventsURL(sid)+"/"+e.ID.String())
	detail := decodeDetail(t, resp)
	if detail.Type != EventTypeLog {
		t.Fatalf("expected log, got %s", detail.Type)
	}
	if detail.Level != slog.LevelWarn.String() {
		t.Errorf("expected level %s, got %s", slog.LevelWarn, detail.Level)
	}
	if detail.Message != "something happened" {
		t.Errorf("unexpected message %q", detail.Message)
	}
	if detail.Attrs["key"] != "value" {
		t.Errorf("expected attr key=value, got %v", detail.Attrs["key"])
	}
}

func TestAgentAPI_DefaultHeaderRedaction(t *testing.T) {
	h, sid := newAgentTestHandler(t)
	headers := http.Header{}
	headers.Set("Authorization", "Bearer secret-token")
	headers.Set("Cookie", "session=abc")
	headers.Set("X-Trace-Id", "trace-123")
	e := httpServerEvent(time.Now().Add(-time.Second), "GET", "/secure", 200, headers)
	addEvent(t, h, sid, e)

	resp := doGET(t, h, eventsURL(sid)+"/"+e.ID.String())
	detail := decodeDetail(t, resp)

	if got := detail.RequestHeaders.Get("Authorization"); got != redactedHeaderValue {
		t.Errorf("Authorization not redacted: %q", got)
	}
	if got := detail.RequestHeaders.Get("Cookie"); got != redactedHeaderValue {
		t.Errorf("Cookie not redacted: %q", got)
	}
	if got := detail.RequestHeaders.Get("X-Trace-Id"); got != "trace-123" {
		t.Errorf("non-sensitive header should be preserved, got %q", got)
	}
	// Key preserved.
	if _, ok := detail.RequestHeaders["Authorization"]; !ok {
		t.Error("Authorization key should be preserved")
	}
}

func TestAgentAPI_RedactedHeaders_Extended(t *testing.T) {
	h, sid := newAgentTestHandler(t, WithAgentRedactedHeaders("X-Api-Key"))
	headers := http.Header{}
	headers.Set("X-Api-Key", "key-123")
	headers.Set("Authorization", "Bearer t")
	e := httpServerEvent(time.Now().Add(-time.Second), "GET", "/x", 200, headers)
	addEvent(t, h, sid, e)

	resp := doGET(t, h, eventsURL(sid)+"/"+e.ID.String())
	detail := decodeDetail(t, resp)
	if got := detail.RequestHeaders.Get("X-Api-Key"); got != redactedHeaderValue {
		t.Errorf("X-Api-Key not redacted: %q", got)
	}
	if got := detail.RequestHeaders.Get("Authorization"); got != redactedHeaderValue {
		t.Errorf("default Authorization redaction lost: %q", got)
	}
}

func TestAgentAPI_InsecureHeaders_OptOut(t *testing.T) {
	h, sid := newAgentTestHandler(t, WithAgentInsecureHeaders())
	headers := http.Header{}
	headers.Set("Authorization", "Bearer visible")
	e := httpServerEvent(time.Now().Add(-time.Second), "GET", "/x", 200, headers)
	addEvent(t, h, sid, e)

	resp := doGET(t, h, eventsURL(sid)+"/"+e.ID.String())
	detail := decodeDetail(t, resp)
	if got := detail.RequestHeaders.Get("Authorization"); got != "Bearer visible" {
		t.Errorf("expected Authorization visible with insecure headers, got %q", got)
	}
}

func TestAgentAPI_Redactor_DropsEvent(t *testing.T) {
	redactor := func(d *EventDetail) *EventDetail {
		if d.Type == EventTypeHTTPServer && d.Path == "/auth/login" {
			return nil
		}
		return d
	}
	h, sid := newAgentTestHandler(t, WithAgentRedactor(redactor))
	base := time.Now().Add(-time.Minute)
	authEvent := httpServerEvent(base, "POST", "/auth/login", 200, nil)
	okEvent := httpServerEvent(base.Add(time.Second), "GET", "/dashboard", 200, nil)
	addEvent(t, h, sid, authEvent)
	addEvent(t, h, sid, okEvent)

	// List omits the dropped event.
	resp := doGET(t, h, eventsURL(sid))
	summaries := decodeSummaries(t, resp)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary (auth dropped), got %d", len(summaries))
	}
	if summaries[0].Path != "/dashboard" {
		t.Errorf("expected /dashboard, got %s", summaries[0].Path)
	}

	// Detail of the dropped event is 404.
	detailResp := doGET(t, h, eventsURL(sid)+"/"+authEvent.ID.String())
	if detailResp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for dropped event, got %d", detailResp.StatusCode)
	}
}

func TestAgentAPI_RequestBody_Raw(t *testing.T) {
	h, sid := newAgentTestHandler(t)
	payload := []byte(`{"hello":"world"}`)
	e := httpServerEventWithReqBody(time.Now().Add(-time.Second), "/submit", "application/json", payload)
	addEvent(t, h, sid, e)

	resp := doGET(t, h, eventsURL(sid)+"/"+e.ID.String()+"/request-body")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, payload) {
		t.Errorf("body mismatch: got %q want %q", got, payload)
	}
	if resp.Header.Get("X-Devlog-Truncated") == "true" {
		t.Error("did not expect truncation for a small body")
	}
}

func TestAgentAPI_RequestBody_Truncated(t *testing.T) {
	const maxBytes = 8
	h, sid := newAgentTestHandler(t, WithAgentMaxBodyBytes(maxBytes))
	payload := []byte("0123456789ABCDEF") // 16 bytes > maxBytes
	e := httpServerEventWithReqBody(time.Now().Add(-time.Second), "/big", "text/plain", payload)
	addEvent(t, h, sid, e)

	// Body endpoint truncates and signals via header.
	resp := doGET(t, h, eventsURL(sid)+"/"+e.ID.String()+"/request-body")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Devlog-Truncated") != "true" {
		t.Error("expected X-Devlog-Truncated: true")
	}
	got, _ := io.ReadAll(resp.Body)
	if len(got) != maxBytes || !bytes.Equal(got, payload[:maxBytes]) {
		t.Errorf("expected first %d bytes, got %q", maxBytes, got)
	}

	// Detail metadata reflects truncation.
	detailResp := doGET(t, h, eventsURL(sid)+"/"+e.ID.String())
	detail := decodeDetail(t, detailResp)
	if detail.RequestBody == nil || !detail.RequestBody.Truncated {
		t.Errorf("expected requestBody.truncated=true, got %+v", detail.RequestBody)
	}
}

func TestAgentAPI_Redactor_RedactsBody(t *testing.T) {
	redactor := func(d *EventDetail) *EventDetail {
		if d.RequestBody != nil {
			d.RequestBody.Available = false // suppress the body
		}
		return d
	}
	h, sid := newAgentTestHandler(t, WithAgentRedactor(redactor))
	e := httpServerEventWithReqBody(time.Now().Add(-time.Second), "/login", "application/json", []byte(`{"pw":"secret"}`))
	addEvent(t, h, sid, e)

	// Detail shows the body as redacted.
	detailResp := doGET(t, h, eventsURL(sid)+"/"+e.ID.String())
	detail := decodeDetail(t, detailResp)
	if detail.RequestBody == nil || !detail.RequestBody.Redacted {
		t.Errorf("expected requestBody.redacted=true, got %+v", detail.RequestBody)
	}

	// Body endpoint refuses with 410 Gone.
	bodyResp := doGET(t, h, eventsURL(sid)+"/"+e.ID.String()+"/request-body")
	if bodyResp.StatusCode != http.StatusGone {
		t.Fatalf("expected 410 for redacted body, got %d", bodyResp.StatusCode)
	}
}

func TestAgentAPI_RequestBody_NotCaptured(t *testing.T) {
	h, sid := newAgentTestHandler(t)
	// HTTP event without any request body.
	e := httpServerEvent(time.Now().Add(-time.Second), "GET", "/nobody", 200, nil)
	addEvent(t, h, sid, e)

	resp := doGET(t, h, eventsURL(sid)+"/"+e.ID.String()+"/request-body")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing body, got %d", resp.StatusCode)
	}
}

func TestAgentAPI_RequestBody_NonBodyEventType(t *testing.T) {
	h, sid := newAgentTestHandler(t)
	e := dbQueryEvent(time.Now().Add(-time.Second), "SELECT 1")
	addEvent(t, h, sid, e)

	resp := doGET(t, h, eventsURL(sid)+"/"+e.ID.String()+"/request-body")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-body event type, got %d", resp.StatusCode)
	}
}

func TestAgentAPI_CaptureStatus_ReadOnly(t *testing.T) {
	// The session is created by the dashboard (here via SessionManager directly);
	// the agent only reads status — there is no agent start/stop.
	h, sid := newAgentTestHandler(t) // creates a global-mode session
	addEvent(t, h, sid, httpServerEvent(time.Now(), "GET", "/x", 200, nil))
	addEvent(t, h, sid, dbQueryEvent(time.Now(), "SELECT 1"))

	resp := doGET(t, h, capturePath(sid, "status"))
	st := decodeCaptureStatus(t, resp)
	if !st.Active || st.Mode != "global" || st.EventCount != 2 {
		t.Fatalf("unexpected status: %+v", st)
	}
}

func TestAgentAPI_CaptureStartStop_NotExposed(t *testing.T) {
	h, sid := newAgentTestHandler(t)
	for _, action := range []string{"start", "stop"} {
		req := httptest.NewRequest(http.MethodPost, capturePath(sid, action), nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Result().StatusCode != http.StatusNotFound {
			t.Errorf("capture/%s should not be exposed to agents, got %d", action, rec.Result().StatusCode)
		}
	}
}

func TestAgentAPI_Stats(t *testing.T) {
	h, sid := newAgentTestHandler(t)
	addEvent(t, h, sid, httpServerEvent(time.Now(), "GET", "/a", 200, nil))
	addEvent(t, h, sid, dbQueryEvent(time.Now(), "SELECT 1"))

	resp := doGET(t, h, "/api/agent/v1/stats")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var sr StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		t.Fatalf("decode stats: %v", err)
	}

	want := h.eventAggregator.CalculateStats()
	if sr.EventCount != want.EventCount {
		t.Errorf("eventCount: got %d want %d", sr.EventCount, want.EventCount)
	}
	if sr.EventCount != 2 {
		t.Errorf("expected 2 events, got %d", sr.EventCount)
	}
	if sr.SessionCount != 1 {
		t.Errorf("expected 1 session, got %d", sr.SessionCount)
	}
	if sr.MemoryFormatted == "" {
		t.Error("expected a formatted memory string")
	}
}

func TestAgentAPI_KeepsSessionAlive(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	h := NewHandler(aggregator, WithAgentAPI(), WithSessionIdleTimeout(60*time.Millisecond))
	defer h.Close()

	sid := uuid.Must(uuid.NewV4())
	if _, _, err := h.sessions.GetOrCreate(sid, collector.CaptureModeGlobal); err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	// Poll repeatedly across more than one idle-timeout window.
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		resp := doGET(t, h, eventsURL(sid))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected session to stay alive, got %d", resp.StatusCode)
		}
		time.Sleep(20 * time.Millisecond)
	}

	if storage := h.sessions.Get(sid); storage == nil {
		t.Fatal("session was cleaned up despite active polling")
	}
}
