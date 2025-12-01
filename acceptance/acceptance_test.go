//go:build acceptance
// +build acceptance

package acceptance

import (
	"log"
	"os"
	"testing"

	"github.com/playwright-community/playwright-go"
)

// TestMain installs Playwright browsers before running tests.
func TestMain(m *testing.M) {
	if err := playwright.Install(); err != nil {
		log.Fatalf("could not install playwright: %v", err)
	}
	os.Exit(m.Run())
}
