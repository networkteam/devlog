package collector_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/networkteam/devlog/collector"
)

func TestEventCollector_BasicCollection(t *testing.T) {
	// Create a new event collector
	evtCollector := collector.NewEventCollectorWithOptions(100, collector.DefaultEventOptions())
	defer evtCollector.Close()

	// Create some test data
	testData := "This is test data"

	// Collect the event
	ctx := context.Background()
	evtCollector.CollectEvent(ctx, testData)

	// Verify the event was collected
	events := evtCollector.GetEvents(10)
	require.Len(t, events, 1)

	// Check event properties
	evt := events[0]
	assert.NotEqual(t, uuid.Nil, evt.ID)
	assert.Nil(t, evt.GroupID)
	assert.Equal(t, testData, evt.Data)
	assert.False(t, evt.Start.IsZero())
	assert.False(t, evt.End.IsZero())
	assert.Equal(t, 0, len(evt.Children))
}

func TestEventCollector_StartEndEvent(t *testing.T) {
	// Create a new event collector
	evtCollector := collector.NewEventCollectorWithOptions(100, collector.DefaultEventOptions())
	defer evtCollector.Close()

	// Create some test data
	testData := map[string]string{
		"key": "value",
	}

	// Start an event
	ctx := context.Background()
	ctx = evtCollector.StartEvent(ctx)

	// Simulate some work
	time.Sleep(10 * time.Millisecond)

	// End the event
	evtCollector.EndEvent(ctx, testData)

	// Verify the event was collected
	events := evtCollector.GetEvents(10)
	require.Len(t, events, 1)

	// Check event properties
	evt := events[0]
	assert.NotEqual(t, uuid.Nil, evt.ID)
	assert.Nil(t, evt.GroupID)
	assert.Equal(t, testData, evt.Data)
	assert.False(t, evt.Start.IsZero())
	assert.False(t, evt.End.IsZero())
	assert.True(t, evt.End.After(evt.Start))
	assert.Equal(t, 0, len(evt.Children))

	// Extract the group ID from the context
	groupID, ok := collector.groupIDFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, evt.ID, groupID)
}

func TestEventCollector_NestedEvents(t *testing.T) {
	// Create a new event collector
	evtCollector := collector.NewEventCollectorWithOptions(100, collector.DefaultEventOptions())
	defer evtCollector.Close()

	// Start a parent event
	ctx := context.Background()
	parentCtx := evtCollector.StartEvent(ctx)

	// Collect a child event
	childData := "Child event"
	evtCollector.CollectEvent(parentCtx, childData)

	// Start another nested event
	nestedCtx := evtCollector.StartEvent(parentCtx)

	// Simulate some work
	time.Sleep(10 * time.Millisecond)

	// End the nested event
	nestedData := "Nested event"
	evtCollector.EndEvent(nestedCtx, nestedData)

	// End the parent event
	parentData := "Parent event"
	evtCollector.EndEvent(parentCtx, parentData)

	// Verify the events were collected
	events := evtCollector.GetEvents(10)
	require.Len(t, events, 1) // Only the parent event should be in the top level

	// Check parent event properties
	parent := events[0]
	assert.Equal(t, parentData, parent.Data)
	assert.Nil(t, parent.GroupID)

	// Check children
	require.Len(t, parent.Children, 2)

	// Check child event properties
	found := 0
	for _, child := range parent.Children {
		assert.NotEqual(t, uuid.Nil, child.ID)
		assert.NotNil(t, child.GroupID)
		assert.Equal(t, parent.ID, *child.GroupID)

		// Verify we have both children
		if val, ok := child.Data.(string); ok {
			if val == childData {
				found++
			} else if val == nestedData {
				found++
				// The nested event doesn't have any children since it was empty
				assert.Equal(t, 0, len(child.Children))
			}
		}
	}
	assert.Equal(t, 2, found, "Both children should be found")
}

func TestEventCollector_DeeplyNestedEvents(t *testing.T) {
	// Create a new event collector
	evtCollector := collector.NewEventCollectorWithOptions(100, collector.DefaultEventOptions())
	defer evtCollector.Close()

	// Create a three-level deep event hierarchy
	ctx := context.Background()

	// Level 1
	ctx1 := evtCollector.StartEvent(ctx)

	// Level 2
	ctx2 := evtCollector.StartEvent(ctx1)

	// Level 3
	ctx3 := evtCollector.StartEvent(ctx2)

	// End them in reverse order
	evtCollector.EndEvent(ctx3, "Level 3")
	evtCollector.EndEvent(ctx2, "Level 2")
	evtCollector.EndEvent(ctx1, "Level 1")

	// Verify the events were collected
	events := evtCollector.GetEvents(10)
	require.Len(t, events, 1) // Only the top level event should be in the main buffer

	// Check level 1
	lvl1 := events[0]
	assert.Equal(t, "Level 1", lvl1.Data)
	assert.Nil(t, lvl1.GroupID)
	require.Len(t, lvl1.Children, 1)

	// Check level 2
	lvl2 := lvl1.Children[0]
	assert.Equal(t, "Level 2", lvl2.Data)
	assert.NotNil(t, lvl2.GroupID)
	assert.Equal(t, lvl1.ID, *lvl2.GroupID)
	require.Len(t, lvl2.Children, 1)

	// Check level 3
	lvl3 := lvl2.Children[0]
	assert.Equal(t, "Level 3", lvl3.Data)
	assert.NotNil(t, lvl3.GroupID)
	assert.Equal(t, lvl2.ID, *lvl3.GroupID)
	assert.Len(t, lvl3.Children, 0)
}

