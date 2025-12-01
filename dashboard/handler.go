package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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

		mux: mux,
	}

	// Start cleanup goroutine
	go handler.sessionCleanupLoop()

	// Static assets (no session required)
	mux.Handle("GET /static/", http.StripPrefix("/static", http.FileServerFS(static.Assets)))

	// Root redirect - creates new session and redirects
	mux.HandleFunc("GET /{$}", handler.rootRedirect)

	// Session-scoped routes under /s/{sid}/ (the /s/ prefix avoids conflicts with /static/)
	mux.HandleFunc("GET /s/{sid}/{$}", handler.root)
	mux.HandleFunc("GET /s/{sid}/event-list", handler.getEventList)
	mux.HandleFunc("DELETE /s/{sid}/event-list", handler.clearEventList)
	mux.HandleFunc("GET /s/{sid}/event/{eventId}", handler.getEventDetails)
	mux.HandleFunc("GET /s/{sid}/events-sse", handler.getEventsSSE)
	mux.HandleFunc("GET /s/{sid}/download/request-body/{eventId}", handler.downloadRequestBody)
	mux.HandleFunc("GET /s/{sid}/download/response-body/{eventId}", handler.downloadResponseBody)

	// Capture control endpoints
	mux.HandleFunc("POST /s/{sid}/capture/start", handler.captureStart)
	mux.HandleFunc("POST /s/{sid}/capture/stop", handler.captureStop)
	mux.HandleFunc("POST /s/{sid}/capture/mode", handler.captureMode)
	mux.HandleFunc("GET /s/{sid}/capture/status", handler.captureStatus)
	mux.HandleFunc("POST /s/{sid}/capture/cleanup", handler.captureCleanup)

	return handler
}

// withHandlerOptions is a helper to set HandlerOptions in context before rendering
func (h *Handler) withHandlerOptions(r *http.Request, sessionID string, captureActive bool, captureMode string) *http.Request {
	ctx := views.WithHandlerOptions(r.Context(), views.HandlerOptions{
		PathPrefix:    h.pathPrefix,
		TruncateAfter: h.truncateAfter,
		SessionID:     sessionID,
		CaptureActive: captureActive,
		CaptureMode:   captureMode,
	})
	return r.WithContext(ctx)
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
			fmt.Fprintf(os.Stderr, "[DEBUG] %s: cleanupIdleSessions: cleaning up session %s, idle for %v\n", time.Now().Format(time.DateTime), sessionID, now.Sub(state.lastActive))
			// Clean up this session
			if storage := h.eventAggregator.GetStorage(state.storageID); storage != nil {
				storage.Close()
			}
			h.eventAggregator.UnregisterStorage(state.storageID)
			delete(h.sessions, sessionID)
		}
	}
}

// getSessionID extracts the session ID from the URL path parameter
func (h *Handler) getSessionID(r *http.Request) (uuid.UUID, bool) {
	sidStr := r.PathValue("sid")
	if sidStr == "" {
		return uuid.Nil, false
	}
	sessionID, err := uuid.FromString(sidStr)
	if err != nil {
		return uuid.Nil, false
	}
	return sessionID, true
}

