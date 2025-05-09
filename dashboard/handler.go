package dashboard

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"time"

	"github.com/a-h/templ"

	"github.com/networkteam/devlog/collector"
	"github.com/networkteam/devlog/dashboard/views"
)

type Handler struct {
	logCollector        *collector.LogCollector
	httpClientCollector *collector.HTTPClientCollector
	httpServerCollector *collector.HTTPServerCollector

	mux http.Handler
}

type HandlerOptions struct {
	LogCollector        *collector.LogCollector
	HTTPClientCollector *collector.HTTPClientCollector
	HTTPServerCollector *collector.HTTPServerCollector
}

func NewHandler(options HandlerOptions) *Handler {
	mux := http.NewServeMux()
	handler := &Handler{
		logCollector:        options.LogCollector,
		httpClientCollector: options.HTTPClientCollector,
		httpServerCollector: options.HTTPServerCollector,
		mux:                 mux,
	}

	// Mount handlers for each section
	mux.HandleFunc("/", handler.root)
	mux.HandleFunc("/logs", handler.getLogs)
	mux.HandleFunc("/logs/events", handler.getLogsSSE)
	mux.HandleFunc("/http-client-requests", handler.getHTTPClientRequests)
	mux.HandleFunc("/http-server-requests", handler.getHTTPServerRequests)

	return handler
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) root(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	templ.Handler(views.Dashboard()).ServeHTTP(w, r)
}

func (h *Handler) getLogs(w http.ResponseWriter, r *http.Request) {
	recentLogs := h.logCollector.Tail(10)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// TODO Use proper templating
	_, _ = w.Write([]byte("<html><body><h1>Recent Logs</h1><a href='/'>&larr; Back to Dashboard</a><ul>"))
	for _, log := range recentLogs {
		_, _ = w.Write([]byte(formatLogAsHTML(log)))
	}
	_, _ = w.Write([]byte("</ul></body></html>"))
}

func formatLogAsHTML(log slog.Record) string {
	return "<li>" + log.Time.Format(time.RFC3339) + " " + html.EscapeString(log.Message) + "</li>"
}

// getLogsSSE handles SSE connections for real-time log updates
func (h *Handler) getLogsSSE(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // For NGINX proxy

	// Create a context that gets canceled when the connection is closed
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Monitor for client disconnect
	go func() {
		<-ctx.Done()
		// Context was canceled, connection is closed
	}()

	// Create a notification channel for new logs
	logCh := h.logCollector.Subscribe(ctx)

	// Send a keep-alive message initially to ensure the connection is established
	fmt.Fprintf(w, "event: keepalive\ndata: connected\n\n")
	w.(http.Flusher).Flush()

	// Listen for new logs and send them as SSE events
	for {
		select {
		case <-ctx.Done():
			return // Client disconnected
		case log, ok := <-logCh:
			if !ok {
				return // Channel closed
			}

			// Format log as HTML
			logHTML := formatLogAsHTML(log)

			// Send as SSE event
			fmt.Fprintf(w, "data: %s\n\n", logHTML)
			w.(http.Flusher).Flush()
		}
	}
}

func (h *Handler) getHTTPClientRequests(w http.ResponseWriter, r *http.Request) {
	requests := h.httpClientCollector.GetRequests(10)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// TODO Use proper templating
	_, _ = w.Write([]byte("<html><body><h1>Recent HTTP Client Requests</h1><a href='/'>&larr; Back to Dashboard</a><ul>"))
	for _, req := range requests {
		var responseBody string
		if req.ResponseBody != nil {
			responseBody = html.EscapeString(truncate(req.ResponseBody.String(), 100))
		}
		duration := req.Duration().String()
		_, _ = w.Write([]byte("<li>" +
			req.RequestTime.Format(time.RFC3339) + " " +
			html.EscapeString(req.Method) + " " +
			html.EscapeString(req.URL) + " " +
			statusBadge(req.StatusCode) + " " +
			"<em>Duration: " + duration + "</em><br>" +
			"<pre>" + responseBody + "</pre></li>"))
	}
	_, _ = w.Write([]byte("</ul></body></html>"))
}

func (h *Handler) getHTTPServerRequests(w http.ResponseWriter, r *http.Request) {
	if h.httpServerCollector == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body><h1>HTTP Server Requests</h1><a href='/'>&larr; Back to Dashboard</a><p>HTTP Server collector is not enabled.</p></body></html>"))
		return
	}

	requests := h.httpServerCollector.GetRequests(10)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// TODO Use proper templating
	_, _ = w.Write([]byte("<html><body><h1>Recent HTTP Server Requests</h1><a href='/'>&larr; Back to Dashboard</a><ul>"))
	for _, req := range requests {
		var requestBody, responseBody string
		if req.RequestBody != nil {
			requestBody = html.EscapeString(truncate(req.RequestBody.String(), 100))
		}
		if req.ResponseBody != nil {
			responseBody = html.EscapeString(truncate(req.ResponseBody.String(), 100))
		}
		duration := req.Duration().String()
		_, _ = w.Write([]byte("<li>" +
			req.RequestTime.Format(time.RFC3339) + " " +
			html.EscapeString(req.Method) + " " +
			html.EscapeString(req.Path) + " from " +
			html.EscapeString(req.RemoteAddr) + " " +
			statusBadge(req.StatusCode) + " " +
			"<em>Duration: " + duration + "</em><br>" +
			"<strong>Request:</strong> <pre>" + requestBody + "</pre><br>" +
			"<strong>Response:</strong> <pre>" + responseBody + "</pre></li>"))
	}
	_, _ = w.Write([]byte("</ul></body></html>"))
}

// Helper functions

// statusBadge returns an HTML span with a colored badge for the HTTP status code
func statusBadge(statusCode int) string {
	var color, text string

	switch {
	case statusCode >= 500:
		color = "red"
		text = fmt.Sprintf("%d Server Error", statusCode)
	case statusCode >= 400:
		color = "orange"
		text = fmt.Sprintf("%d Client Error", statusCode)
	case statusCode >= 300:
		color = "blue"
		text = fmt.Sprintf("%d Redirect", statusCode)
	case statusCode >= 200:
		color = "green"
		text = fmt.Sprintf("%d Success", statusCode)
	default:
		color = "gray"
		text = fmt.Sprintf("%d", statusCode)
	}

	return fmt.Sprintf(`<span style="display:inline-block; padding:2px 8px; border-radius:4px; background-color:%s; color:white; font-size:12px; font-weight:bold;">%s</span>`, color, text)
}

// truncate truncates a string to maxLen and adds "..." if truncated
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
