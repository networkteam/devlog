//go:build acceptance
// +build acceptance

package acceptance

import (
	"fmt"
	"testing"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

// DashboardPage provides helper methods for interacting with the devlog dashboard.
// It implements the Page Object pattern for cleaner test code.
type DashboardPage struct {
	Page       playwright.Page
	DevlogURL  string
	SessionURL string
	t          *testing.T
}

// NewDashboardPage navigates to the devlog dashboard and waits for redirect to session URL.
func NewDashboardPage(t *testing.T, ctx playwright.BrowserContext, devlogURL string) *DashboardPage {
	t.Helper()

	page, err := ctx.NewPage()
	require.NoError(t, err)

	_, err = page.Goto(devlogURL)
	require.NoError(t, err)

	// Wait for redirect to session URL
	err = page.WaitForURL("**/_devlog/s/*/", playwright.PageWaitForURLOptions{
		Timeout: playwright.Float(10000),
	})
	require.NoError(t, err, "failed to redirect to session URL")

	return &DashboardPage{
		Page:       page,
		DevlogURL:  devlogURL,
		SessionURL: page.URL(),
		t:          t,
	}
}

// StartCapture starts capturing events in the specified mode ("session" or "global").
func (dp *DashboardPage) StartCapture(mode string) {
	dp.t.Helper()

	// If mode is global, click the Global button first (before capturing)
	if mode == "global" {
		globalBtn := dp.Page.Locator("button:has-text('Global')")
		err := globalBtn.Click()
		require.NoError(dp.t, err, "failed to click Global button")

		// Wait for mode to be set in data attribute
		err = dp.Page.Locator("#capture-controls[data-mode='global']").WaitFor(playwright.LocatorWaitForOptions{
			State:   playwright.WaitForSelectorStateAttached,
			Timeout: playwright.Float(5000),
		})
		require.NoError(dp.t, err, "failed to switch to global mode")
	}

	// Set up a promise that resolves when htmx:sseOpen fires (SSE connection established)
	// We set this up BEFORE clicking record so we don't miss the event
	_, err := dp.Page.Evaluate(`() => {
		window._sseOpenPromise = new Promise((resolve) => {
			document.body.addEventListener('htmx:sseOpen', () => {
				resolve(true);
			}, { once: true });
		});
	}`)
	require.NoError(dp.t, err, "failed to set up SSE open listener")

	// Click record button
	recordBtn := dp.Page.Locator("button[title='Start capture']")
	err = recordBtn.Click()
	require.NoError(dp.t, err, "failed to click record button")

	// Wait for the placeholder text to disappear (indicates capture UI has loaded)
	err = dp.Page.Locator("text=Click Record to start capturing events").WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateHidden,
		Timeout: playwright.Float(5000),
	})
	require.NoError(dp.t, err, "capture UI did not load")

	// Wait for SSE connection to actually be open (not just attribute present)
	_, err = dp.Page.WaitForFunction(`() => window._sseOpenPromise`, playwright.PageWaitForFunctionOptions{
		Timeout: playwright.Float(5000),
	})
	require.NoError(dp.t, err, "failed to establish SSE connection")
}

// StopCapture stops capturing events.
func (dp *DashboardPage) StopCapture() {
	dp.t.Helper()

	stopBtn := dp.Page.Locator("button[title='Stop capture']")
	err := stopBtn.Click()
	require.NoError(dp.t, err, "failed to click stop button")

	// Wait for the record button to become enabled (indicates capture stopped)
	err = dp.Page.Locator("button[title='Start capture']:not([disabled])").WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(5000),
	})
	require.NoError(dp.t, err, "capture did not stop")
}

// GetEventCount returns the number of events currently shown in the event list.
func (dp *DashboardPage) GetEventCount() int {
	dp.t.Helper()

	count, err := dp.Page.Locator("#event-list > li").Count()
	require.NoError(dp.t, err)
	return count
}

// WaitForEventCount waits for the specified number of events to appear.
func (dp *DashboardPage) WaitForEventCount(expectedCount int, timeout float64) {
	dp.t.Helper()

	if expectedCount == 0 {
		// For zero events, wait for list to be empty
		err := dp.Page.Locator("#event-list:empty, #event-list:not(:has(> li))").WaitFor(playwright.LocatorWaitForOptions{
			State:   playwright.WaitForSelectorStateAttached,
			Timeout: playwright.Float(timeout),
		})
		// Don't fail on timeout for zero - just return and let caller check
		if err != nil {
			dp.t.Logf("Note: timeout waiting for empty event list (may be expected)")
		}
		return
	}

	selector := fmt.Sprintf("#event-list > li:nth-child(%d)", expectedCount)
	err := dp.Page.Locator(selector).WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateAttached,
		Timeout: playwright.Float(timeout),
	})
	require.NoError(dp.t, err, "failed to wait for %d events", expectedCount)
}

// ClearEvents clears all events from the event list.
func (dp *DashboardPage) ClearEvents() {
	dp.t.Helper()

	clearBtn := dp.Page.Locator("button[title='Clear list']")
	err := clearBtn.Click()
	require.NoError(dp.t, err, "failed to click clear button")

	// Wait for event list to be empty (HTMX swaps the content)
	err = dp.Page.Locator("#event-list:not(:has(> li))").WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateAttached,
		Timeout: playwright.Float(5000),
	})
	require.NoError(dp.t, err, "failed to wait for clear to complete")
}

