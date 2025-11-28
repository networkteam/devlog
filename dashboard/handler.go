package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/gofrs/uuid"

	"github.com/networkteam/devlog/collector"
	"github.com/networkteam/devlog/dashboard/static"
	"github.com/networkteam/devlog/dashboard/views"
)

// DefaultStorageCapacity is the default number of events per storage
const DefaultStorageCapacity uint64 = 1000

// DefaultSessionIdleTimeout is the default time before an inactive session is cleaned up
const DefaultSessionIdleTimeout = 30 * time.Second

// sessionState tracks a user's capture session
type sessionState struct {
	storageID  uuid.UUID
	lastActive time.Time
}

type Handler struct {
	eventAggregator *collector.EventAggregator

	// Session management
	sessions   map[uuid.UUID]*sessionState // sessionID -> sessionState
	sessionsMu sync.RWMutex

	pathPrefix       string
	truncateAfter    uint64
	storageCapacity  uint64
	idleTimeout      time.Duration
	cleanupCtx       context.Context
	cleanupCtxCancel context.CancelFunc

	mux http.Handler
}

type HandlerOptions struct {
	// EventAggregator is the aggregator for collecting events
	EventAggregator *collector.EventAggregator

	// PathPrefix where the Handler is mounted (e.g. "/_devlog"), can be left empty if the Handler is at the root ("/").
	PathPrefix string
	// TruncateAfter is the maximum number of events to show in the event list and dashboard.
	TruncateAfter uint64
	// StorageCapacity is the number of events per user storage. If 0, DefaultStorageCapacity is used.
	StorageCapacity uint64
	// SessionIdleTimeout is how long to wait after SSE disconnect before cleaning up. If 0, DefaultSessionIdleTimeout is used.
	SessionIdleTimeout time.Duration
}

func NewHandler(options HandlerOptions) *Handler {
	mux := http.NewServeMux()

	storageCapacity := options.StorageCapacity
	if storageCapacity == 0 {
		storageCapacity = DefaultStorageCapacity
	}

	truncateAfter := options.TruncateAfter
	if truncateAfter == 0 || truncateAfter > storageCapacity {
		truncateAfter = storageCapacity
	}

	idleTimeout := options.SessionIdleTimeout
	if idleTimeout == 0 {
		idleTimeout = DefaultSessionIdleTimeout
	}

	cleanupCtx, cleanupCtxCancel := context.WithCancel(context.Background())

	handler := &Handler{
		eventAggregator:  options.EventAggregator,
		sessions:         make(map[uuid.UUID]*sessionState),
		truncateAfter:    truncateAfter,
		storageCapacity:  storageCapacity,
		idleTimeout:      idleTimeout,
		cleanupCtx:       cleanupCtx,
		cleanupCtxCancel: cleanupCtxCancel,

		pathPrefix: options.PathPrefix,

		mux: setHandlerOptions(options, truncateAfter, mux),
	}

	// Start cleanup goroutine
	go handler.sessionCleanupLoop()

	mux.HandleFunc("GET /{$}", handler.root)
	mux.HandleFunc("GET /event-list", handler.getEventList)
	mux.HandleFunc("DELETE /event-list", handler.clearEventList)
	mux.HandleFunc("GET /event/{eventId}", handler.getEventDetails)
	mux.HandleFunc("GET /events-sse", handler.getEventsSSE)
	mux.HandleFunc("GET /download/request-body/{eventId}", handler.downloadRequestBody)
	mux.HandleFunc("GET /download/response-body/{eventId}", handler.downloadResponseBody)

	// Capture control endpoints
	mux.HandleFunc("POST /capture/start", handler.captureStart)
	mux.HandleFunc("POST /capture/stop", handler.captureStop)
	mux.HandleFunc("POST /capture/mode", handler.captureMode)
	mux.HandleFunc("GET /capture/status", handler.captureStatus)

	mux.Handle("/static/", http.StripPrefix("/static", http.FileServerFS(static.Assets)))

	return handler
}

