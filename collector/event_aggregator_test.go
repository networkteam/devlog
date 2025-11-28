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

func TestEventAggregator_ShouldCapture_NoStorages(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	ctx := context.Background()

	assert.False(t, aggregator.ShouldCapture(ctx))
}

func TestEventAggregator_ShouldCapture_SessionModeMatch(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeSession)
	aggregator.RegisterStorage(storage)

	ctx := collector.WithSessionID(context.Background(), sessionID)

	assert.True(t, aggregator.ShouldCapture(ctx))
}

func TestEventAggregator_ShouldCapture_SessionModeNoMatch(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	otherSessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeSession)
	aggregator.RegisterStorage(storage)

	ctx := collector.WithSessionID(context.Background(), otherSessionID)

	assert.False(t, aggregator.ShouldCapture(ctx))
}

func TestEventAggregator_ShouldCapture_GlobalMode(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	aggregator.RegisterStorage(storage)

	// Should return true for any context
	assert.True(t, aggregator.ShouldCapture(context.Background()))

	// Even with a different session ID
	otherSessionID := uuid.Must(uuid.NewV4())
	ctx := collector.WithSessionID(context.Background(), otherSessionID)
	assert.True(t, aggregator.ShouldCapture(ctx))
}

func TestEventAggregator_CollectEvent_DispatchesToMatchingStorages(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionA := uuid.Must(uuid.NewV4())
	sessionB := uuid.Must(uuid.NewV4())

	storageA := collector.NewCaptureStorage(sessionA, 100, collector.CaptureModeSession)
	storageB := collector.NewCaptureStorage(sessionB, 100, collector.CaptureModeGlobal)

	aggregator.RegisterStorage(storageA)
	aggregator.RegisterStorage(storageB)

	// Event with session A should go to both storages
	// (A matches session, B is global)
	ctx := collector.WithSessionID(context.Background(), sessionA)
	aggregator.CollectEvent(ctx, "test event")

	eventsA := storageA.GetEvents(10)
	eventsB := storageB.GetEvents(10)

	require.Len(t, eventsA, 1)
	require.Len(t, eventsB, 1)
	assert.Equal(t, "test event", eventsA[0].Data)
	assert.Equal(t, "test event", eventsB[0].Data)
}

func TestEventAggregator_CollectEvent_MultipleGlobalStorages(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionA := uuid.Must(uuid.NewV4())
	sessionB := uuid.Must(uuid.NewV4())

	storageA := collector.NewCaptureStorage(sessionA, 100, collector.CaptureModeGlobal)
	storageB := collector.NewCaptureStorage(sessionB, 100, collector.CaptureModeGlobal)

	aggregator.RegisterStorage(storageA)
	aggregator.RegisterStorage(storageB)

	// Event should go to both global storages
	aggregator.CollectEvent(context.Background(), "test event")

	eventsA := storageA.GetEvents(10)
	eventsB := storageB.GetEvents(10)

	require.Len(t, eventsA, 1)
	require.Len(t, eventsB, 1)
}

func TestEventAggregator_CollectEvent_NoCapture_NoDispatch(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionA := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionA, 100, collector.CaptureModeSession)
	aggregator.RegisterStorage(storage)

	// Event with different session should not be captured
	otherSessionID := uuid.Must(uuid.NewV4())
	ctx := collector.WithSessionID(context.Background(), otherSessionID)
	aggregator.CollectEvent(ctx, "test event")

	events := storage.GetEvents(10)
	assert.Len(t, events, 0)
}

func TestEventAggregator_RegisterUnregister_Storage(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)

	// Register
	aggregator.RegisterStorage(storage)
	assert.True(t, aggregator.ShouldCapture(context.Background()))

	// Get storage
	retrieved := aggregator.GetStorage(storage.ID())
	assert.NotNil(t, retrieved)
	assert.Equal(t, storage.ID(), retrieved.ID())

	// Unregister
	aggregator.UnregisterStorage(storage.ID())
	assert.False(t, aggregator.ShouldCapture(context.Background()))
	assert.Nil(t, aggregator.GetStorage(storage.ID()))
}

