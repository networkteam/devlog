package dashboard

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/gofrs/uuid"

	"github.com/networkteam/devlog/collector"
	"github.com/networkteam/devlog/dashboard/views"
)

type Handler struct {
	logCollector        *collector.LogCollector
	httpClientCollector *collector.HTTPClientCollector
	httpServerCollector *collector.HTTPServerCollector
	eventCollector      *collector.EventCollector

	mux http.Handler
}

type HandlerOptions struct {
	LogCollector        *collector.LogCollector
	HTTPClientCollector *collector.HTTPClientCollector
	HTTPServerCollector *collector.HTTPServerCollector
	EventCollector      *collector.EventCollector
}

func NewHandler(options HandlerOptions) *Handler {
	mux := http.NewServeMux()
	handler := &Handler{
		logCollector:        options.LogCollector,
		httpClientCollector: options.HTTPClientCollector,
		httpServerCollector: options.HTTPServerCollector,
		eventCollector:      options.EventCollector,
		mux:                 mux,
	}

	// Mount handlers for each section
	mux.HandleFunc("/", handler.root)
	mux.HandleFunc("/event-list", handler.getEventList)
	mux.HandleFunc("/event/{eventId}", handler.getEventDetails)

	// Test routes
	mux.HandleFunc("/events", handler.getEvents)
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

	idStr := r.URL.Query().Get("id")
	var selectedEvent *collector.Event
	if idStr != "" {
		eventID, err := uuid.FromString(idStr)
		if err != nil {
			http.Error(w, "Invalid event id", http.StatusBadRequest)
			return
		}
		event, exists := h.eventCollector.GetEvent(eventID)
		if !exists {
			http.Error(w, "Event not found", http.StatusNotFound)
			return
		}
		selectedEvent = &event
	}

	templ.Handler(views.Dashboard(views.DashboardProps{
		SelectedEvent: selectedEvent,
	})).ServeHTTP(w, r)
}

func (h *Handler) getEventList(w http.ResponseWriter, r *http.Request) {
	recentEvents := h.eventCollector.GetEvents(100)
	slices.Reverse(recentEvents)

	idStr := r.URL.Query().Get("id")
	var selectedEventID *uuid.UUID
	if idStr != "" {
		eventID, err := uuid.FromString(idStr)
		if err != nil {
			http.Error(w, "Invalid event id", http.StatusBadRequest)
			return
		}
		selectedEventID = &eventID
	}

	templ.Handler(views.EventList(views.EventListProps{
		Events:          recentEvents,
		SelectedEventID: selectedEventID,
	})).ServeHTTP(w, r)
}

func (h *Handler) getEventDetails(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("eventId")
	eventID, err := uuid.FromString(idStr)
	if err != nil {
		http.Error(w, "Invalid event id", http.StatusBadRequest)
		return
	}

	// TODO We need to handle nested events somehow

	event, exists := h.eventCollector.GetEvent(eventID)
	if !exists {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	}

	templ.Handler(views.EventDetailContainer(event)).ServeHTTP(w, r)
}

func (h *Handler) getEvents(w http.ResponseWriter, r *http.Request) {
	recentEvents := h.eventCollector.GetEvents(10)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	// TODO Use proper templating
	_, _ = w.Write([]byte("<html><body><h1>Recent Events</h1><a href='/'>&larr; Back to Dashboard</a><ul>"))
	for _, event := range recentEvents {
		_, _ = w.Write([]byte("<li>" + formatEventAsHTML(event) + "</li>"))
	}
	_, _ = w.Write([]byte("</ul></body></html>"))
}

func formatEventAsHTML(event collector.Event) string {
	var sb strings.Builder
	sb.WriteString("<div>")
	sb.WriteString("<div>")
	sb.WriteString("Start:")
	sb.WriteString(event.Start.Format("15:04:05.999999"))
	sb.WriteString("</div>")
	sb.WriteString("<div>")
	sb.WriteString("End:")
	sb.WriteString(event.End.Format("15:04:05.999999"))
	sb.WriteString("</div>")
	sb.WriteString("<div>")

	switch v := event.Data.(type) {
	case slog.Record:
		sb.WriteString(v.Time.Format(time.RFC3339) + " " + html.EscapeString(v.Message))
	case collector.HTTPRequest:
		sb.WriteString(v.RequestTime.Format(time.RFC3339) + " → " + html.EscapeString(v.Method) + " " + html.EscapeString(v.URL) + ": " + statusBadge(v.StatusCode))
	case collector.HTTPServerRequest:
		sb.WriteString(v.RequestTime.Format(time.RFC3339) + " ← " + html.EscapeString(v.Method) + " " + html.EscapeString(v.Path) + ": " + statusBadge(v.StatusCode))
	}

	if len(event.Children) > 0 {
		sb.WriteString("<ul>")
	}
	for _, childEvent := range event.Children {
		sb.WriteString("<li>" + formatEventAsHTML(childEvent) + "</li>")
	}
	if len(event.Children) > 0 {
		sb.WriteString("</ul>")
	}

	sb.WriteString("</div>")
	sb.WriteString("</div>")
	return sb.String()
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
