package main

import (
	"log/slog"
	"net/http"
	"os"

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

	// 2. Create an HTTP client with devlog middleware (RoundTripper)

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
