package main

import (
	"html"
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

	http.HandleFunc("/inspect", func(w http.ResponseWriter, r *http.Request) {
		recentLogs := dlog.Logs(10)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)

		// TODO Use proper templating
		_, _ = w.Write([]byte("<html><body><h1>Recent Logs</h1><ul>"))
		for _, log := range recentLogs {
			_, _ = w.Write([]byte("<li>" + log.Time.Format(time.RFC3339) + " " + html.EscapeString(log.Message) + "</li>"))
		}
		_, _ = w.Write([]byte("</ul></body></html>"))
	})

	// 2. Create an HTTP client with devlog middleware (RoundTripper)

	// 3. Create a new HTTP server with a simple handler

	http.HandleFunc("/log", func(w http.ResponseWriter, r *http.Request) {
		logger := slog.With("component", "http", "handler", "/log")

		logger.Debug("Debug log from /log HTTP handler", slog.Group("request", slog.String("method", r.Method)))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Log a thing"))
	})

	// 4. Wrap with devlog middleware to inspect requests and responses to the server

	// Run the server

	logger.Info("Starting server on :9911")
	if err := http.ListenAndServe(":9911", nil); err != nil {
		logger.Error("Failed to start server", slog.Group("error", slog.String("message", err.Error())))
		os.Exit(1)
	}
}
