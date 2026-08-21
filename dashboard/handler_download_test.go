package dashboard

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofrs/uuid"

	"github.com/networkteam/devlog/collector"
)

func startGlobalCapture(t *testing.T, handler *Handler, sessionID uuid.UUID) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/s/"+sessionID.String()+"/capture/start?mode=global", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("capture/start: unexpected status %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHandler_DownloadBody_SyntheticBody verifies that the download endpoints
// work for HTTPClientRequest bodies built via collector.NewBodyFromBytes, not
// just bodies captured lazily from a live http.Response - both request and
// response body downloads resolve the body from the stored event by ID, so
// they should be agnostic to how the *Body was constructed.
func TestHandler_DownloadBody_SyntheticBody(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	httpClientCollector := collector.NewHTTPClientCollectorWithOptions(collector.HTTPClientOptions{
		EventAggregator: aggregator,
	})
	defer httpClientCollector.Close()

	handler := NewHandler(aggregator)
	defer handler.Close()

	sessionID := uuid.Must(uuid.NewV4())
	startGlobalCapture(t, handler, sessionID)

	requestBody := []byte(`{"request":"payload"}`)
	responseBody := []byte(`{"response":"payload"}`)

	httpClientCollector.Collect(context.Background(), collector.HTTPClientRequest{
		Method:          http.MethodPost,
		URL:             "https://example.com/v2/items",
		StatusCode:      http.StatusOK,
		RequestBody:     collector.NewBodyFromBytes(requestBody, collector.DefaultMaxBodySize),
		RequestHeaders:  http.Header{"Content-Type": {"application/json"}},
		ResponseBody:    collector.NewBodyFromBytes(responseBody, collector.DefaultMaxBodySize),
		ResponseHeaders: http.Header{"Content-Type": {"application/json"}},
	})

	storage := handler.sessions.Get(sessionID)
	if storage == nil {
		t.Fatal("expected storage to exist after capture start")
	}
	events := storage.GetEvents(10)
	if len(events) != 1 {
		t.Fatalf("expected 1 stored event, got %d", len(events))
	}
	eventID := events[0].ID

	t.Run("request body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/s/"+sessionID.String()+"/download/request-body/"+eventID.String(), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
		}
		got, err := io.ReadAll(rec.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(got) != string(requestBody) {
			t.Errorf("expected body %q, got %q", requestBody, got)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", ct)
		}
	})

	t.Run("response body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/s/"+sessionID.String()+"/download/response-body/"+eventID.String(), nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
		}
		got, err := io.ReadAll(rec.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(got) != string(responseBody) {
			t.Errorf("expected body %q, got %q", responseBody, got)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", ct)
		}
	})
}
