//go:build acceptance
// +build acceptance

package acceptance

import (
	"os"
	"testing"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

// PlaywrightFixture manages Playwright browser instances for tests.
type PlaywrightFixture struct {
	PW      *playwright.Playwright
	Browser playwright.Browser
}

// NewPlaywrightFixture creates a new Playwright fixture with a Chromium browser.
// Set HEADLESS=false environment variable to run with visible browser for debugging.
func NewPlaywrightFixture(t *testing.T) *PlaywrightFixture {
	t.Helper()

	pw, err := playwright.Run()
	require.NoError(t, err, "failed to start playwright")

	headless := os.Getenv("HEADLESS") != "false"
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(headless),
	})
	require.NoError(t, err, "failed to launch browser")

	return &PlaywrightFixture{PW: pw, Browser: browser}
}

// NewContext creates a new browser context with isolated cookies and storage.
// Each context is independent, useful for testing session isolation.
func (pf *PlaywrightFixture) NewContext(t *testing.T) playwright.BrowserContext {
	t.Helper()
	ctx, err := pf.Browser.NewContext()
	require.NoError(t, err, "failed to create browser context")
	return ctx
}

// Close releases all Playwright resources.
func (pf *PlaywrightFixture) Close() {
	pf.Browser.Close()
	pf.PW.Stop()
}