// SwitchMode switches the capture mode ("session" or "global") while capturing.
func (dp *DashboardPage) SwitchMode(mode string) {
	dp.t.Helper()

	// Set up a promise that resolves when htmx:sseOpen fires (SSE connection established)
	// We set this up BEFORE clicking so we don't miss the event
	_, err := dp.Page.Evaluate(`() => {
		window._sseOpenPromise = new Promise((resolve) => {
			document.body.addEventListener('htmx:sseOpen', () => {
				resolve(true);
			}, { once: true });
		});
	}`)
	require.NoError(dp.t, err, "failed to set up SSE open listener")

	var btn playwright.Locator
	if mode == "session" {
		btn = dp.Page.Locator("button:has-text('Session')")
	} else {
		btn = dp.Page.Locator("button:has-text('Global')")
	}

	err = btn.Click()
	require.NoError(dp.t, err, "failed to switch to %s mode", mode)

	// Wait for mode to be updated in the capture controls data attribute
	selector := fmt.Sprintf("#capture-controls[data-mode='%s']", mode)
	err = dp.Page.Locator(selector).WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateAttached,
		Timeout: playwright.Float(5000),
	})
	require.NoError(dp.t, err, "failed to confirm mode switch to %s", mode)

	// Wait for SSE connection to actually be open (not just attribute present)
	_, err = dp.Page.WaitForFunction(`() => window._sseOpenPromise`, playwright.PageWaitForFunctionOptions{
		Timeout: playwright.Float(5000),
	})
	require.NoError(dp.t, err, "failed to establish SSE connection for mode %s", mode)
}

// ClickFirstEvent clicks on the first parent event in the event list to show its details.
// This specifically targets the parent HTTP event div, not nested child events.
func (dp *DashboardPage) ClickFirstEvent() {
	dp.t.Helper()

	// Target the parent event's div directly (first div child of first li)
	firstEvent := dp.Page.Locator("#event-list > li:first-child > div[id$='-item']")
	err := firstEvent.Click()
	require.NoError(dp.t, err, "failed to click first event")

	// Wait for event details to load
	dp.WaitForEventDetails(5000)
}

// ClickFirstChildEvent clicks on the first child event (nested inside a parent event).
func (dp *DashboardPage) ClickFirstChildEvent() {
	dp.t.Helper()

	// Target child events inside the nested ul
	childEvent := dp.Page.Locator("#event-list > li:first-child ul div[id$='-item']").First()
	err := childEvent.Click()
	require.NoError(dp.t, err, "failed to click first child event")

	// Wait for event details to load
	dp.WaitForEventDetails(5000)
}

// Reload reloads the current page.
func (dp *DashboardPage) Reload() {
	dp.t.Helper()

	_, err := dp.Page.Reload()
	require.NoError(dp.t, err, "failed to reload page")

	// Wait for DOM to be loaded
	err = dp.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateDomcontentloaded,
	})
	require.NoError(dp.t, err, "failed to wait for DOM load")

	// Wait for capture controls to be present (indicates page is ready)
	err = dp.Page.Locator("#capture-controls").WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(5000),
	})
	require.NoError(dp.t, err, "failed to wait for page load")
}

// GetEventDetailsText returns the text content of the event details panel.
func (dp *DashboardPage) GetEventDetailsText() string {
	dp.t.Helper()

	details := dp.Page.Locator("#event-details")
	text, err := details.TextContent()
	require.NoError(dp.t, err)
	return text
}

// GetFirstEventText returns the text content of the first parent event in the list.
// This specifically targets the parent HTTP event div, not nested child events.
func (dp *DashboardPage) GetFirstEventText() string {
	dp.t.Helper()

	firstEvent := dp.Page.Locator("#event-list > li:first-child > div[id$='-item']")
	text, err := firstEvent.TextContent()
	require.NoError(dp.t, err)
	return text
}

// WaitForEventDetails waits for the event details panel to show content.
func (dp *DashboardPage) WaitForEventDetails(timeout float64) {
	dp.t.Helper()

	detailsHeader := dp.Page.Locator("#event-details h2")
	err := detailsHeader.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(timeout),
	})
	require.NoError(dp.t, err, "failed to wait for event details")
}

// FetchAPI executes a fetch request from the browser context and waits for the response.
func (dp *DashboardPage) FetchAPI(path string) {
	dp.t.Helper()

	// Wait for the fetch to complete fully (read the response body)
	_, err := dp.Page.Evaluate(fmt.Sprintf(`fetch('%s').then(r => r.text())`, path))
	require.NoError(dp.t, err)
}

// FetchAPIWithBody executes a POST fetch request with JSON body from the browser context.
func (dp *DashboardPage) FetchAPIWithBody(path string, body string) {
	dp.t.Helper()

	js := fmt.Sprintf(`fetch('%s', {
		method: 'POST',
		headers: {'Content-Type': 'application/json'},
		body: JSON.stringify(%s)
	})`, path, body)

	_, err := dp.Page.Evaluate(js)
	require.NoError(dp.t, err)
}
