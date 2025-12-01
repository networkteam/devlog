//go:build acceptance
// +build acceptance

package acceptance

import (
	"fmt"
	"testing"
	"time"

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
		time.Sleep(200 * time.Millisecond)
	}

	// Click record button
	recordBtn := dp.Page.Locator("button[title='Start capture']")
	err := recordBtn.Click()
	require.NoError(dp.t, err, "failed to click record button")

	// Wait for SSE connection to be established
	err = dp.Page.Locator("#event-list[sse-connect]").WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateAttached,
		Timeout: playwright.Float(5000),
	})
	require.NoError(dp.t, err, "failed to establish SSE connection")

	// Give a moment for connection to stabilize
	time.Sleep(300 * time.Millisecond)
}

// StopCapture stops capturing events.
func (dp *DashboardPage) StopCapture() {
	dp.t.Helper()

	stopBtn := dp.Page.Locator("button[title='Stop capture']")
	err := stopBtn.Click()
	require.NoError(dp.t, err, "failed to click stop button")

	time.Sleep(300 * time.Millisecond)
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
		// For zero events, just wait a bit and check
		time.Sleep(time.Duration(timeout/2) * time.Millisecond)
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

	time.Sleep(500 * time.Millisecond)
}

// SwitchMode switches the capture mode ("session" or "global") while capturing.
func (dp *DashboardPage) SwitchMode(mode string) {
	dp.t.Helper()

	var btn playwright.Locator
	if mode == "session" {
		btn = dp.Page.Locator("button:has-text('Session')")
	} else {
		btn = dp.Page.Locator("button:has-text('Global')")
	}

	err := btn.Click()
	require.NoError(dp.t, err, "failed to switch to %s mode", mode)

	time.Sleep(300 * time.Millisecond)
}

// ClickFirstEvent clicks on the first event in the event list to show its details.
func (dp *DashboardPage) ClickFirstEvent() {
	dp.t.Helper()

	firstEvent := dp.Page.Locator("#event-list > li:first-child")
	err := firstEvent.Click()
	require.NoError(dp.t, err, "failed to click first event")

	time.Sleep(300 * time.Millisecond)
}

// Reload reloads the current page.
func (dp *DashboardPage) Reload() {
	dp.t.Helper()

	_, err := dp.Page.Reload()
	require.NoError(dp.t, err, "failed to reload page")

	err = dp.Page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateDomcontentloaded,
	})
	require.NoError(dp.t, err, "failed to wait for page load")

	time.Sleep(500 * time.Millisecond)
}

// GetEventDetailsText returns the text content of the event details panel.
func (dp *DashboardPage) GetEventDetailsText() string {
	dp.t.Helper()

	details := dp.Page.Locator("#event-details")
	text, err := details.TextContent()
	require.NoError(dp.t, err)
	return text
}

// GetFirstEventText returns the text content of the first event in the list.
func (dp *DashboardPage) GetFirstEventText() string {
	dp.t.Helper()

	firstEvent := dp.Page.Locator("#event-list > li:first-child")
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

// FetchAPI executes a fetch request from the browser context.
func (dp *DashboardPage) FetchAPI(path string) {
	dp.t.Helper()

	_, err := dp.Page.Evaluate(fmt.Sprintf(`fetch('%s')`, path))
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
