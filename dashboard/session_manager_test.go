package dashboard

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"

	"github.com/networkteam/devlog/collector"
)

func TestSessionManager_Get_NonExistent(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	sm := NewSessionManager(SessionManagerOptions{
		EventAggregator: aggregator,
		StorageCapacity: 100,
		IdleTimeout:     time.Minute,
	})
	defer sm.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := sm.Get(sessionID)

	if storage != nil {
		t.Errorf("expected nil storage for non-existent session, got %v", storage)
	}
}

func TestSessionManager_GetOrCreate_CreatesNew(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	sm := NewSessionManager(SessionManagerOptions{
		EventAggregator: aggregator,
		StorageCapacity: 100,
		IdleTimeout:     time.Minute,
	})
	defer sm.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage, created, err := sm.GetOrCreate(sessionID, collector.CaptureModeSession)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Error("expected created to be true for new session")
	}
	if storage == nil {
		t.Fatal("expected non-nil storage")
	}
	if storage.SessionID() != sessionID {
		t.Errorf("expected session ID %s, got %s", sessionID, storage.SessionID())
	}
	if storage.CaptureMode() != collector.CaptureModeSession {
		t.Errorf("expected session mode, got %v", storage.CaptureMode())
	}
}

func TestSessionManager_GetOrCreate_ReturnsExisting(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	sm := NewSessionManager(SessionManagerOptions{
		EventAggregator: aggregator,
		StorageCapacity: 100,
		IdleTimeout:     time.Minute,
	})
	defer sm.Close()

	sessionID := uuid.Must(uuid.NewV4())

	// Create first
	storage1, created1, err := sm.GetOrCreate(sessionID, collector.CaptureModeSession)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created1 {
		t.Error("expected created to be true for first call")
	}

	// Get existing
	storage2, created2, err := sm.GetOrCreate(sessionID, collector.CaptureModeGlobal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created2 {
		t.Error("expected created to be false for second call")
	}

	if storage1.ID() != storage2.ID() {
		t.Error("expected same storage instance")
	}

	// Mode should not change on GetOrCreate of existing
	if storage2.CaptureMode() != collector.CaptureModeSession {
		t.Errorf("expected original mode to be preserved, got %v", storage2.CaptureMode())
	}
}

func TestSessionManager_Get_AfterCreate(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	sm := NewSessionManager(SessionManagerOptions{
		EventAggregator: aggregator,
		StorageCapacity: 100,
		IdleTimeout:     time.Minute,
	})
	defer sm.Close()

	sessionID := uuid.Must(uuid.NewV4())

	// Create
	storage1, _, _ := sm.GetOrCreate(sessionID, collector.CaptureModeGlobal)

	// Get
	storage2 := sm.Get(sessionID)

	if storage2 == nil {
		t.Fatal("expected non-nil storage after create")
	}
	if storage1.ID() != storage2.ID() {
		t.Error("expected same storage instance")
	}
}

func TestSessionManager_Delete(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	sm := NewSessionManager(SessionManagerOptions{
		EventAggregator: aggregator,
		StorageCapacity: 100,
		IdleTimeout:     time.Minute,
	})
	defer sm.Close()

	sessionID := uuid.Must(uuid.NewV4())

	// Create
	_, _, _ = sm.GetOrCreate(sessionID, collector.CaptureModeSession)

	// Delete
	sm.Delete(sessionID)

	// Should be gone
	storage := sm.Get(sessionID)
	if storage != nil {
		t.Error("expected nil storage after delete")
	}

	// Storage should be unregistered from aggregator
	if aggregator.GetStorage(sessionID) != nil {
		t.Error("expected storage to be unregistered from aggregator")
	}
}

func TestSessionManager_Delete_NonExistent(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	sm := NewSessionManager(SessionManagerOptions{
		EventAggregator: aggregator,
		StorageCapacity: 100,
		IdleTimeout:     time.Minute,
	})
	defer sm.Close()

	sessionID := uuid.Must(uuid.NewV4())

	// Should not panic
	sm.Delete(sessionID)
}

func TestSessionManager_UpdateActivity(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	sm := NewSessionManager(SessionManagerOptions{
		EventAggregator: aggregator,
		StorageCapacity: 100,
		IdleTimeout:     time.Minute,
	})
	defer sm.Close()

	sessionID := uuid.Must(uuid.NewV4())

	// Create session
	_, _, _ = sm.GetOrCreate(sessionID, collector.CaptureModeSession)

	// Record time before update
	sm.sessionsMu.RLock()
	timeBefore := sm.sessions[sessionID].lastActive
	sm.sessionsMu.RUnlock()

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Update activity
	sm.UpdateActivity(sessionID)

	// Check time was updated
	sm.sessionsMu.RLock()
	timeAfter := sm.sessions[sessionID].lastActive
	sm.sessionsMu.RUnlock()

	if !timeAfter.After(timeBefore) {
		t.Error("expected lastActive to be updated")
	}
}

func TestSessionManager_UpdateActivity_NonExistent(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	sm := NewSessionManager(SessionManagerOptions{
		EventAggregator: aggregator,
		StorageCapacity: 100,
		IdleTimeout:     time.Minute,
	})
	defer sm.Close()

	sessionID := uuid.Must(uuid.NewV4())

	// Should not panic
	sm.UpdateActivity(sessionID)
}

func TestSessionManager_Close(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	sm := NewSessionManager(SessionManagerOptions{
		EventAggregator: aggregator,
		StorageCapacity: 100,
		IdleTimeout:     time.Minute,
	})

	sessionID1 := uuid.Must(uuid.NewV4())
	sessionID2 := uuid.Must(uuid.NewV4())

	_, _, _ = sm.GetOrCreate(sessionID1, collector.CaptureModeSession)
	_, _, _ = sm.GetOrCreate(sessionID2, collector.CaptureModeGlobal)

	sm.Close()

	// All sessions should be gone
	if sm.Get(sessionID1) != nil {
		t.Error("expected session 1 to be cleaned up")
	}
	if sm.Get(sessionID2) != nil {
		t.Error("expected session 2 to be cleaned up")
	}
}

func TestSessionManager_IdleCleanup(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	sm := NewSessionManager(SessionManagerOptions{
		EventAggregator: aggregator,
		StorageCapacity: 100,
		IdleTimeout:     50 * time.Millisecond,
	})
	defer sm.Close()

	sessionID := uuid.Must(uuid.NewV4())

	// Create session
	_, _, _ = sm.GetOrCreate(sessionID, collector.CaptureModeSession)

	// Session should exist
	if sm.Get(sessionID) == nil {
		t.Fatal("expected session to exist")
	}

	// Wait for idle timeout + cleanup interval
	time.Sleep(100 * time.Millisecond)

	// Session should be cleaned up
	if sm.Get(sessionID) != nil {
		t.Error("expected session to be cleaned up after idle timeout")
	}
}

func TestSessionManager_IdleTimeout(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	idleTimeout := 100 * time.Millisecond
	sm := NewSessionManager(SessionManagerOptions{
		EventAggregator: aggregator,
		StorageCapacity: 100,
		IdleTimeout:     idleTimeout,
	})
	defer sm.Close()

	if sm.IdleTimeout() != idleTimeout {
		t.Errorf("expected idle timeout %v, got %v", idleTimeout, sm.IdleTimeout())
	}
}

func TestSessionManager_DefaultValues(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	sm := NewSessionManager(SessionManagerOptions{
		EventAggregator: aggregator,
		// Leave StorageCapacity and IdleTimeout at 0 to test defaults
	})
	defer sm.Close()

	if sm.storageCapacity != DefaultStorageCapacity {
		t.Errorf("expected default storage capacity %d, got %d", DefaultStorageCapacity, sm.storageCapacity)
	}
	if sm.idleTimeout != DefaultSessionIdleTimeout {
		t.Errorf("expected default idle timeout %v, got %v", DefaultSessionIdleTimeout, sm.idleTimeout)
	}
}

func TestSessionManager_MultipleSessions(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	sm := NewSessionManager(SessionManagerOptions{
		EventAggregator: aggregator,
		StorageCapacity: 100,
		IdleTimeout:     time.Minute,
	})
	defer sm.Close()

	// Create multiple sessions
	sessions := make([]uuid.UUID, 5)
	for i := range sessions {
		sessions[i] = uuid.Must(uuid.NewV4())
		_, _, _ = sm.GetOrCreate(sessions[i], collector.CaptureModeSession)
	}

	// All should exist
	for i, sessionID := range sessions {
		storage := sm.Get(sessionID)
		if storage == nil {
			t.Errorf("expected session %d to exist", i)
		}
	}

	// Delete one
	sm.Delete(sessions[2])

	// Check counts
	existing := 0
	for _, sessionID := range sessions {
		if sm.Get(sessionID) != nil {
			existing++
		}
	}
	if existing != 4 {
		t.Errorf("expected 4 sessions, got %d", existing)
	}
}
