package acceptance

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// Global Mode Tests
// -----------------------------------------------------------------------------

// TestGlobalMode_SameContext verifies that global mode captures requests from the same browser context.
func TestGlobalMode_SameContext(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Make a request from the same browser context
	dashboard.FetchAPI("/api/test")

	dashboard.WaitForEventCount(1, 5000)
	assert.Equal(t, 1, dashboard.GetEventCount())
}

// TestGlobalMode_DifferentContext verifies that global mode captures requests from different browser contexts.
func TestGlobalMode_DifferentContext(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx1 := pw.NewContext(t)
	defer ctx1.Close()

	dashboard := NewDashboardPage(t, ctx1, app.DevlogURL)
	dashboard.StartCapture("global")

	// Make request from a completely different browser context (no cookies)
	ctx2 := pw.NewContext(t)
	defer ctx2.Close()

	page2, err := ctx2.NewPage()
	require.NoError(t, err)

	_, err = page2.Goto(app.AppURL + "/api/test")
	require.NoError(t, err)

	// In global mode, this should still be captured
	dashboard.WaitForEventCount(1, 5000)
	assert.Equal(t, 1, dashboard.GetEventCount())
}

// TestGlobalMode_ReloadPersistence verifies that events persist after page reload in global mode.
func TestGlobalMode_ReloadPersistence(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Make a request
	dashboard.FetchAPI("/api/test")

	dashboard.WaitForEventCount(1, 5000)
	assert.Equal(t, 1, dashboard.GetEventCount())

	// Reload the page
	dashboard.Reload()

	// Events should persist after reload
	assert.Equal(t, 1, dashboard.GetEventCount())
}

// TestClearEvents_GlobalMode verifies that events can be cleared in global mode.
func TestClearEvents_GlobalMode(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("global")

	// Create some events
	for i := 0; i < 3; i++ {
		_, err := dashboard.Page.Evaluate(fmt.Sprintf(`fetch('/api/test?i=%d')`, i))
		require.NoError(t, err)
	}

	dashboard.WaitForEventCount(3, 5000)
	assert.Equal(t, 3, dashboard.GetEventCount())

	// Clear events
	dashboard.ClearEvents()

	assert.Equal(t, 0, dashboard.GetEventCount())
}

// -----------------------------------------------------------------------------
// Session Mode Tests
// -----------------------------------------------------------------------------

// TestSessionMode_SameContext verifies that session mode captures requests from the same browser context.
func TestSessionMode_SameContext(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("session")

	// Make a request from the same browser context (has session cookie)
	dashboard.FetchAPI("/api/test")

	dashboard.WaitForEventCount(1, 5000)
	assert.Equal(t, 1, dashboard.GetEventCount())
}

// TestSessionMode_DifferentContext verifies that session mode does NOT capture requests from different browser contexts.
func TestSessionMode_DifferentContext(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx1 := pw.NewContext(t)
	defer ctx1.Close()

	dashboard := NewDashboardPage(t, ctx1, app.DevlogURL)
	dashboard.StartCapture("session")

	// Make request from different browser context (no session cookie)
	ctx2 := pw.NewContext(t)
	defer ctx2.Close()

	page2, err := ctx2.NewPage()
	require.NoError(t, err)

	_, err = page2.Goto(app.AppURL + "/api/test")
	require.NoError(t, err)

	// In session mode, this should NOT be captured (no cookie)
	// Poll for 2 seconds to verify no events appear
	dashboard.ExpectNoEvents(2 * time.Second)
}

// TestSessionMode_ReloadPersistence verifies that events persist and new events are captured after reload in session mode.
func TestSessionMode_ReloadPersistence(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("session")

	// Make first request
	dashboard.FetchAPI("/api/test?n=1")

	dashboard.WaitForEventCount(1, 5000)
	assert.Equal(t, 1, dashboard.GetEventCount())

	// Reload page
	dashboard.Reload()

	// Old events should persist
	assert.Equal(t, 1, dashboard.GetEventCount())

	// Make another request after reload - should still capture
	dashboard.FetchAPI("/api/test?n=2")

	dashboard.WaitForEventCount(2, 5000)
	assert.Equal(t, 2, dashboard.GetEventCount())
}

// TestClearEvents_SessionMode verifies that events can be cleared in session mode.
func TestClearEvents_SessionMode(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx := pw.NewContext(t)
	defer ctx.Close()

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)
	dashboard.StartCapture("session")

	// Create some events
	for i := 0; i < 3; i++ {
		_, err := dashboard.Page.Evaluate(fmt.Sprintf(`fetch('/api/test?i=%d')`, i))
		require.NoError(t, err)
	}

	dashboard.WaitForEventCount(3, 5000)
	assert.Equal(t, 3, dashboard.GetEventCount())

	// Clear events
	dashboard.ClearEvents()

	assert.Equal(t, 0, dashboard.GetEventCount())
}

// -----------------------------------------------------------------------------
// Mode Switching Tests
// -----------------------------------------------------------------------------

// TestModeSwitching verifies that switching between session and global mode works correctly.
func TestModeSwitching(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx1 := pw.NewContext(t)
	defer ctx1.Close()

	dashboard := NewDashboardPage(t, ctx1, app.DevlogURL)
	dashboard.StartCapture("session")

	// Create a second context for external requests
	ctx2 := pw.NewContext(t)
	defer ctx2.Close()

	page2, err := ctx2.NewPage()
	require.NoError(t, err)

	// External request in session mode - should NOT be captured
	_, err = page2.Goto(app.AppURL + "/api/test?source=external1")
	require.NoError(t, err)

	// Poll for 1 second to verify no events appear
	dashboard.ExpectNoEvents(1 * time.Second)

	// Switch to global mode
	dashboard.SwitchMode("global")

	// External request in global mode - should be captured
	_, err = page2.Goto(app.AppURL + "/api/test?source=external2")
	require.NoError(t, err)

	dashboard.WaitForEventCount(1, 5000)
	assert.Equal(t, 1, dashboard.GetEventCount(), "global mode should capture external requests")
}

// TestModeSwitchingBackToSession verifies switching from global back to session mode.
func TestModeSwitchingBackToSession(t *testing.T) {
	t.Parallel()

	app := NewTestApp(t)
	defer app.Close()

	pw := NewPlaywrightFixture(t)
	defer pw.Close()

	ctx1 := pw.NewContext(t)
	defer ctx1.Close()

	dashboard := NewDashboardPage(t, ctx1, app.DevlogURL)
	dashboard.StartCapture("global")

	// Create a second context for external requests
	ctx2 := pw.NewContext(t)
	defer ctx2.Close()

	page2, err := ctx2.NewPage()
	require.NoError(t, err)

	// External request in global mode - should be captured
	_, err = page2.Goto(app.AppURL + "/api/test?source=external1")
	require.NoError(t, err)

	dashboard.WaitForEventCount(1, 5000)
	assert.Equal(t, 1, dashboard.GetEventCount())

	// Clear and switch to session mode
	dashboard.ClearEvents()
	dashboard.SwitchMode("session")

	// External request in session mode - should NOT be captured
	_, err = page2.Goto(app.AppURL + "/api/test?source=external2")
	require.NoError(t, err)

	// Poll for 1 second to verify no events appear
	dashboard.ExpectNoEvents(1 * time.Second)

	// Same context request should still work
	dashboard.FetchAPI("/api/test?source=same")
	dashboard.WaitForEventCount(1, 5000)
	assert.Equal(t, 1, dashboard.GetEventCount(), "session mode should capture same-context requests")
}
