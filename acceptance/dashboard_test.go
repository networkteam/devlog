package acceptance

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDashboardAccess verifies that the dashboard is accessible and redirects to a session URL.
func TestDashboardAccess(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	page, err := ctx.NewPage()
	require.NoError(t, err)

	_, err = page.Goto(app.DevlogURL)
	require.NoError(t, err)

	// Verify redirect to session URL
	err = page.WaitForURL("**/_devlog/s/*/", playwright.PageWaitForURLOptions{
		Timeout: playwright.Float(5000),
	})
	require.NoError(t, err)

	url := page.URL()
	assert.Regexp(t, `/_devlog/s/[a-f0-9-]+/$`, url)

	// Verify dashboard elements present
	captureControls := page.Locator("#capture-controls")
	visible, err := captureControls.IsVisible()
	require.NoError(t, err)
	assert.True(t, visible, "capture controls should be visible")
}

// TestSSERealTimeUpdates verifies that events appear in real-time via SSE.
func TestSSERealTimeUpdates(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Make requests via Go HTTP client (external to browser)
	for i := 0; i < 3; i++ {
		go func(n int) {
			http.Get(app.AppURL + fmt.Sprintf("/api/test?i=%d", n))
		}(i)
	}

	// Events should appear via SSE without page refresh
	dashboard.WaitForEventCount(3, 10000)
	assert.Equal(t, 3, dashboard.GetEventCount())
}

// TestEventDetailsPanel verifies that clicking an event shows its details.
func TestEventDetailsPanel(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Make a POST request with body
	dashboard.FetchAPIWithBody("/api/echo", `{"test": "data"}`)
	dashboard.WaitForEventCount(1, 5000)

	// Click the event to see details
	dashboard.ClickFirstEvent()
	dashboard.WaitForEventDetails(5000)

	// Verify the path is shown in details
	text := dashboard.GetEventDetailsText()
	assert.Contains(t, text, "/api/echo")
}

// TestStopCapture verifies that stopping capture prevents new events from being recorded.
func TestStopCapture(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Make a request while capturing (from browser to ensure cookie/context)
	dashboard.FetchAPI("/api/test?before=stop")
	dashboard.WaitForEventCount(1, 5000)
	assert.Equal(t, 1, dashboard.GetEventCount())

	// Stop capture
	dashboard.StopCapture()

	// Make another request after stopping (from browser)
	dashboard.FetchAPI("/api/test?after=stop")

	// Poll for 2 seconds to verify event count stays at 1 (no new events)
	dashboard.ExpectEventCountStable(1, 2*time.Second)
}

// TestMultipleConcurrentRequests verifies that multiple concurrent requests are all captured.
func TestMultipleConcurrentRequests(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Fire multiple concurrent requests from browser
	_, err := dashboard.Page.Evaluate(`
		Promise.all([
			fetch('/api/test?n=1'),
			fetch('/api/test?n=2'),
			fetch('/api/test?n=3'),
			fetch('/api/test?n=4'),
			fetch('/api/test?n=5')
		])
	`)
	require.NoError(t, err)

	// All should appear
	dashboard.WaitForEventCount(5, 10000)
	assert.Equal(t, 5, dashboard.GetEventCount())
}

// TestLargeRequestBody verifies that large request bodies are captured.
func TestLargeRequestBody(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Create a request with larger body
	_, err := dashboard.Page.Evaluate(`
		const largeData = 'x'.repeat(10000);
		fetch('/api/echo', {
			method: 'POST',
			headers: {'Content-Type': 'text/plain'},
			body: largeData
		})
	`)
	require.NoError(t, err)

	dashboard.WaitForEventCount(1, 5000)
	assert.Equal(t, 1, dashboard.GetEventCount())
}

