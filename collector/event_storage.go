package collector

import (
	"context"

	"github.com/gofrs/uuid"
)

// EventStorage is the interface for event storage backends.
// Storages are responsible for deciding which events to capture and storing them.
type EventStorage interface {
	// ID returns the unique identifier for this storage
	ID() uuid.UUID

	// ShouldCapture returns true if this storage wants to capture events for the given context
	ShouldCapture(ctx context.Context) bool

	// Add adds an event to the storage
	Add(event *Event)

	// GetEvent retrieves an event by its ID
	GetEvent(id uuid.UUID) (*Event, bool)

	// GetEvents returns the most recent n events
	GetEvents(limit uint64) []*Event

	// Subscribe returns a channel that receives notifications of new events
	Subscribe(ctx context.Context) <-chan *Event

	// Clear removes all events from the storage
	Clear()

	// Close releases resources used by the storage
	Close()
}

// CaptureMode defines how a CaptureStorage decides which events to capture
type CaptureMode int

const (
	// CaptureModeSession captures only events from requests with matching session ID
	CaptureModeSession CaptureMode = iota
	// CaptureModeGlobal captures all events
	CaptureModeGlobal
)

// CaptureStorage implements EventStorage with configurable capture mode.
// Each user gets their own CaptureStorage instance.
type CaptureStorage struct {
	id          uuid.UUID
	sessionID   uuid.UUID
	captureMode CaptureMode

	buffer   *LookupRingBuffer[*Event, uuid.UUID]
	notifier *Notifier[*Event]
}

// NewCaptureStorage creates a new CaptureStorage for the given session ID.
func NewCaptureStorage(sessionID uuid.UUID, capacity uint64, mode CaptureMode) *CaptureStorage {
	return &CaptureStorage{
		id:          uuid.Must(uuid.NewV7()),
		sessionID:   sessionID,
		captureMode: mode,
		buffer:      NewLookupRingBuffer[*Event, uuid.UUID](capacity),
		notifier:    NewNotifier[*Event](),
	}
}

// ID returns the unique identifier for this storage
func (s *CaptureStorage) ID() uuid.UUID {
	return s.id
}

// SessionID returns the session ID this storage belongs to
func (s *CaptureStorage) SessionID() uuid.UUID {
	return s.sessionID
}

// CaptureMode returns the current capture mode
func (s *CaptureStorage) CaptureMode() CaptureMode {
	return s.captureMode
}

// SetCaptureMode sets the capture mode
func (s *CaptureStorage) SetCaptureMode(mode CaptureMode) {
	s.captureMode = mode
}

// ShouldCapture returns true if this storage wants to capture events for the given context
func (s *CaptureStorage) ShouldCapture(ctx context.Context) bool {
	switch s.captureMode {
	case CaptureModeGlobal:
		return true
	case CaptureModeSession:
		ctxSessionID, ok := SessionIDFromContext(ctx)
		if !ok {
			return false
		}
		return ctxSessionID == s.sessionID
	default:
		return false
	}
}

// Add adds an event to the storage and notifies subscribers
func (s *CaptureStorage) Add(event *Event) {
	s.buffer.Add(event)
	s.notifier.Notify(event)
}

// GetEvent retrieves an event by its ID
func (s *CaptureStorage) GetEvent(id uuid.UUID) (*Event, bool) {
	return s.buffer.Lookup(id)
}

// GetEvents returns the most recent n events
func (s *CaptureStorage) GetEvents(limit uint64) []*Event {
	return s.buffer.GetRecords(limit)
}

// Subscribe returns a channel that receives notifications of new events
func (s *CaptureStorage) Subscribe(ctx context.Context) <-chan *Event {
	return s.notifier.Subscribe(ctx)
}

// Clear removes all events from the storage
func (s *CaptureStorage) Clear() {
	s.buffer.Clear()
}

// Close releases resources used by the storage
func (s *CaptureStorage) Close() {
	s.notifier.Close()
}

// Ensure CaptureStorage implements EventStorage
var _ EventStorage = (*CaptureStorage)(nil)
