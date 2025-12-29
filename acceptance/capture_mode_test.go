package acceptance

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGlobalMode tests capture behavior in global mode.
func TestGlobalMode(t *testing.T) {
	t.Parallel()

	t.Run("same context", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			f.Dashboard.StartCapture("global")

			// Make a request from the same browser context
			f.Dashboard.FetchAPI("/api/test")

			f.Dashboard.WaitForEventCount(1, 5000)
			assert.Equal(t, 1, f.Dashboard.GetEventCount())
		})
	})

	t.Run("different context", func(t *testing.T) {
		t.Parallel()
		WithTestApp(t, func(t *testing.T, app *TestApp, pw *PlaywrightFixture) {
			ctx1 := pw.NewContext(t)
			t.Cleanup(func() { ctx1.Close() })

			dashboard := NewDashboardPage(t, ctx1, app.DevlogURL)
			dashboard.StartCapture("global")

			// Make request from a completely different browser context (no cookies)
			ctx2 := pw.NewContext(t)
			t.Cleanup(func() { ctx2.Close() })

			page2, err := ctx2.NewPage()
			require.NoError(t, err)

			_, err = page2.Goto(app.AppURL + "/api/test")
			require.NoError(t, err)

			// In global mode, this should still be captured
			dashboard.WaitForEventCount(1, 5000)
			assert.Equal(t, 1, dashboard.GetEventCount())
		})
	})

	t.Run("reload persistence", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			f.Dashboard.StartCapture("global")

			// Make a request
			f.Dashboard.FetchAPI("/api/test")

			f.Dashboard.WaitForEventCount(1, 5000)
			assert.Equal(t, 1, f.Dashboard.GetEventCount())

			// Reload the page
			f.Dashboard.Reload()

			// Events should persist after reload
			assert.Equal(t, 1, f.Dashboard.GetEventCount())
		})
	})

	t.Run("clear events", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			f.Dashboard.StartCapture("global")

			// Create some events
			for i := 0; i < 3; i++ {
				_, err := f.Dashboard.Page.Evaluate(fmt.Sprintf(`fetch('/api/test?i=%d')`, i))
				require.NoError(t, err)
			}

			f.Dashboard.WaitForEventCount(3, 5000)
			assert.Equal(t, 3, f.Dashboard.GetEventCount())

			// Clear events
			f.Dashboard.ClearEvents()

			assert.Equal(t, 0, f.Dashboard.GetEventCount())
		})
	})
}

// TestSessionMode tests capture behavior in session mode.
func TestSessionMode(t *testing.T) {
	t.Parallel()

	t.Run("same context", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			f.Dashboard.StartCapture("session")

			// Make a request from the same browser context (has session cookie)
			f.Dashboard.FetchAPI("/api/test")

			f.Dashboard.WaitForEventCount(1, 5000)
			assert.Equal(t, 1, f.Dashboard.GetEventCount())
		})
	})

	t.Run("different context isolation", func(t *testing.T) {
		t.Parallel()
		WithTestApp(t, func(t *testing.T, app *TestApp, pw *PlaywrightFixture) {
			ctx1 := pw.NewContext(t)
			t.Cleanup(func() { ctx1.Close() })

			dashboard := NewDashboardPage(t, ctx1, app.DevlogURL)
			dashboard.StartCapture("session")

			// Make request from different browser context (no session cookie)
			ctx2 := pw.NewContext(t)
			t.Cleanup(func() { ctx2.Close() })

			page2, err := ctx2.NewPage()
			require.NoError(t, err)

			_, err = page2.Goto(app.AppURL + "/api/test")
			require.NoError(t, err)

			// In session mode, this should NOT be captured (no cookie)
			dashboard.ExpectNoEvents(2 * time.Second)
		})
	})

	t.Run("reload persistence", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			f.Dashboard.StartCapture("session")

			// Make first request
			f.Dashboard.FetchAPI("/api/test?n=1")

			f.Dashboard.WaitForEventCount(1, 5000)
			assert.Equal(t, 1, f.Dashboard.GetEventCount())

			// Reload page
			f.Dashboard.Reload()

			// Old events should persist
			assert.Equal(t, 1, f.Dashboard.GetEventCount())

			// Make another request after reload - should still capture
			f.Dashboard.FetchAPI("/api/test?n=2")

			f.Dashboard.WaitForEventCount(2, 5000)
			assert.Equal(t, 2, f.Dashboard.GetEventCount())
		})
	})

	t.Run("clear events", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			f.Dashboard.StartCapture("session")

			// Create some events
			for i := 0; i < 3; i++ {
				_, err := f.Dashboard.Page.Evaluate(fmt.Sprintf(`fetch('/api/test?i=%d')`, i))
				require.NoError(t, err)
			}

			f.Dashboard.WaitForEventCount(3, 5000)
			assert.Equal(t, 3, f.Dashboard.GetEventCount())

			// Clear events
			f.Dashboard.ClearEvents()

			assert.Equal(t, 0, f.Dashboard.GetEventCount())
		})
	})
}

// TestModeSwitching tests switching between session and global modes.
func TestModeSwitching(t *testing.T) {
	t.Parallel()

	t.Run("session to global", func(t *testing.T) {
		t.Parallel()
		WithTestApp(t, func(t *testing.T, app *TestApp, pw *PlaywrightFixture) {
			ctx1 := pw.NewContext(t)
			t.Cleanup(func() { ctx1.Close() })

			dashboard := NewDashboardPage(t, ctx1, app.DevlogURL)
			dashboard.StartCapture("session")

			// Create a second context for external requests
			ctx2 := pw.NewContext(t)
			t.Cleanup(func() { ctx2.Close() })

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
		})
	})

	t.Run("global to session", func(t *testing.T) {
		t.Parallel()
		WithTestApp(t, func(t *testing.T, app *TestApp, pw *PlaywrightFixture) {
			ctx1 := pw.NewContext(t)
			t.Cleanup(func() { ctx1.Close() })

			dashboard := NewDashboardPage(t, ctx1, app.DevlogURL)
			dashboard.StartCapture("global")

			// Create a second context for external requests
			ctx2 := pw.NewContext(t)
			t.Cleanup(func() { ctx2.Close() })

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
		})
	})
}