// TestNewSessionAfterBrowserClose verifies that closing and reopening creates a new session.
func TestNewSessionAfterBrowserClose(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	// First session
	ctx1 := pw.NewContext(t)
	dashboard1 := NewDashboardPage(t, ctx1, app.DevlogURL)
	firstSessionURL := dashboard1.SessionURL

	dashboard1.StartCapture("global")
	dashboard1.FetchAPI("/api/test")
	dashboard1.WaitForEventCount(1, 5000)
	ctx1.Close()

	// Second session - should get new session URL
	ctx2 := pw.NewContext(t)
	defer ctx2.Close()

	dashboard2 := NewDashboardPage(t, ctx2, app.DevlogURL)
	secondSessionURL := dashboard2.SessionURL

	assert.NotEqual(t, firstSessionURL, secondSessionURL, "should get new session URL")

	// Second session should start fresh
	assert.Equal(t, 0, dashboard2.GetEventCount(), "new session should have no events")
}

// TestDownloadRequestBody verifies that request body can be downloaded via the download link.
func TestDownloadRequestBody(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Make a POST request with known body content
	dashboard.FetchAPIWithBody("/api/echo", `{"test": "download-data"}`)
	dashboard.WaitForEventCount(1, 5000)

	// Click the event to see details
	dashboard.ClickFirstEvent()
	dashboard.WaitForEventDetails(5000)

	// Download the request body and verify
	path, body, contentType := dashboard.DownloadRequestBody()

	// Verify the URL includes the session prefix /s/{sid}/
	assert.Contains(t, path, "/s/", "download URL should include session prefix")
	assert.Contains(t, path, "/download/request-body/", "download URL should be for request body")

	// Verify the content type and body content
	assert.Contains(t, contentType, "application/json", "content type should be JSON")
	assert.Contains(t, string(body), "download-data", "downloaded body should contain original data")
}

// TestDownloadResponseBody verifies that response body can be downloaded via the download link.
func TestDownloadResponseBody(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Make a POST request - the echo endpoint returns the same body
	dashboard.FetchAPIWithBody("/api/echo", `{"response": "body-test"}`)
	dashboard.WaitForEventCount(1, 5000)

	// Click the event to see details
	dashboard.ClickFirstEvent()
	dashboard.WaitForEventDetails(5000)

	// Download the response body and verify
	path, body, contentType := dashboard.DownloadResponseBody()

	// Verify the URL structure
	assert.Contains(t, path, "/s/", "download URL should include session prefix")
	assert.Contains(t, path, "/download/response-body/", "download URL should be for response body")

	// Verify the content type and body content
	assert.Contains(t, contentType, "application/json", "content type should be JSON")
	assert.Contains(t, string(body), "body-test", "downloaded body should contain original data")
}

// TestUsagePanel verifies that the usage panel shows memory and session stats.
func TestUsagePanel(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)

	// Wait for usage panel to load
	dashboard.WaitForUsagePanel(5000)

	// Check that usage panel is visible and has content
	usageText := dashboard.GetUsagePanelText()
	assert.NotEmpty(t, usageText, "usage panel should have content")

	// Should show memory usage (contains "B" for bytes suffix)
	assert.Contains(t, usageText, "B", "usage panel should show memory stats")

	// Should show session count (at least 1 session - the current one)
	assert.Regexp(t, `\d`, usageText, "usage panel should show session count")

	// Start capture and make some requests
	dashboard.StartCapture("global")

	// Make requests to generate events with body content
	dashboard.FetchAPIWithBody("/api/echo", `{"test": "data with some content"}`)
	dashboard.WaitForEventCount(1, 5000)

	// Wait for stats to refresh (polls every 5 seconds, but we wait for first poll)
	time.Sleep(1 * time.Second)

	// Check that memory increased (panel should show non-zero memory)
	// Just verify the panel is still showing valid stats
	updatedText := dashboard.GetUsagePanelText()
	assert.NotEmpty(t, updatedText, "usage panel should still have content after events")
	assert.Contains(t, updatedText, "B", "usage panel should still show memory stats")
}