// setSessionCookie sets the session cookie for event filtering
func (h *Handler) setSessionCookie(w http.ResponseWriter, sessionID uuid.UUID) {
	http.SetCookie(w, &http.Cookie{
		Name:     collector.SessionCookiePrefix + sessionID.String(),
		Value:    "1",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie clears the session cookie
func (h *Handler) clearSessionCookie(w http.ResponseWriter, sessionID uuid.UUID) {
	http.SetCookie(w, &http.Cookie{
		Name:     collector.SessionCookiePrefix + sessionID.String(),
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

// createSession creates a new capture session and returns the storage
func (h *Handler) createSession(sessionID uuid.UUID, mode collector.CaptureMode) *collector.CaptureStorage {
	storage := collector.NewCaptureStorage(sessionID, h.storageCapacity, mode)
	h.eventAggregator.RegisterStorage(storage)

	h.sessionsMu.Lock()
	h.sessions[sessionID] = &sessionState{
		storageID:  storage.ID(),
		lastActive: time.Now(),
	}
	h.sessionsMu.Unlock()

	return storage
}

// rootRedirect redirects to a new session
func (h *Handler) rootRedirect(w http.ResponseWriter, r *http.Request) {
	sessionID := uuid.Must(uuid.NewV4())
	http.Redirect(w, r, fmt.Sprintf("%s/s/%s/", h.pathPrefix, sessionID), http.StatusTemporaryRedirect)
}

func (h *Handler) root(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := h.getSessionID(r)
	if !ok {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	storage := h.getSessionStorage(sessionID)

	// Check URL params for capture state - allows recreating session from URL
	captureParam := r.URL.Query().Get("capture")
	modeParam := r.URL.Query().Get("mode")
	if modeParam == "" {
		modeParam = "session" // default
	}

	// Recreate session from URL params if needed
	if storage == nil && captureParam == "true" {
		mode := collector.CaptureModeSession
		if modeParam == "global" {
			mode = collector.CaptureModeGlobal
		}
		storage = h.createSession(sessionID, mode)
		if mode == collector.CaptureModeSession {
			h.setSessionCookie(w, sessionID)
		}
	}

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
			http.Redirect(w, r, fmt.Sprintf("%s/s/%s/", h.pathPrefix, sessionID), http.StatusTemporaryRedirect)
			return
		}
		selectedEvent = event
	}

	var recentEvents []*collector.Event
	captureActive := false
	captureMode := modeParam
	if storage != nil {
		h.updateSessionActivity(sessionID)
		recentEvents = h.loadRecentEvents(storage)
		captureActive = true
		if storage.CaptureMode() == collector.CaptureModeGlobal {
			captureMode = "global"
		} else {
			captureMode = "session"
			// Re-set session cookie for session mode (cleared on beforeunload)
			h.setSessionCookie(w, sessionID)
		}
	}

	r = h.withHandlerOptions(r, sessionID.String(), captureActive, captureMode)
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
	captureActive := false
	captureMode := "session"
	if storage != nil {
		recentEvents = h.loadRecentEvents(storage)
		captureActive = true
		if storage.CaptureMode() == collector.CaptureModeGlobal {
			captureMode = "global"
		}
	}

	r = h.withHandlerOptions(r, sessionID.String(), captureActive, captureMode)

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
			CaptureActive:   captureActive,
			CaptureMode:     captureMode,
		}),
	).ServeHTTP(w, r)
}

func (h *Handler) clearEventList(w http.ResponseWriter, r *http.Request) {
	sessionID, _ := h.getSessionID(r)
	storage := h.getSessionStorage(sessionID)
	if storage != nil {
		storage.Clear()
	}

	// Keep capture active if storage exists
	captureActive := storage != nil
	captureMode := "session"
	if storage != nil && storage.CaptureMode() == collector.CaptureModeGlobal {
		captureMode = "global"
	}

	r = h.withHandlerOptions(r, sessionID.String(), captureActive, captureMode)
	opts := views.HandlerOptions{
		PathPrefix:    h.pathPrefix,
		SessionID:     sessionID.String(),
		CaptureActive: captureActive,
		CaptureMode:   captureMode,
	}

	// Update URL to remove id parameter but preserve capture state
	w.Header().Set("HX-Push-Url", opts.BuildEventDetailURL(""))

	templ.Handler(
		views.SplitLayout(views.EventList(views.EventListProps{CaptureActive: captureActive, CaptureMode: captureMode}), views.EventDetailContainer(nil)),
	).ServeHTTP(w, r)
}

func (h *Handler) getEventDetails(w http.ResponseWriter, r *http.Request) {
	sessionID, _ := h.getSessionID(r)
	storage := h.getSessionStorage(sessionID)
	if storage == nil {
		http.Error(w, "No capture session active", http.StatusNotFound)
		return
	}

	captureActive := true
	captureMode := "session"
	if storage.CaptureMode() == collector.CaptureModeGlobal {
		captureMode = "global"
	}
	r = h.withHandlerOptions(r, sessionID.String(), captureActive, captureMode)

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
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	storage := h.getSessionStorage(sessionID)
	if storage == nil {
		// Session was cleaned up - recreate it (fresh and empty)
		// Use mode from query param, default to session mode
		mode := collector.CaptureModeSession
		if r.URL.Query().Get("mode") == "global" {
			mode = collector.CaptureModeGlobal
		}

		storage = h.createSession(sessionID, mode)

		// Set cookie if session mode
		if mode == collector.CaptureModeSession {
			h.setSessionCookie(w, sessionID)
		}
	}

	// Set handler options in context for template rendering
	captureMode := "session"
	if storage.CaptureMode() == collector.CaptureModeGlobal {
		captureMode = "global"
	}
	r = h.withHandlerOptions(r, sessionID.String(), true, captureMode)

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

	// Create a ticker to keep the session alive and send keepalive messages
	// This prevents idle timeout while SSE connection is open
	keepaliveTicker := time.NewTicker(h.idleTimeout / 2)
	defer keepaliveTicker.Stop()

	// Listen for new events and send them as SSE events
	for {
		select {
		case <-ctx.Done():
			return // Client disconnected
		case <-keepaliveTicker.C:
			// Keep session alive while SSE is connected
			h.updateSessionActivity(sessionID)
			// Send keepalive to client
			fmt.Fprintf(w, "event: keepalive\ndata: ping\n\n")
			w.(http.Flusher).Flush()
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

// captureStart handles POST /capture/start - creates or resumes a capture session
func (h *Handler) captureStart(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := h.getSessionID(r)
	if !ok {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	// Parse mode from request body (default to session mode)
	mode := collector.CaptureModeSession
	if r.FormValue("mode") == "global" {
		mode = collector.CaptureModeGlobal
	}

	// Check if session already exists (may be paused or active)
	h.sessionsMu.Lock()
	if state, exists := h.sessions[sessionID]; exists {
		h.sessionsMu.Unlock()
		// Session exists, resume capturing with potentially new mode
		if storage := h.eventAggregator.GetStorage(state.storageID); storage != nil {
			captureStorage := storage.(*collector.CaptureStorage)
			oldMode := captureStorage.CaptureMode()
			captureStorage.SetCapturing(true)
			captureStorage.SetCaptureMode(mode)

			// Handle cookie based on mode change
			if mode == collector.CaptureModeSession && oldMode != collector.CaptureModeSession {
				h.setSessionCookie(w, sessionID)
			} else if mode == collector.CaptureModeGlobal && oldMode == collector.CaptureModeSession {
				h.clearSessionCookie(w, sessionID)
			}
		}
		h.respondWithCaptureState(w, r, sessionID, true, mode)
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

	// Set session cookie for event filtering (if session mode)
	if mode == collector.CaptureModeSession {
		h.setSessionCookie(w, sessionID)
	}

	h.respondWithCaptureState(w, r, sessionID, true, mode)
}

// captureStop handles POST /capture/stop - pauses capture but keeps session and events
func (h *Handler) captureStop(w http.ResponseWriter, r *http.Request) {
	sessionID, hasSession := h.getSessionID(r)
	if !hasSession {
		h.respondWithCaptureState(w, r, sessionID, false, collector.CaptureModeSession)
		return
	}

	storage := h.getSessionStorage(sessionID)
	if storage == nil {
		h.respondWithCaptureState(w, r, sessionID, false, collector.CaptureModeSession)
		return
	}

	// Pause capturing - keep storage, session, and events intact
	storage.SetCapturing(false)

	// Keep session cookie so user can resume
	// Respond with active=false but preserve the mode
	h.respondWithCaptureState(w, r, sessionID, false, storage.CaptureMode())
}

// captureMode handles POST /capture/mode - changes capture mode
func (h *Handler) captureMode(w http.ResponseWriter, r *http.Request) {
	sessionID, hasSession := h.getSessionID(r)
	if !hasSession {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
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

	oldMode := storage.CaptureMode()
	storage.SetCaptureMode(mode)

	// Handle cookie based on mode change
	if mode == collector.CaptureModeSession && oldMode != collector.CaptureModeSession {
		// Switching to session mode: set cookie
		h.setSessionCookie(w, sessionID)
	} else if mode == collector.CaptureModeGlobal && oldMode == collector.CaptureModeSession {
		// Switching from session to global: clear cookie
		h.clearSessionCookie(w, sessionID)
	}

	h.respondWithCaptureState(w, r, sessionID, true, mode)
}

// captureStatus handles GET /capture/status - returns current capture state
func (h *Handler) captureStatus(w http.ResponseWriter, r *http.Request) {
	sessionID, hasSession := h.getSessionID(r)
	if !hasSession {
		h.respondWithCaptureState(w, r, sessionID, false, collector.CaptureModeSession)
		return
	}

	storage := h.getSessionStorage(sessionID)
	if storage == nil {
		h.respondWithCaptureState(w, r, sessionID, false, collector.CaptureModeSession)
		return
	}

	h.respondWithCaptureState(w, r, sessionID, storage.IsCapturing(), storage.CaptureMode())
}

// respondWithCaptureState responds with capture state as HTML for HTMX or JSON for API
func (h *Handler) respondWithCaptureState(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID, active bool, mode collector.CaptureMode) {
	modeStr := "session"
	if mode == collector.CaptureModeGlobal {
		modeStr = "global"
	}

	// Check if this is an HTMX request
	if r.Header.Get("HX-Request") == "true" {
		r = h.withHandlerOptions(r, sessionID.String(), active, modeStr)
		opts := views.HandlerOptions{
			PathPrefix:    h.pathPrefix,
			SessionID:     sessionID.String(),
			CaptureActive: active,
			CaptureMode:   modeStr,
		}

		// Trigger event list refresh via HTMX response header
		w.Header().Set("HX-Trigger", "capture-state-changed")

		// Update browser URL to reflect capture state
		w.Header().Set("HX-Push-Url", opts.BuildEventDetailURL(""))

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

// captureCleanup handles POST /capture/cleanup - called via sendBeacon on tab close/reload
func (h *Handler) captureCleanup(w http.ResponseWriter, r *http.Request) {
	sessionID, ok := h.getSessionID(r)
	if !ok {
		// Silent success for beacon - no session to clean up
		w.WriteHeader(http.StatusOK)
		return
	}

	// Only clear session cookie - don't delete storage
	// Storage will be cleaned up by idle timeout or explicit stop
	// Cookie will be re-set on page load if session is still active
	h.clearSessionCookie(w, sessionID)

	w.WriteHeader(http.StatusOK)
}