func TestEventCollector_MultipleTopLevelEvents(t *testing.T) {
	// Create a new event collector
	evtCollector := collector.NewEventCollectorWithOptions(100, collector.DefaultEventOptions())
	defer evtCollector.Close()

	// Create multiple top-level events
	ctx := context.Background()

	// Event 1
	evtCollector.CollectEvent(ctx, "Event 1")

	// Event 2
	evtCollector.CollectEvent(ctx, "Event 2")

	// Event 3
	ctx3 := evtCollector.StartEvent(ctx)
	evtCollector.EndEvent(ctx3, "Event 3")

	// Verify all events were collected
	events := evtCollector.GetEvents(10)
	require.Len(t, events, 3)

	// Check we have all three events
	foundEvents := make(map[string]bool)
	for _, evt := range events {
		if data, ok := evt.Data.(string); ok {
			foundEvents[data] = true
		}
	}

	assert.True(t, foundEvents["Event 1"], "Event 1 should be found")
	assert.True(t, foundEvents["Event 2"], "Event 2 should be found")
	assert.True(t, foundEvents["Event 3"], "Event 3 should be found")
}

func TestEventCollector_EndEventWithoutStart(t *testing.T) {
	// Create a new event collector
	evtCollector := collector.NewEventCollectorWithOptions(100, collector.DefaultEventOptions())
	defer evtCollector.Close()

	// Try to end an event without starting one
	ctx := context.Background()
	evtCollector.EndEvent(ctx, "This should not be collected")

	// Verify no events were collected
	events := evtCollector.GetEvents(10)
	assert.Len(t, events, 0)
}

func TestEventCollector_WithCustomData(t *testing.T) {
	// Create a new event collector
	evtCollector := collector.NewEventCollectorWithOptions(100, collector.DefaultEventOptions())
	defer evtCollector.Close()

	// Create custom data types to collect
	type HTTPData struct {
		Method string
		URL    string
		Status int
	}

	type LogData struct {
		Level   string
		Message string
	}

	// Collect different types of events
	ctx := context.Background()

	// HTTP event
	httpData := HTTPData{
		Method: "GET",
		URL:    "https://example.com",
		Status: 200,
	}
	evtCollector.CollectEvent(ctx, httpData)

	// Log event
	logData := LogData{
		Level:   "INFO",
		Message: "This is a log message",
	}
	evtCollector.CollectEvent(ctx, logData)

	// Verify both events were collected
	events := evtCollector.GetEvents(10)
	require.Len(t, events, 2)

	// Check we can retrieve the typed data
	foundHTTP := false
	foundLog := false

	for _, evt := range events {
		switch data := evt.Data.(type) {
		case HTTPData:
			assert.Equal(t, "GET", data.Method)
			assert.Equal(t, "https://example.com", data.URL)
			assert.Equal(t, 200, data.Status)
			foundHTTP = true

		case LogData:
			assert.Equal(t, "INFO", data.Level)
			assert.Equal(t, "This is a log message", data.Message)
			foundLog = true
		}
	}

	assert.True(t, foundHTTP, "HTTP event should be found")
	assert.True(t, foundLog, "Log event should be found")
}

func TestEventCollector_ConcurrentEvents(t *testing.T) {
	// Create a new event collector
	evtCollector := collector.NewEventCollectorWithOptions(100, collector.DefaultEventOptions())
	defer evtCollector.Close()

	// Create multiple events concurrently
	ctx := context.Background()
	numGoroutines := 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			// Start an event
			eventCtx := evtCollector.StartEvent(ctx)

			// Add a child event
			evtCollector.CollectEvent(eventCtx, "Child of "+string(rune('A'+id)))

			// End the event
			evtCollector.EndEvent(eventCtx, "Parent "+string(rune('A'+id)))
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify all events were collected
	events := evtCollector.GetEvents(uint64(numGoroutines * 2))
	assert.Len(t, events, numGoroutines)

	// Check that each parent has a child
	for _, evt := range events {
		require.Len(t, evt.Children, 1)

		// Parent should be "Parent X"
		parentData, ok := evt.Data.(string)
		require.True(t, ok)
		assert.Contains(t, parentData, "Parent ")

		// Child should be "Child of X"
		childData, ok := evt.Children[0].Data.(string)
		require.True(t, ok)
		assert.Contains(t, childData, "Child of ")

		// The parent and child letters should match
		parentLetter := parentData[len(parentData)-1]
		childLetter := childData[len(childData)-1]
		assert.Equal(t, parentLetter, childLetter)
	}
}

