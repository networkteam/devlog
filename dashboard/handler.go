package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"

	"github.com/a-h/templ"
	"github.com/gofrs/uuid"

	"github.com/networkteam/devlog/collector"
	"github.com/networkteam/devlog/dashboard/static"
	"github.com/networkteam/devlog/dashboard/views"
)

type Handler struct {
	eventCollector *collector.EventCollector

	pathPrefix    string
	truncateAfter uint64

	mux http.Handler
}

type HandlerOptions struct {
	EventCollector *collector.EventCollector

	// PathPrefix where the Handler is mounted (e.g. "/_devlog"), can be left empty if the Handler is at the root ("/").
	PathPrefix string
	// TruncateAfter is the maximum number of events to show in the event list and dashboard. If 0 or larger than the event collector capacity, it will use the collector's capacity.
	TruncateAfter uint64
}

func NewHandler(options HandlerOptions) *Handler {
	mux := http.NewServeMux()
	if options.TruncateAfter == 0 || options.TruncateAfter > options.EventCollector.Capacity() {
		options.TruncateAfter = options.EventCollector.Capacity()
	}

	handler := &Handler{
		eventCollector: options.EventCollector,
		truncateAfter:  options.TruncateAfter,

		pathPrefix: options.PathPrefix,

		mux: setHandlerOptions(options, mux),
	}

	mux.HandleFunc("GET /{$}", handler.root)
	mux.HandleFunc("GET /event-list", handler.getEventList)
	mux.HandleFunc("DELETE /event-list", handler.clearEventList)
	mux.HandleFunc("GET /event/{eventId}", handler.getEventDetails)
	mux.HandleFunc("GET /events-sse", handler.getEventsSSE)
	mux.HandleFunc("GET /download/request-body/{eventId}", handler.downloadRequestBody)
	mux.HandleFunc("GET /download/response-body/{eventId}", handler.downloadResponseBody)

	mux.Handle("/static/", http.StripPrefix("/static", http.FileServerFS(static.Assets)))

	return handler
}

func setHandlerOptions(options HandlerOptions, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = views.WithHandlerOptions(ctx, views.HandlerOptions{
			PathPrefix:    options.PathPrefix,
			TruncateAfter: options.TruncateAfter,
		})
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) root(w http.ResponseWriter, r *http.Request) {
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
			http.Redirect(w, r, fmt.Sprintf("%s/", h.pathPrefix), http.StatusTemporaryRedirect) // TODO Build correct URL
			return
		} else {
			selectedEvent = event
		}
	}

	recentEvents := h.loadRecentEvents()

	templ.Handler(
		views.Dashboard(views.DashboardProps{
			SelectedEvent: selectedEvent,
			Events:        recentEvents,
		}),
	).ServeHTTP(w, r)
}

func (h *Handler) getEventList(w http.ResponseWriter, r *http.Request) {
	recentEvents := h.loadRecentEvents()

	selectedStr := r.URL.Query().Get("selected")
	var selectedEventID *uuid.UUID
	if selectedStr != "" {
		eventID, err := uuid.FromString(selectedStr)
		if err == nil {
			selectedEventID = &eventID
		}
	}

	templ.Handler(
		views.EventList(views.EventListProps{
			Events:          recentEvents,
			SelectedEventID: selectedEventID,
		}),
	).ServeHTTP(w, r)
}

func (h *Handler) clearEventList(w http.ResponseWriter, r *http.Request) {
	h.eventCollector.Clear()

	// Check if there's an id parameter in the current URL that needs to be removed to unselect an event
	currentURL, _ := url.Parse(r.Header.Get("HX-Current-URL"))
	if currentURL != nil && currentURL.Query().Get("id") != "" {
		// Build URL preserving all query parameters except 'id'
		query := r.URL.Query()
		query.Del("id")

		redirectURL := fmt.Sprintf("%s/", h.pathPrefix)
		if len(query) > 0 {
			redirectURL += "?" + query.Encode()
		}

		// Use HTMX header to update the URL client-side without the id parameter
		w.Header().Set("HX-Push-Url", redirectURL)
	}

	templ.Handler(
		views.SplitLayout(views.EventList(views.EventListProps{}), views.EventDetailContainer(nil)),
	).ServeHTTP(w, r)
}

func (h *Handler) getEventDetails(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("eventId")
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

	templ.Handler(
		views.EventDetailContainer(event),
	).ServeHTTP(w, r)
}

// getEventsSSE handles SSE connections for real-time log updates
func (h *Handler) getEventsSSE(w http.ResponseWriter, r *http.Request) {
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
	eventCh := h.eventCollector.Subscribe(ctx)

	// Send a keep-alive message initially to ensure the connection is established
	fmt.Fprintf(w, "event: keepalive\ndata: connected\n\n")
	w.(http.Flusher).Flush()

	// Listen for new logs and send them as SSE events
	for {
		select {
		case <-ctx.Done():
			return // Client disconnected
		case event, ok := <-eventCh:
			if !ok {
				return // Channel closed
			}

			// Send as SSE event
			fmt.Fprintf(w, "event: new-event\n")
			fmt.Fprintf(w, "data: ")

			views.EventListItem(&event, nil).Render(ctx, w)

			fmt.Fprintf(w, "\n\n")

			w.(http.Flusher).Flush()
		}
	}
}

func (h *Handler) loadRecentEvents() []*collector.Event {
	recentEvents := h.eventCollector.GetEvents(h.truncateAfter)
	slices.Reverse(recentEvents)

	return recentEvents
}

// downloadRequestBody handles downloading the request body for an event
func (h *Handler) downloadRequestBody(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("eventId")
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

	var body []byte
	var contentType string

	switch data := event.Data.(type) {
	case collector.HTTPClientRequest:
		if data.RequestBody == nil {
			http.Error(w, "No request body available", http.StatusNotFound)
			return
		}
		body = data.RequestBody.Bytes()
		contentType = data.RequestHeaders.Get("Content-Type")
	case collector.HTTPServerRequest:
		if data.RequestBody == nil {
			http.Error(w, "No request body available", http.StatusNotFound)
			return
		}
		body = data.RequestBody.Bytes()
		contentType = data.RequestHeaders.Get("Content-Type")
	default:
		http.Error(w, "Event type does not have a request body", http.StatusBadRequest)
		return
	}

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=request-body-%s", eventID))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.Write(body)
}

// downloadResponseBody handles downloading the response body for an event
func (h *Handler) downloadResponseBody(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("eventId")
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

	var body []byte
	var contentType string

	switch data := event.Data.(type) {
	case collector.HTTPClientRequest:
		if data.ResponseBody == nil {
			http.Error(w, "No response body available", http.StatusNotFound)
			return
		}
		body = data.ResponseBody.Bytes()
		contentType = data.ResponseHeaders.Get("Content-Type")
	case collector.HTTPServerRequest:
		if data.ResponseBody == nil {
			http.Error(w, "No response body available", http.StatusNotFound)
			return
		}
		body = data.ResponseBody.Bytes()
		contentType = data.ResponseHeaders.Get("Content-Type")
	default:
		http.Error(w, "Event type does not have a response body", http.StatusBadRequest)
		return
	}

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=response-body-%s", eventID))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.Write(body)
}
