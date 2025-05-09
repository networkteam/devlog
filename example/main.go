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

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

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
	http.HandleFunc("/http-client", func(w http.ResponseWriter, r *http.Request) {
		logger := slog.With("component", "http", "handler", "/http-client")

		logger.Debug("Requesting uselessfacts API")

		resp, err := httpClient.Get("https://uselessfacts.jsph.pl/api/v2/facts/random")
		if err != nil {
			logger.Error("Failed to get uselessfacts API", slog.Any("err", err.Error()))
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Failed to get uselessfacts API"))
			return
		}
		defer resp.Body.Close()
		var fact uselessfactResponse
		if err := json.NewDecoder(resp.Body).Decode(&fact); err != nil {
			logger.Error("Failed to decode uselessfacts API response", slog.Any("err", err.Error()))
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Failed to decode uselessfacts API response"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fact.Text))
	})

	// 3. Create a new HTTP server with a simple handler

	http.HandleFunc("/log", func(w http.ResponseWriter, r *http.Request) {
		logger := slog.With("component", "http", "handler", "/log")

		logger.Debug("Debug log from /log HTTP handler", slog.Group("request", slog.String("method", r.Method)))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Log a thing"))
	})

	// 4. Wrap with devlog middleware to inspect requests and responses to the server

	// 5. Mount devlog dashboard

	http.Handle("/_devlog/", http.StripPrefix("/_devlog", dlog.DashboardHandler()))

	// Run the server

	logger.Info("Starting server on :1095")
	if err := http.ListenAndServe(":1095", nil); err != nil {
		logger.Error("Failed to start server", slog.Group("error", slog.String("message", err.Error())))
		os.Exit(1)
	}
}