func TestEventCollector_Notification(t *testing.T) {
	// Create a new event collector
	evtCollector := collector.NewEventCollectorWithOptions(100, collector.DefaultEventOptions())
	defer evtCollector.Close()

	// Create a context for subscription
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe to notifications
	events := make(chan collector.Event, 10)
	subscription := evtCollector.Subscribe(ctx)

	// Start a goroutine to collect notifications
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		for evt := range subscription {
			events <- evt
		}
	}()

	// Collect some events
	eventCtx := context.Background()
	evtCollector.CollectEvent(eventCtx, "Event 1")
	evtCollector.CollectEvent(eventCtx, "Event 2")

	// Start and end an event
	nestedCtx := evtCollector.StartEvent(eventCtx)
	evtCollector.EndEvent(nestedCtx, "Event 3")

	// Wait a bit for events to be processed
	time.Sleep(50 * time.Millisecond)

	// Cancel the subscription context
	cancel()

	// Wait for the goroutine to finish
	wg.Wait()

	// Check notifications
	assert.Len(t, events, 3)

	// Check the contents of the events
	receivedEvents := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		select {
		case evt := <-events:
			if data, ok := evt.Data.(string); ok {
				receivedEvents = append(receivedEvents, data)
			}
		default:
			t.Fatal("Expected to receive 3 events")
		}
	}

	assert.Contains(t, receivedEvents, "Event 1")
	assert.Contains(t, receivedEvents, "Event 2")
	assert.Contains(t, receivedEvents, "Event 3")
}

func TestEventCollector_RingBufferCapacity(t *testing.T) {
	// Create a new event collector with small capacity
	capacity := uint64(5)
	evtCollector := collector.NewEventCollectorWithOptions(capacity, collector.DefaultEventOptions())
	defer evtCollector.Close()

	// Collect more events than the capacity
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		evtCollector.CollectEvent(ctx, "Event "+string(rune('A'+i)))
	}

	// Verify only the capacity number of events were kept
	events := evtCollector.GetEvents(20)
	assert.Len(t, events, int(capacity))

	// Verify we have the most recent events (F-J, not A-E)
	for _, evt := range events {
		data, ok := evt.Data.(string)
		require.True(t, ok)

		// The event should be one of the later ones (F-J)
		letter := data[len(data)-1]
		assert.True(t, letter >= 'F' && letter <= 'J')
	}
}

func TestEventCollector_IntegrationWithSlog(t *testing.T) {
	// Create a new event collector
	evtCollector := collector.NewEventCollectorWithOptions(100, collector.DefaultEventOptions())
	defer evtCollector.Close()

	// Create a context with a group ID
	ctx := context.Background()
	ctx = evtCollector.StartEvent(ctx)

	// Create a log record
	record := slog.Record{
		Time:    time.Now(),
		Message: "Test log message",
		Level:   slog.LevelInfo,
	}

	// Collect the log record as a child event
	evtCollector.CollectEvent(ctx, record)

	// End the parent event
	evtCollector.EndEvent(ctx, "HTTP request")

	// Verify the events were collected
	events := evtCollector.GetEvents(10)
	require.Len(t, events, 1)

	// Check parent event
	assert.Equal(t, "HTTP request", events[0].Data)
	require.Len(t, events[0].Children, 1)

	// Check log event
	logEvent := events[0].Children[0]

	// Verify the log record
	logRecord, ok := logEvent.Data.(slog.Record)
	require.True(t, ok)
	assert.Equal(t, "Test log message", logRecord.Message)
	assert.Equal(t, slog.LevelInfo, logRecord.Level)
}

func TestEventCollector_IntegrationWithHTTP(t *testing.T) {
	// Create a new event collector
	evtCollector := collector.NewEventCollectorWithOptions(100, collector.DefaultEventOptions())
	defer evtCollector.Close()

	// Simulate an HTTP server request
	serverCtx := context.Background()
	serverCtx = evtCollector.StartEvent(serverCtx)

	// Simulate an HTTP client request made during server request processing
	clientData := map[string]interface{}{
		"method": "GET",
		"url":    "https://api.example.com/data",
		"status": 200,
	}
	evtCollector.CollectEvent(serverCtx, clientData)

	// End the server request
	serverData := map[string]interface{}{
		"method":  "POST",
		"path":    "/process",
		"status":  200,
		"latency": "150ms",
	}
	evtCollector.EndEvent(serverCtx, serverData)

	// Verify the events were collected
	events := evtCollector.GetEvents(10)
	require.Len(t, events, 1)

	// Check server event
	serverEvent := events[0]
	serverEventData, ok := serverEvent.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "POST", serverEventData["method"])
	assert.Equal(t, "/process", serverEventData["path"])
	assert.Equal(t, 200, serverEventData["status"])

	// Check client event
	require.Len(t, serverEvent.Children, 1)
	clientEvent := serverEvent.Children[0]
	clientEventData, ok := clientEvent.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "GET", clientEventData["method"])
	assert.Equal(t, "https://api.example.com/data", clientEventData["url"])
	assert.Equal(t, 200, clientEventData["status"])
}