func setHandlerOptions(options HandlerOptions, truncateAfter uint64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = views.WithHandlerOptions(ctx, views.HandlerOptions{
			PathPrefix:    options.PathPrefix,
			TruncateAfter: truncateAfter,
		})
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// Close shuts down the handler and releases resources
func (h *Handler) Close() {
	h.cleanupCtxCancel()

	// Unregister all storages
	h.sessionsMu.Lock()
	for sessionID, state := range h.sessions {
		if storage := h.eventAggregator.GetStorage(state.storageID); storage != nil {
			storage.Close()
		}
		h.eventAggregator.UnregisterStorage(state.storageID)
		delete(h.sessions, sessionID)
	}
	h.sessionsMu.Unlock()
}

// sessionCleanupLoop periodically checks for idle sessions and cleans them up
func (h *Handler) sessionCleanupLoop() {
	ticker := time.NewTicker(h.idleTimeout / 2)
	defer ticker.Stop()

	for {
		select {
		case <-h.cleanupCtx.Done():
			return
		case <-ticker.C:
			h.cleanupIdleSessions()
		}
	}
}

func (h *Handler) cleanupIdleSessions() {
	now := time.Now()

	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()

	for sessionID, state := range h.sessions {
		if now.Sub(state.lastActive) > h.idleTimeout {
			// Clean up this session
			if storage := h.eventAggregator.GetStorage(state.storageID); storage != nil {
				storage.Close()
			}
			h.eventAggregator.UnregisterStorage(state.storageID)
			delete(h.sessions, sessionID)
		}
	}
}

// getSessionID extracts the session ID from the request cookie
func (h *Handler) getSessionID(r *http.Request) (uuid.UUID, bool) {
	cookie, err := r.Cookie(collector.SessionCookieName)
	if err != nil {
		return uuid.Nil, false
	}
	sessionID, err := uuid.FromString(cookie.Value)
	if err != nil {
		return uuid.Nil, false
	}
	return sessionID, true
}

// getOrCreateSessionID gets existing session ID or creates a new one
func (h *Handler) getOrCreateSessionID(w http.ResponseWriter, r *http.Request) uuid.UUID {
	if sessionID, ok := h.getSessionID(r); ok {
		return sessionID
	}

	// Create new session ID
	sessionID := uuid.Must(uuid.NewV4())
	h.setSessionCookie(w, sessionID)
	return sessionID
}

// setSessionCookie sets the session cookie
func (h *Handler) setSessionCookie(w http.ResponseWriter, sessionID uuid.UUID) {
	http.SetCookie(w, &http.Cookie{
		Name:     collector.SessionCookieName,
		Value:    sessionID.String(),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie clears the session cookie
func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     collector.SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

// getSessionStorage returns the storage for a session, or nil if not found
func (h *Handler) getSessionStorage(sessionID uuid.UUID) *collector.CaptureStorage {
	h.sessionsMu.RLock()
	state, exists := h.sessions[sessionID]
	h.sessionsMu.RUnlock()

	if !exists {
		return nil
	}

	storage := h.eventAggregator.GetStorage(state.storageID)
	if storage == nil {
		return nil
	}

	return storage.(*collector.CaptureStorage)
}

// updateSessionActivity updates the last active time for a session
func (h *Handler) updateSessionActivity(sessionID uuid.UUID) {
	h.sessionsMu.Lock()
	if state, exists := h.sessions[sessionID]; exists {
		state.lastActive = time.Now()
	}
	h.sessionsMu.Unlock()
}

func (h *Handler) root(w http.ResponseWriter, r *http.Request) {
	sessionID, _ := h.getSessionID(r)
	storage := h.getSessionStorage(sessionID)

	var selectedEvent *collector.Event
	idStr := r.URL.Query().Get("id")
	if idStr != "" && storage != nil {
		eventID, err := uuid.FromString(idStr)
		if err != nil {
			http.Error(w, "Invalid event id", http.StatusBadRequest)
			return
		}
		event, exists := storage.GetEvent(eventID)
		if !exists {
			http.Redirect(w, r, fmt.Sprintf("%s/", h.pathPrefix), http.StatusTemporaryRedirect)
			return
		}
		selectedEvent = event
	}

	var recentEvents []*collector.Event
	captureActive := false
	captureMode := "session"
	if storage != nil {
		recentEvents = h.loadRecentEvents(storage)
		captureActive = true
		if storage.CaptureMode() == collector.CaptureModeGlobal {
			captureMode = "global"
		}
	}

	templ.Handler(
		views.Dashboard(views.DashboardProps{
			SelectedEvent: selectedEvent,
			Events:        recentEvents,
			CaptureActive: captureActive,
			CaptureMode:   captureMode,
		}),
	).ServeHTTP(w, r)
}

func (h *Handler) getEventList(w http.ResponseWriter, r *http.Request) {
	sessionID, _ := h.getSessionID(r)
	storage := h.getSessionStorage(sessionID)

	var recentEvents []*collector.Event
	if storage != nil {
		recentEvents = h.loadRecentEvents(storage)
	}

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
	sessionID, _ := h.getSessionID(r)
	if storage := h.getSessionStorage(sessionID); storage != nil {
		storage.Clear()
	}

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
	sessionID, _ := h.getSessionID(r)
	storage := h.getSessionStorage(sessionID)
	if storage == nil {
		http.Error(w, "No capture session active", http.StatusNotFound)
		return
	}

	idStr := r.PathValue("eventId")
	eventID, err := uuid.FromString(idStr)
	if err != nil {
		http.Error(w, "Invalid event id", http.StatusBadRequest)
		return
	}

	event, exists := storage.GetEvent(eventID)
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
	sessionID, hasSession := h.getSessionID(r)
	if !hasSession {
		http.Error(w, "No session cookie", http.StatusUnauthorized)
		return
	}

	storage := h.getSessionStorage(sessionID)
	if storage == nil {
		http.Error(w, "No capture session active", http.StatusNotFound)
		return
	}

	// Update activity for this session
	h.updateSessionActivity(sessionID)

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // For NGINX proxy

	// Create a context that gets canceled when the connection is closed
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Create a notification channel for new events from the user's storage
	eventCh := storage.Subscribe(ctx)

	// Send a keep-alive message initially to ensure the connection is established
	fmt.Fprintf(w, "event: keepalive\ndata: connected\n\n")
	w.(http.Flusher).Flush()

	// Listen for new events and send them as SSE events
	for {
		select {
		case <-ctx.Done():
			return // Client disconnected
		case event, ok := <-eventCh:
			if !ok {
				return // Channel closed
			}

			// Update activity on each event
			h.updateSessionActivity(sessionID)

			// Send as SSE event
			fmt.Fprintf(w, "event: new-event\n")
			fmt.Fprintf(w, "data: ")

			views.EventListItem(event, nil).Render(ctx, w)

			fmt.Fprintf(w, "\n\n")

			w.(http.Flusher).Flush()
		}
	}
}

func (h *Handler) loadRecentEvents(storage *collector.CaptureStorage) []*collector.Event {
	recentEvents := storage.GetEvents(h.truncateAfter)
	slices.Reverse(recentEvents)

	return recentEvents
}

// downloadRequestBody handles downloading the request body for an event
func (h *Handler) downloadRequestBody(w http.ResponseWriter, r *http.Request) {
	sessionID, _ := h.getSessionID(r)
	storage := h.getSessionStorage(sessionID)
	if storage == nil {
		http.Error(w, "No capture session active", http.StatusNotFound)
		return
	}

	idStr := r.PathValue("eventId")
	eventID, err := uuid.FromString(idStr)
	if err != nil {
		http.Error(w, "Invalid event id", http.StatusBadRequest)
		return
	}

	event, exists := storage.GetEvent(eventID)
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
	sessionID, _ := h.getSessionID(r)
	storage := h.getSessionStorage(sessionID)
	if storage == nil {
		http.Error(w, "No capture session active", http.StatusNotFound)
		return
	}

	idStr := r.PathValue("eventId")
	eventID, err := uuid.FromString(idStr)
	if err != nil {
		http.Error(w, "Invalid event id", http.StatusBadRequest)
		return
	}

	event, exists := storage.GetEvent(eventID)
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

// Capture control endpoints

// CaptureStatusResponse is the response for GET /capture/status
type CaptureStatusResponse struct {
	Active bool   `json:"active"`
	Mode   string `json:"mode,omitempty"` // "session" or "global"
}

// captureStart handles POST /capture/start - creates a new capture session
func (h *Handler) captureStart(w http.ResponseWriter, r *http.Request) {
	// Get or create session ID
	sessionID := h.getOrCreateSessionID(w, r)

	// Parse mode from request body (default to session mode)
	mode := collector.CaptureModeSession
	if r.FormValue("mode") == "global" {
		mode = collector.CaptureModeGlobal
	}

	// Check if already capturing
	h.sessionsMu.Lock()
	if state, exists := h.sessions[sessionID]; exists {
		h.sessionsMu.Unlock()
		// Already capturing, get current mode from storage
		if storage := h.eventAggregator.GetStorage(state.storageID); storage != nil {
			mode = storage.(*collector.CaptureStorage).CaptureMode()
		}
		h.respondWithCaptureState(w, r, true, mode)
		return
	}

	// Create new storage
	storage := collector.NewCaptureStorage(sessionID, h.storageCapacity, mode)

	// Register with aggregator
	h.eventAggregator.RegisterStorage(storage)

	// Track the session
	h.sessions[sessionID] = &sessionState{
		storageID:  storage.ID(),
		lastActive: time.Now(),
	}
	h.sessionsMu.Unlock()

	h.respondWithCaptureState(w, r, true, mode)
}

// captureStop handles POST /capture/stop - stops capture and removes storage
func (h *Handler) captureStop(w http.ResponseWriter, r *http.Request) {
	sessionID, hasSession := h.getSessionID(r)
	if !hasSession {
		h.respondWithCaptureState(w, r, false, collector.CaptureModeSession)
		return
	}

	h.sessionsMu.Lock()
	state, exists := h.sessions[sessionID]
	if exists {
		// Close and unregister storage
		if storage := h.eventAggregator.GetStorage(state.storageID); storage != nil {
			storage.Close()
		}
		h.eventAggregator.UnregisterStorage(state.storageID)
		delete(h.sessions, sessionID)
	}
	h.sessionsMu.Unlock()

	h.respondWithCaptureState(w, r, false, collector.CaptureModeSession)
}

// captureMode handles POST /capture/mode - changes capture mode
func (h *Handler) captureMode(w http.ResponseWriter, r *http.Request) {
	sessionID, hasSession := h.getSessionID(r)
	if !hasSession {
		http.Error(w, "No session cookie", http.StatusUnauthorized)
		return
	}

	storage := h.getSessionStorage(sessionID)
	if storage == nil {
		http.Error(w, "No capture session active", http.StatusNotFound)
		return
	}

	// Parse mode from request
	modeStr := r.FormValue("mode")
	var mode collector.CaptureMode
	switch modeStr {
	case "session":
		mode = collector.CaptureModeSession
	case "global":
		mode = collector.CaptureModeGlobal
	default:
		http.Error(w, "Invalid mode, must be 'session' or 'global'", http.StatusBadRequest)
		return
	}

	storage.SetCaptureMode(mode)

	h.respondWithCaptureState(w, r, true, mode)
}

// captureStatus handles GET /capture/status - returns current capture state
func (h *Handler) captureStatus(w http.ResponseWriter, r *http.Request) {
	sessionID, hasSession := h.getSessionID(r)
	if !hasSession {
		h.respondWithCaptureState(w, r, false, collector.CaptureModeSession)
		return
	}

	storage := h.getSessionStorage(sessionID)
	if storage == nil {
		h.respondWithCaptureState(w, r, false, collector.CaptureModeSession)
		return
	}

	h.respondWithCaptureState(w, r, true, storage.CaptureMode())
}

// respondWithCaptureState responds with capture state as HTML for HTMX or JSON for API
func (h *Handler) respondWithCaptureState(w http.ResponseWriter, r *http.Request, active bool, mode collector.CaptureMode) {
	modeStr := "session"
	if mode == collector.CaptureModeGlobal {
		modeStr = "global"
	}

	// Check if this is an HTMX request
	if r.Header.Get("HX-Request") == "true" {
		templ.Handler(
			views.CaptureControls(views.CaptureState{
				Active: active,
				Mode:   modeStr,
			}),
		).ServeHTTP(w, r)
		return
	}

	// Return JSON for API compatibility
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(CaptureStatusResponse{Active: active, Mode: modeStr})
}
