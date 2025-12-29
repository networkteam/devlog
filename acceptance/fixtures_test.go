package acceptance

import (
	"testing"

	"github.com/playwright-community/playwright-go"
)

// TestFixtures bundles all commonly needed test fixtures.
type TestFixtures struct {
	App       *TestApp
	PW        *PlaywrightFixture
	Ctx       playwright.BrowserContext
	Dashboard *DashboardPage
}

// WithTestFixtures creates all fixtures, registers cleanup with t.Cleanup(), and calls the test function.
// This reduces boilerplate in tests by handling the common setup pattern.
func WithTestFixtures(t *testing.T, fn func(t *testing.T, f *TestFixtures)) {
	t.Helper()

	app := NewTestApp(t)
	t.Cleanup(func() { app.Close() })

	pw := NewPlaywrightFixture(t)
	t.Cleanup(func() { pw.Close() })

	ctx := pw.NewContext(t)
	t.Cleanup(func() { ctx.Close() })

	dashboard := NewDashboardPage(t, ctx, app.DevlogURL)

	fn(t, &TestFixtures{
		App:       app,
		PW:        pw,
		Ctx:       ctx,
		Dashboard: dashboard,
	})
}

// WithTestApp creates only the test app fixture (useful when custom browser setup is needed).
// The callback receives the test app and playwright fixture, allowing custom context creation.
func WithTestApp(t *testing.T, fn func(t *testing.T, app *TestApp, pw *PlaywrightFixture)) {
	t.Helper()

	app := NewTestApp(t)
	t.Cleanup(func() { app.Close() })

	pw := NewPlaywrightFixture(t)
	t.Cleanup(func() { pw.Close() })

	fn(t, app, pw)
}
