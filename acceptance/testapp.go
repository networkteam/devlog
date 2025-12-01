//go:build acceptance
// +build acceptance

package acceptance

import (
	"database/sql/driver"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/networkteam/devlog"
	"github.com/networkteam/devlog/collector"
)

// TestApp represents a test application with devlog fully integrated.
// It provides endpoints that generate all event types for testing.
type TestApp struct {
	Server     *httptest.Server
	DevlogURL  string
	AppURL     string
	Devlog     *devlog.Instance
	HTTPClient *http.Client
	Logger     *slog.Logger
}

// NewTestApp creates a new test application with devlog fully integrated.
// It sets up endpoints for all event types: HTTP server, HTTP client, DB queries, and logs.
func NewTestApp(t *testing.T) *TestApp {
	t.Helper()

	dlog := devlog.NewWithOptions(devlog.Options{
		HTTPServerCapacity: 100,
		HTTPServerOptions: &collector.HTTPServerOptions{
			MaxBodySize:         1024 * 1024,
			CaptureRequestBody:  true,
			CaptureResponseBody: true,
		},
		HTTPClientCapacity: 100,
		HTTPClientOptions: &collector.HTTPClientOptions{
			MaxBodySize:         1024 * 1024,
			CaptureRequestBody:  true,
			CaptureResponseBody: true,
		},
		LogCapacity:     100,
		DBQueryCapacity: 100,
	})

	// Create an HTTP client that collects requests
	httpClient := &http.Client{
		Transport: dlog.CollectHTTPClient(http.DefaultTransport),
		Timeout:   5 * time.Second,
	}

	// Create a logger that collects logs
	logger := slog.New(dlog.CollectSlogLogs(collector.CollectSlogLogsOptions{
		Level: slog.LevelDebug,
	}))

	// Create DB query collector
	collectDBQuery := dlog.CollectDBQuery()

	mux := http.NewServeMux()

	// Test endpoint: simple JSON response (HTTP server event)
	mux.HandleFunc("GET /api/test", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Test endpoint: echo request body (HTTP server event)
	mux.HandleFunc("POST /api/echo", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	})

	// Test endpoint: makes HTTP client request (HTTP client event)
	mux.HandleFunc("GET /api/external", func(w http.ResponseWriter, r *http.Request) {
		// Make request to a mock endpoint - this will fail but still generate an event
		resp, err := httpClient.Get("http://example.com/external-api")
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"external":"attempted"}`))
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"external":"called"}`))
	})

	// Test endpoint: simulates DB query (DB query event)
	mux.HandleFunc("GET /api/db", func(w http.ResponseWriter, r *http.Request) {
		// Simulate a DB query event
		collectDBQuery(r.Context(), collector.DBQuery{
			Query:    "SELECT * FROM users WHERE id = $1",
			Args:     []driver.NamedValue{{Ordinal: 1, Value: 1}},
			Duration: 5 * time.Millisecond,
			Language: "postgresql",
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"users":[{"id":1,"name":"Test User"}]}`))
	})

	// Test endpoint: writes slog entries (log event)
	mux.HandleFunc("GET /api/log", func(w http.ResponseWriter, r *http.Request) {
		logger.InfoContext(r.Context(), "Test log message", slog.String("key", "value"))
		logger.DebugContext(r.Context(), "Debug message", slog.Int("count", 42))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"logged":true}`))
	})

	// Test endpoint: combined events (nested events)
	mux.HandleFunc("GET /api/combined", func(w http.ResponseWriter, r *http.Request) {
		logger.InfoContext(r.Context(), "Starting combined operation")
		collectDBQuery(r.Context(), collector.DBQuery{
			Query:    "INSERT INTO logs (message) VALUES ($1)",
			Args:     []driver.NamedValue{{Ordinal: 1, Value: "combined test"}},
			Duration: 2 * time.Millisecond,
			Language: "postgresql",
		})
		logger.InfoContext(r.Context(), "Combined operation complete")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"combined":true}`))
	})

	outerMux := http.NewServeMux()
	outerMux.Handle("/", dlog.CollectHTTPServer(mux))
	outerMux.Handle("/_devlog/", http.StripPrefix("/_devlog", dlog.DashboardHandler("/_devlog")))

	server := httptest.NewServer(outerMux)

	return &TestApp{
		Server:     server,
		DevlogURL:  server.URL + "/_devlog/",
		AppURL:     server.URL,
		Devlog:     dlog,
		HTTPClient: httpClient,
		Logger:     logger,
	}
}

// Close shuts down the test application and releases resources.
func (ta *TestApp) Close() {
	ta.Devlog.Close()
	ta.Server.Close()
}
