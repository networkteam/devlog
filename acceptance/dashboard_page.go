package acceptance

import (
	"fmt"
	"net/url"
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

// waitForSSEConnection waits for the SSE connection on #event-list to be open.
// It polls the htmx internal data to check the EventSource readyState.
func (dp *DashboardPage) waitForSSEConnection(timeout float64) {
	dp.t.Helper()

	_, err := dp.Page.WaitForFunction(`() => {
		const el = document.getElementById('event-list');
		const internalData = el && el['htmx-internal-data'];
		if (internalData && internalData.sseEventSource) {
			return internalData.sseEventSource.readyState === 1; // OPEN
		}
		return false;
	}`, playwright.PageWaitForFunctionOptions{
		Timeout: playwright.Float(timeout),
		Polling: playwright.Float(100),
	})
	require.NoError(dp.t, err, "failed to establish SSE connection")
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

	// Click record button
	recordBtn := dp.Page.Locator("button[title='Start capture']")
	err := recordBtn.Click()
	require.NoError(dp.t, err, "failed to click record button")

	// Wait for the placeholder text to disappear (indicates capture UI has loaded)
	err = dp.Page.Locator("text=Click Record to start capturing events").WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateHidden,
		Timeout: playwright.Float(5000),
	})
	require.NoError(dp.t, err, "capture UI did not load")

	dp.waitForSSEConnection(5000)
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

// ExpectDuring repeatedly checks that the assertion function returns true
// for the entire duration. Polls every interval. Fails immediately if
// the assertion returns false at any point.
// This is useful for negative assertions where we want to verify something
// does NOT happen over a period of time.
func (dp *DashboardPage) ExpectDuring(assertion func() bool, interval time.Duration, duration time.Duration, msgAndArgs ...interface{}) {
	dp.t.Helper()

	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		if !assertion() {
			if len(msgAndArgs) > 0 {
				dp.t.Fatalf("assertion failed during wait: %v", msgAndArgs...)
			} else {
				dp.t.Fatal("assertion failed during wait")
			}
		}
		time.Sleep(interval)
	}

	// Final check
	if !assertion() {
		if len(msgAndArgs) > 0 {
			dp.t.Fatalf("assertion failed at end of wait: %v", msgAndArgs...)
		} else {
			dp.t.Fatal("assertion failed at end of wait")
		}
	}
}

// ExpectNoEvents verifies that no events appear during the given duration.
// Polls every 100ms and fails immediately if any event appears.
func (dp *DashboardPage) ExpectNoEvents(duration time.Duration) {
	dp.t.Helper()

	dp.ExpectDuring(func() bool {
		return dp.GetEventCount() == 0
	}, 100*time.Millisecond, duration, "expected no events but some appeared")
}

// ExpectEventCountStable verifies that the event count stays at the expected value
// for the given duration. Fails immediately if the count changes.
func (dp *DashboardPage) ExpectEventCountStable(expectedCount int, duration time.Duration) {
	dp.t.Helper()

	dp.ExpectDuring(func() bool {
		return dp.GetEventCount() == expectedCount
	}, 100*time.Millisecond, duration, "expected %d events to remain stable", expectedCount)
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

	var btn playwright.Locator
	if mode == "session" {
		btn = dp.Page.Locator("button:has-text('Session')")
	} else {
		btn = dp.Page.Locator("button:has-text('Global')")
	}

	err := btn.Click()
	require.NoError(dp.t, err, "failed to switch to %s mode", mode)

	// Wait for mode to be updated in the capture controls data attribute
	selector := fmt.Sprintf("#capture-controls[data-mode='%s']", mode)
	err = dp.Page.Locator(selector).WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateAttached,
		Timeout: playwright.Float(5000),
	})
	require.NoError(dp.t, err, "failed to confirm mode switch to %s", mode)

	dp.waitForSSEConnection(5000)
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

