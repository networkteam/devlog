package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	slogmulti "github.com/samber/slog-multi"

	"github.com/networkteam/devlog"
	"github.com/networkteam/devlog/collector"
)

func main() {
	// 1. Set up slog with devlog middleware

	dlog := devlog.New()
	defer dlog.Close()

	logger := slog.New(
		slogmulti.Fanout(
			// Collect debug logs with devlog
			dlog.CollectSlogLogs(collector.CollectSlogLogsOptions{
				Level: slog.LevelDebug,
			}),
			// Log info to stderr
			slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}),
		),
	)
	slog.SetDefault(logger)

	mux := http.NewServeMux()

	// 2. Create an HTTP client with devlog middleware (RoundTripper)

	httpClient := &http.Client{
		Transport: dlog.CollectHTTPClient(http.DefaultTransport),
		Timeout:   time.Second * 5,
	}
	type uselessfactResponse struct {
		ID        string `json:"id"`
		Text      string `json:"text"`
		Source    string `json:"source"`
		SourceURL string `json:"source_url"`
		Language  string `json:"language"`
		Permalink string `json:"permalink"`
	}
	mux.HandleFunc("/http-client", func(w http.ResponseWriter, r *http.Request) {
		logger := slog.With("component", "http", "handler", "/http-client")

		logger.DebugContext(r.Context(), "Requesting uselessfacts API")

		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://uselessfacts.jsph.pl/api/v2/facts/random", nil)
		resp, err := httpClient.Do(req)
		if err != nil {
			logger.ErrorContext(r.Context(), "Failed to get uselessfacts API", slog.Any("err", err.Error()))
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Failed to get uselessfacts API"))
			return
		}
		defer resp.Body.Close()
		var fact uselessfactResponse
		if err := json.NewDecoder(resp.Body).Decode(&fact); err != nil {
			logger.ErrorContext(r.Context(), "Failed to decode uselessfacts API response", slog.Any("err", err.Error()))
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Failed to decode uselessfacts API response"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fact.Text))
	})

	// 3. Create a new HTTP server with a simple handler

	mux.HandleFunc("/log", func(w http.ResponseWriter, r *http.Request) {
		logger := slog.With("component", "http", "handler", "/log")

		logger.DebugContext(r.Context(), "Debug log from /log HTTP handler", slog.Group("request", slog.String("method", r.Method)))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Log a thing"))
	})

	// 4. Wrap with devlog middleware to inspect requests and responses to the server

	outerMux := http.NewServeMux()
	outerMux.Handle("/", dlog.CollectHTTPServer(
		mux,
	))

	// 5. Mount devlog dashboard

	// Mount under path prefix /_devlog, so we handle the dashboard handler under this path, strip the prefix, so dashboard routes match and inform it about the path prefix to render correct URLs
	outerMux.Handle("/_devlog/", http.StripPrefix("/_devlog", dlog.DashboardHandler("/_devlog")))

	// Add a health check endpoint for refresh

	outerMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Run the server

	logger.Info("Starting server on :1095")
	if err := http.ListenAndServe(":1095", outerMux); err != nil {
		logger.Error("Failed to start server", slog.Group("error", slog.String("message", err.Error())))
		os.Exit(1)
	}
}
