package collector_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/networkteam/devlog/collector"
)

func TestCaptureStorage_ShouldCapture_SessionMode_NoSessionInCtx(t *testing.T) {
	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeSession)
	defer storage.Close()

	ctx := context.Background() // no session ID

	assert.False(t, storage.ShouldCapture(ctx))
}

func TestCaptureStorage_ShouldCapture_SessionMode_WrongSession(t *testing.T) {
	sessionID := uuid.Must(uuid.NewV4())
	otherSessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeSession)
	defer storage.Close()

	ctx := collector.WithSessionIDs(context.Background(), []uuid.UUID{otherSessionID})

	assert.False(t, storage.ShouldCapture(ctx))
}

func TestCaptureStorage_ShouldCapture_SessionMode_MatchingSession(t *testing.T) {
	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeSession)
	defer storage.Close()

	ctx := collector.WithSessionIDs(context.Background(), []uuid.UUID{sessionID})

	assert.True(t, storage.ShouldCapture(ctx))
}

func TestCaptureStorage_ShouldCapture_SessionMode_MultipleSessionsInContext(t *testing.T) {
	sessionID := uuid.Must(uuid.NewV4())
	otherSessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeSession)
	defer storage.Close()

	// Context has multiple session IDs, including the one we're looking for
	ctx := collector.WithSessionIDs(context.Background(), []uuid.UUID{otherSessionID, sessionID})

	assert.True(t, storage.ShouldCapture(ctx))
}

func TestCaptureStorage_ShouldCapture_GlobalMode_AlwaysTrue(t *testing.T) {
	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	defer storage.Close()

	// Should return true for any context
	assert.True(t, storage.ShouldCapture(context.Background()))

	// Even with a different session ID
	otherSessionID := uuid.Must(uuid.NewV4())
	ctx := collector.WithSessionIDs(context.Background(), []uuid.UUID{otherSessionID})
	assert.True(t, storage.ShouldCapture(ctx))
}

func TestCaptureStorage_SetCaptureMode(t *testing.T) {
	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeSession)
	defer storage.Close()

	assert.Equal(t, collector.CaptureModeSession, storage.CaptureMode())

	storage.SetCaptureMode(collector.CaptureModeGlobal)
	assert.Equal(t, collector.CaptureModeGlobal, storage.CaptureMode())

	storage.SetCaptureMode(collector.CaptureModeSession)
	assert.Equal(t, collector.CaptureModeSession, storage.CaptureMode())
}

func TestCaptureStorage_Add_StoresEvent(t *testing.T) {
	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	defer storage.Close()

	event := &collector.Event{
		ID:    uuid.Must(uuid.NewV7()),
		Data:  "test data",
		Start: time.Now(),
		End:   time.Now(),
	}

	storage.Add(event)

	events := storage.GetEvents(10)
	require.Len(t, events, 1)
	assert.Equal(t, event.ID, events[0].ID)
	assert.Equal(t, "test data", events[0].Data)
}

func TestCaptureStorage_RingBuffer_Capacity(t *testing.T) {
	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 5, collector.CaptureModeGlobal)
	defer storage.Close()

	// Add 10 events
	for i := 0; i < 10; i++ {
		event := &collector.Event{
			ID:    uuid.Must(uuid.NewV7()),
			Data:  i,
			Start: time.Now(),
			End:   time.Now(),
		}
		storage.Add(event)
	}

	// Should only keep last 5
	events := storage.GetEvents(20)
	require.Len(t, events, 5)

	// Verify we have the last 5 events (5-9)
	for i, evt := range events {
		assert.Equal(t, 5+i, evt.Data)
	}
}

func TestCaptureStorage_Subscribe_ReceivesEvents(t *testing.T) {
	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	defer storage.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	subscription := storage.Subscribe(ctx)

	// Collect events in goroutine
	var receivedEvents []*collector.Event
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for evt := range subscription {
			receivedEvents = append(receivedEvents, evt)
			if len(receivedEvents) >= 2 {
				cancel()
				return
			}
		}
	}()

	// Add events
	event1 := &collector.Event{
		ID:    uuid.Must(uuid.NewV7()),
		Data:  "event1",
		Start: time.Now(),
		End:   time.Now(),
	}
	event2 := &collector.Event{
		ID:    uuid.Must(uuid.NewV7()),
		Data:  "event2",
		Start: time.Now(),
		End:   time.Now(),
	}

	storage.Add(event1)
	storage.Add(event2)

	wg.Wait()

	require.Len(t, receivedEvents, 2)
	assert.Equal(t, "event1", receivedEvents[0].Data)
	assert.Equal(t, "event2", receivedEvents[1].Data)
}

func TestCaptureStorage_GetEvent_ByID(t *testing.T) {
	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	defer storage.Close()

	eventID := uuid.Must(uuid.NewV7())
	event := &collector.Event{
		ID:    eventID,
		Data:  "test data",
		Start: time.Now(),
		End:   time.Now(),
	}

	storage.Add(event)

	// Should find the event
	found, exists := storage.GetEvent(eventID)
	assert.True(t, exists)
	assert.Equal(t, eventID, found.ID)
	assert.Equal(t, "test data", found.Data)

	// Should not find non-existent event
	_, exists = storage.GetEvent(uuid.Must(uuid.NewV7()))
	assert.False(t, exists)
}

func TestCaptureStorage_Clear(t *testing.T) {
	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	defer storage.Close()

	event := &collector.Event{
		ID:    uuid.Must(uuid.NewV7()),
		Data:  "test data",
		Start: time.Now(),
		End:   time.Now(),
	}
	storage.Add(event)

	require.Len(t, storage.GetEvents(10), 1)

	storage.Clear()

	assert.Len(t, storage.GetEvents(10), 0)
}

func TestCaptureStorage_ID(t *testing.T) {
	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	defer storage.Close()

	// Storage ID should not be the same as session ID
	assert.NotEqual(t, uuid.Nil, storage.ID())
}

func TestCaptureStorage_SessionID(t *testing.T) {
	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	defer storage.Close()

	assert.Equal(t, sessionID, storage.SessionID())
}