// Reload reloads the current page and waits for SSE to reconnect if capture was active.
func (dp *DashboardPage) Reload() {
	dp.t.Helper()

	// Check if capture is currently active (before reload)
	stopBtn := dp.Page.Locator("button[title='Stop capture']:not([disabled])")
	wasCapturing, _ := stopBtn.IsVisible()

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

	// If capture was active, wait for SSE to be connected
	if wasCapturing {
		// Wait for event list with SSE to be present
		err = dp.Page.Locator("ul#event-list[sse-connect]").WaitFor(playwright.LocatorWaitForOptions{
			State:   playwright.WaitForSelectorStateAttached,
			Timeout: playwright.Float(5000),
		})
		require.NoError(dp.t, err, "failed to find SSE element after reload")

		dp.waitForSSEConnection(5000)
	}
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

// FetchAPI executes a GET request using Playwright's API request context (shares browser cookies).
func (dp *DashboardPage) FetchAPI(path string) {
	dp.t.Helper()

	_, err := dp.Page.Request().Get(dp.BaseURL() + path)
	require.NoError(dp.t, err)
}

// FetchAPIWithBody executes a POST request with JSON body using Playwright's API request context.
func (dp *DashboardPage) FetchAPIWithBody(path string, body string) {
	dp.t.Helper()

	_, err := dp.Page.Request().Post(dp.BaseURL()+path, playwright.APIRequestContextPostOptions{
		Headers: map[string]string{"Content-Type": "application/json"},
		Data:    body,
	})
	require.NoError(dp.t, err)
}

// GetUsagePanelText returns the text content of the usage panel.
func (dp *DashboardPage) GetUsagePanelText() string {
	dp.t.Helper()

	usagePanel := dp.Page.Locator("#usage-panel")
	text, err := usagePanel.TextContent()
	require.NoError(dp.t, err)
	return text
}

// WaitForUsagePanel waits for the usage panel content to load (not show "Loading...").
func (dp *DashboardPage) WaitForUsagePanel(timeout float64) {
	dp.t.Helper()

	// Wait for the usage panel to contain actual content (not "Loading...")
	_, err := dp.Page.WaitForFunction(`() => {
		const el = document.getElementById('usage-panel');
		return el && !el.textContent.includes('Loading');
	}`, playwright.PageWaitForFunctionOptions{
		Timeout: playwright.Float(timeout),
		Polling: playwright.Float(100),
	})
	require.NoError(dp.t, err, "usage panel did not load content")
}

// BaseURL returns the origin (scheme://host) from the session URL.
func (dp *DashboardPage) BaseURL() string {
	dp.t.Helper()

	parsed, err := url.Parse(dp.SessionURL)
	require.NoError(dp.t, err)
	return fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
}

// DownloadRequestBody finds the "Download" link in the Request Body section and fetches the content.
// Returns the download URL path (for verification) and the response body content.
func (dp *DashboardPage) DownloadRequestBody() (path string, body []byte, contentType string) {
	dp.t.Helper()

	// Find the Request section's Body download link
	// Structure: div with h3 "Request" > div with h4 "Body" > a[download] "Download"
	link := dp.Page.Locator("#event-details").Locator("h3:has-text('Request')").Locator("..").Locator("h4:has-text('Body')").Locator("..").Locator("a[download]:has-text('Download')")

	href, err := link.GetAttribute("href")
	require.NoError(dp.t, err, "failed to get request body download link href")
	require.NotEmpty(dp.t, href, "request body download link should have href")

	// Use Playwright's native API request context (shares browser cookies/context)
	fullURL := dp.BaseURL() + href
	response, err := dp.Page.Request().Get(fullURL)
	require.NoError(dp.t, err, "failed to fetch request body download")
	require.Equal(dp.t, 200, response.Status(), "request body download should return 200")

	bodyBytes, err := response.Body()
	require.NoError(dp.t, err, "failed to read request body")

	headers := response.Headers()
	ct := headers["content-type"]

	return href, bodyBytes, ct
}

// DownloadResponseBody finds the "Download" link in the Response Body section and fetches the content.
// Returns the download URL path (for verification) and the response body content.
func (dp *DashboardPage) DownloadResponseBody() (path string, body []byte, contentType string) {
	dp.t.Helper()

	// Find the Response section's Body download link
	// Structure: div with h3 "Response" > div with h4 "Body" > a[download] "Download"
	link := dp.Page.Locator("#event-details").Locator("h3:has-text('Response')").Locator("..").Locator("h4:has-text('Body')").Locator("..").Locator("a[download]:has-text('Download')")

	href, err := link.GetAttribute("href")
	require.NoError(dp.t, err, "failed to get response body download link href")
	require.NotEmpty(dp.t, href, "response body download link should have href")

	// Use Playwright's native API request context (shares browser cookies/context)
	fullURL := dp.BaseURL() + href
	response, err := dp.Page.Request().Get(fullURL)
	require.NoError(dp.t, err, "failed to fetch response body download")
	require.Equal(dp.t, 200, response.Status(), "response body download should return 200")

	bodyBytes, err := response.Body()
	require.NoError(dp.t, err, "failed to read response body")

	headers := response.Headers()
	ct := headers["content-type"]

	return href, bodyBytes, ct
}
