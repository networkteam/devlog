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

// TestDashboard tests core dashboard functionality.
func TestDashboard(t *testing.T) {
	t.Parallel()

	t.Run("access and session redirect", func(t *testing.T) {
		t.Parallel()
		WithTestApp(t, func(t *testing.T, app *TestApp, pw *PlaywrightFixture) {
			ctx := pw.NewContext(t)
			t.Cleanup(func() { ctx.Close() })

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
		})
	})

	t.Run("SSE real-time updates", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			f.Dashboard.StartCapture("global")

			// Make requests via Go HTTP client (external to browser)
			for i := 0; i < 3; i++ {
				go func(n int) {
					http.Get(f.App.AppURL + fmt.Sprintf("/api/test?i=%d", n))
				}(i)
			}

			// Events should appear via SSE without page refresh
			f.Dashboard.WaitForEventCount(3, 10000)
			assert.Equal(t, 3, f.Dashboard.GetEventCount())
		})
	})

	t.Run("event details panel", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			f.Dashboard.StartCapture("global")

			// Make a POST request with body
			f.Dashboard.FetchAPIWithBody("/api/echo", `{"test": "data"}`)
			f.Dashboard.WaitForEventCount(1, 5000)

			// Click the event to see details
			f.Dashboard.ClickFirstEvent()
			f.Dashboard.WaitForEventDetails(5000)

			// Verify the path is shown in details
			text := f.Dashboard.GetEventDetailsText()
			assert.Contains(t, text, "/api/echo")
		})
	})

	t.Run("stop capture", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			f.Dashboard.StartCapture("global")

			// Make a request while capturing
			f.Dashboard.FetchAPI("/api/test?before=stop")
			f.Dashboard.WaitForEventCount(1, 5000)
			assert.Equal(t, 1, f.Dashboard.GetEventCount())

			// Stop capture
			f.Dashboard.StopCapture()

			// Make another request after stopping
			f.Dashboard.FetchAPI("/api/test?after=stop")

			// Verify event count stays at 1 (no new events)
			f.Dashboard.ExpectEventCountStable(1, 2*time.Second)
		})
	})

	t.Run("multiple concurrent requests", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			f.Dashboard.StartCapture("global")

			// Fire multiple concurrent requests from browser
			_, err := f.Dashboard.Page.Evaluate(`
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
			f.Dashboard.WaitForEventCount(5, 10000)
			assert.Equal(t, 5, f.Dashboard.GetEventCount())
		})
	})

	t.Run("new session after browser close", func(t *testing.T) {
		t.Parallel()
		WithTestApp(t, func(t *testing.T, app *TestApp, pw *PlaywrightFixture) {
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
			t.Cleanup(func() { ctx2.Close() })

			dashboard2 := NewDashboardPage(t, ctx2, app.DevlogURL)
			secondSessionURL := dashboard2.SessionURL

			assert.NotEqual(t, firstSessionURL, secondSessionURL, "should get new session URL")

			// Second session should start fresh
			assert.Equal(t, 0, dashboard2.GetEventCount(), "new session should have no events")
		})
	})

	t.Run("usage panel", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			// Wait for usage panel to load
			f.Dashboard.WaitForUsagePanel(5000)

			// Check that usage panel is visible and has content
			usageText := f.Dashboard.GetUsagePanelText()
			assert.NotEmpty(t, usageText, "usage panel should have content")

			// Should show memory usage (contains "B" for bytes suffix)
			assert.Contains(t, usageText, "B", "usage panel should show memory stats")

			// Should show session count (at least 1 session - the current one)
			assert.Regexp(t, `\d`, usageText, "usage panel should show session count")

			// Start capture and make some requests
			f.Dashboard.StartCapture("global")

			// Make requests to generate events with body content
			f.Dashboard.FetchAPIWithBody("/api/echo", `{"test": "data with some content"}`)
			f.Dashboard.WaitForEventCount(1, 5000)

			// Wait for stats to refresh
			time.Sleep(1 * time.Second)

			// Check that memory panel still shows valid stats
			updatedText := f.Dashboard.GetUsagePanelText()
			assert.NotEmpty(t, updatedText, "usage panel should still have content after events")
			assert.Contains(t, updatedText, "B", "usage panel should still show memory stats")
		})
	})
}
