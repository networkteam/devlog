package dashboard

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gofrs/uuid"

	"github.com/networkteam/devlog/collector"
)

// sessionState tracks a user's capture session
type sessionState struct {
	storageID  uuid.UUID
	lastActive time.Time
}

// SessionManager manages capture sessions and their associated storages.
// It handles session lifecycle, activity tracking, and cleanup.
type SessionManager struct {
	eventAggregator *collector.EventAggregator

	sessions   map[uuid.UUID]*sessionState
	sessionsMu sync.RWMutex

	storageCapacity uint64
	idleTimeout     time.Duration

	cleanupCtx       context.Context
	cleanupCtxCancel context.CancelFunc
}

// SessionManagerOptions configures a SessionManager
type SessionManagerOptions struct {
	EventAggregator *collector.EventAggregator
	StorageCapacity uint64
	IdleTimeout     time.Duration
}

// NewSessionManager creates a new SessionManager and starts the cleanup goroutine
func NewSessionManager(opts SessionManagerOptions) *SessionManager {
	storageCapacity := opts.StorageCapacity
	if storageCapacity == 0 {
		storageCapacity = DefaultStorageCapacity
	}

	idleTimeout := opts.IdleTimeout
	if idleTimeout == 0 {
		idleTimeout = DefaultSessionIdleTimeout
	}

	cleanupCtx, cleanupCtxCancel := context.WithCancel(context.Background())

	sm := &SessionManager{
		eventAggregator:  opts.EventAggregator,
		sessions:         make(map[uuid.UUID]*sessionState),
		storageCapacity:  storageCapacity,
		idleTimeout:      idleTimeout,
		cleanupCtx:       cleanupCtx,
		cleanupCtxCancel: cleanupCtxCancel,
	}

	go sm.cleanupLoop()

	return sm
}

// Get returns the storage for a session, or nil if not found
func (sm *SessionManager) Get(sessionID uuid.UUID) *collector.CaptureStorage {
	sm.sessionsMu.RLock()
	state, exists := sm.sessions[sessionID]
	sm.sessionsMu.RUnlock()

	if !exists {
		return nil
	}

	storage := sm.eventAggregator.GetStorage(state.storageID)
	if storage == nil {
		return nil
	}

	return storage.(*collector.CaptureStorage)
}

// GetOrCreate returns the storage for a session, creating it if it doesn't exist.
// Returns the storage and whether it was newly created.
func (sm *SessionManager) GetOrCreate(sessionID uuid.UUID, mode collector.CaptureMode) (*collector.CaptureStorage, bool) {
	sm.sessionsMu.Lock()
	defer sm.sessionsMu.Unlock()

	// Check if already exists
	if state, exists := sm.sessions[sessionID]; exists {
		if storage := sm.eventAggregator.GetStorage(state.storageID); storage != nil {
			return storage.(*collector.CaptureStorage), false
		}
		// Storage was removed but session state remains - clean it up
		delete(sm.sessions, sessionID)
	}

	// Create new storage
	storage := collector.NewCaptureStorage(sessionID, sm.storageCapacity, mode)
	sm.eventAggregator.RegisterStorage(storage)

	sm.sessions[sessionID] = &sessionState{
		storageID:  storage.ID(),
		lastActive: time.Now(),
	}

	return storage, true
}

// Delete removes a session and its storage
func (sm *SessionManager) Delete(sessionID uuid.UUID) {
	sm.sessionsMu.Lock()
	defer sm.sessionsMu.Unlock()

	state, exists := sm.sessions[sessionID]
	if !exists {
		return
	}

	if storage := sm.eventAggregator.GetStorage(state.storageID); storage != nil {
		storage.Close()
	}
	sm.eventAggregator.UnregisterStorage(state.storageID)
	delete(sm.sessions, sessionID)
}

// UpdateActivity updates the last active time for a session
func (sm *SessionManager) UpdateActivity(sessionID uuid.UUID) {
	sm.sessionsMu.Lock()
	if state, exists := sm.sessions[sessionID]; exists {
		state.lastActive = time.Now()
	}
	sm.sessionsMu.Unlock()
}

// IdleTimeout returns the configured idle timeout duration
func (sm *SessionManager) IdleTimeout() time.Duration {
	return sm.idleTimeout
}

// Close shuts down the session manager and cleans up all sessions
func (sm *SessionManager) Close() {
	sm.cleanupCtxCancel()

	sm.sessionsMu.Lock()
	defer sm.sessionsMu.Unlock()

	for sessionID, state := range sm.sessions {
		if storage := sm.eventAggregator.GetStorage(state.storageID); storage != nil {
			storage.Close()
		}
		sm.eventAggregator.UnregisterStorage(state.storageID)
		delete(sm.sessions, sessionID)
	}
}

// cleanupLoop periodically checks for idle sessions and cleans them up
func (sm *SessionManager) cleanupLoop() {
	ticker := time.NewTicker(sm.idleTimeout / 2)
	defer ticker.Stop()

	for {
		select {
		case <-sm.cleanupCtx.Done():
			return
		case <-ticker.C:
			sm.cleanupIdleSessions()
		}
	}
}

func (sm *SessionManager) cleanupIdleSessions() {
	now := time.Now()

	sm.sessionsMu.Lock()
	defer sm.sessionsMu.Unlock()

	for sessionID, state := range sm.sessions {
		if now.Sub(state.lastActive) > sm.idleTimeout {
			fmt.Fprintf(os.Stderr, "[DEBUG] %s: cleanupIdleSessions: cleaning up session %s, idle for %v\n", time.Now().Format(time.DateTime), sessionID, now.Sub(state.lastActive))
			if storage := sm.eventAggregator.GetStorage(state.storageID); storage != nil {
				storage.Close()
			}
			sm.eventAggregator.UnregisterStorage(state.storageID)
			delete(sm.sessions, sessionID)
		}
	}
}