func TestEventAggregator_StartEndEvent_WithCapture(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	aggregator.RegisterStorage(storage)

	ctx := context.Background()
	ctx = aggregator.StartEvent(ctx)

	time.Sleep(10 * time.Millisecond)

	aggregator.EndEvent(ctx, "test event")

	events := storage.GetEvents(10)
	require.Len(t, events, 1)
	assert.Equal(t, "test event", events[0].Data)
	assert.True(t, events[0].End.After(events[0].Start))
}

func TestEventAggregator_StartEndEvent_NoCapture(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	// No storages registered

	ctx := context.Background()
	ctx = aggregator.StartEvent(ctx)
	aggregator.EndEvent(ctx, "test event")

	// Can't verify no events stored since there's no storage
	// but at least it shouldn't panic
	assert.False(t, aggregator.ShouldCapture(ctx))
}

func TestEventAggregator_NestedEvents_WithCapture(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	aggregator.RegisterStorage(storage)

	// Start parent
	ctx := context.Background()
	parentCtx := aggregator.StartEvent(ctx)

	// Collect child
	aggregator.CollectEvent(parentCtx, "child event")

	// End parent
	aggregator.EndEvent(parentCtx, "parent event")

	events := storage.GetEvents(10)
	require.Len(t, events, 1)

	parent := events[0]
	assert.Equal(t, "parent event", parent.Data)
	require.Len(t, parent.Children, 1)
	assert.Equal(t, "child event", parent.Children[0].Data)
}

func TestEventAggregator_DeeplyNestedEvents(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	aggregator.RegisterStorage(storage)

	ctx := context.Background()

	// Level 1
	ctx1 := aggregator.StartEvent(ctx)

	// Level 2
	ctx2 := aggregator.StartEvent(ctx1)

	// Level 3
	ctx3 := aggregator.StartEvent(ctx2)

	// End in reverse order
	aggregator.EndEvent(ctx3, "Level 3")
	aggregator.EndEvent(ctx2, "Level 2")
	aggregator.EndEvent(ctx1, "Level 1")

	events := storage.GetEvents(10)
	require.Len(t, events, 1)

	lvl1 := events[0]
	assert.Equal(t, "Level 1", lvl1.Data)
	require.Len(t, lvl1.Children, 1)

	lvl2 := lvl1.Children[0]
	assert.Equal(t, "Level 2", lvl2.Data)
	require.Len(t, lvl2.Children, 1)

	lvl3 := lvl2.Children[0]
	assert.Equal(t, "Level 3", lvl3.Data)
	assert.Len(t, lvl3.Children, 0)
}

func TestEventAggregator_ConcurrentEvents(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	aggregator.RegisterStorage(storage)

	ctx := context.Background()
	numGoroutines := 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			eventCtx := aggregator.StartEvent(ctx)
			aggregator.CollectEvent(eventCtx, "Child")
			aggregator.EndEvent(eventCtx, "Parent")
		}(i)
	}

	wg.Wait()

	events := storage.GetEvents(uint64(numGoroutines * 2))
	assert.Len(t, events, numGoroutines)

	for _, evt := range events {
		assert.Equal(t, "Parent", evt.Data)
		require.Len(t, evt.Children, 1)
		assert.Equal(t, "Child", evt.Children[0].Data)
	}
}

func TestEventAggregator_EndEventWithoutStart(t *testing.T) {
	aggregator := collector.NewEventAggregator()
	defer aggregator.Close()

	sessionID := uuid.Must(uuid.NewV4())
	storage := collector.NewCaptureStorage(sessionID, 100, collector.CaptureModeGlobal)
	aggregator.RegisterStorage(storage)

	// Try to end an event without starting one
	ctx := context.Background()
	aggregator.EndEvent(ctx, "This should not be captured")

	events := storage.GetEvents(10)
	assert.Len(t, events, 0)
}
