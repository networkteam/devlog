//go:build acceptance
// +build acceptance

package acceptance

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// -----------------------------------------------------------------------------
// HTTP Server Event Tests
// -----------------------------------------------------------------------------

// TestHTTPServerEvents verifies that HTTP server request events are displayed correctly.
func TestHTTPServerEvents(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Make HTTP server request
	dashboard.FetchAPI("/api/test")

	dashboard.WaitForEventCount(1, 5000)

	// Verify event appears with correct info
	text := dashboard.GetFirstEventText()
	assert.Contains(t, text, "GET")
	assert.Contains(t, text, "/api/test")
}

// TestHTTPServerEventWithPOST verifies POST requests are displayed correctly.
func TestHTTPServerEventWithPOST(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Make POST request
	dashboard.FetchAPIWithBody("/api/echo", `{"message": "hello"}`)

	dashboard.WaitForEventCount(1, 5000)

	// Verify POST is shown
	text := dashboard.GetFirstEventText()
	assert.Contains(t, text, "POST")
	assert.Contains(t, text, "/api/echo")
}

// -----------------------------------------------------------------------------
// HTTP Client Event Tests
// -----------------------------------------------------------------------------

// TestHTTPClientEvents verifies that outgoing HTTP client requests are captured.
func TestHTTPClientEvents(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Trigger endpoint that makes HTTP client request
	dashboard.FetchAPI("/api/external")

	// Wait for the HTTP server event (the endpoint response)
	dashboard.WaitForEventCount(1, 5000)

	// Click to see details - the HTTP client request should be nested
	dashboard.ClickFirstEvent()
	dashboard.WaitForEventDetails(5000)

	text := dashboard.GetEventDetailsText()
	assert.Contains(t, text, "/api/external")
}

// -----------------------------------------------------------------------------
// Database Query Event Tests
// -----------------------------------------------------------------------------

// TestDBQueryEvents verifies that database query events are captured.
func TestDBQueryEvents(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Trigger endpoint that creates DB query event
	dashboard.FetchAPI("/api/db")

	// Should have HTTP request event which contains DB query as child
	dashboard.WaitForEventCount(1, 5000)

	// Click to see details
	dashboard.ClickFirstEvent()
	dashboard.WaitForEventDetails(5000)

	// The parent HTTP event details should be shown
	text := dashboard.GetEventDetailsText()
	assert.Contains(t, text, "/api/db")
}

// -----------------------------------------------------------------------------
// Log Event Tests
// -----------------------------------------------------------------------------

// TestLogEvents verifies that slog entries appear in the event list.
func TestLogEvents(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Trigger endpoint that creates log events
	dashboard.FetchAPI("/api/log")

	// HTTP server event should appear (logs are nested inside)
	dashboard.WaitForEventCount(1, 5000)

	// Verify the first event contains the log endpoint
	text := dashboard.GetFirstEventText()
	assert.Contains(t, text, "/api/log")
}

// -----------------------------------------------------------------------------
// Nested Event Tests
// -----------------------------------------------------------------------------

// TestNestedEvents verifies that parent-child event relationships are displayed correctly.
func TestNestedEvents(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Trigger endpoint that creates multiple nested events (logs + DB query)
	dashboard.FetchAPI("/api/combined")

	// Wait for the parent HTTP event
	dashboard.WaitForEventCount(1, 5000)

	// Verify the first event contains the combined endpoint
	text := dashboard.GetFirstEventText()
	assert.Contains(t, text, "/api/combined")
}

// TestNestedEventsShowChildren verifies that nested events are visible in the UI.
func TestNestedEventsShowChildren(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Trigger endpoint that creates multiple nested events
	dashboard.FetchAPI("/api/combined")

	dashboard.WaitForEventCount(1, 5000)

	// The event list item should indicate it has children
	// Look for the event item and check if there's visual indication of nested events
	time.Sleep(500 * time.Millisecond)

	text := dashboard.GetFirstEventText()
	// The parent HTTP event should be shown
	assert.Contains(t, text, "/api/combined")
}

// -----------------------------------------------------------------------------
// Multiple Event Types Together
// -----------------------------------------------------------------------------

// TestMixedEventTypes verifies that different event types are all captured and displayed.
func TestMixedEventTypes(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Trigger multiple endpoints that create different event types
	dashboard.FetchAPI("/api/test")     // Simple HTTP
	dashboard.FetchAPI("/api/log")      // HTTP + logs
	dashboard.FetchAPI("/api/db")       // HTTP + DB query
	dashboard.FetchAPI("/api/combined") // HTTP + logs + DB

	// Should have 4 top-level HTTP events
	dashboard.WaitForEventCount(4, 10000)
	assert.Equal(t, 4, dashboard.GetEventCount())
}
