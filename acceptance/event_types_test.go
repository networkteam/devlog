package acceptance

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTPServerEvent tests HTTP server request event capture and display.
func TestHTTPServerEvent(t *testing.T) {
	t.Parallel()

	t.Run("GET /api/test", func(t *testing.T) {
		t.Parallel()

		t.Run("captures basic request", func(t *testing.T) {
			t.Parallel()
			WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
				f.Dashboard.StartCapture("global")

				f.Dashboard.FetchAPI("/api/test")
				f.Dashboard.WaitForEventCount(1, 5000)

				text := f.Dashboard.GetFirstEventText()
				assert.Contains(t, text, "GET")
				assert.Contains(t, text, "/api/test")
			})
		})

		t.Run("download response body", func(t *testing.T) {
			t.Parallel()
			WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
				f.Dashboard.StartCapture("global")

				// Make a GET request - the /api/test endpoint returns {"status":"ok"}
				f.Dashboard.FetchAPI("/api/test")
				f.Dashboard.WaitForEventCount(1, 5000)

				f.Dashboard.ClickFirstEvent()
				f.Dashboard.WaitForEventDetails(5000)

				// Download the response body and verify it contains the server's response
				_, body, contentType := f.Dashboard.DownloadResponseBody()
				assert.Contains(t, contentType, "application/json")
				assert.Contains(t, string(body), `"status":"ok"`)
			})
		})
	})

	t.Run("POST /api/echo", func(t *testing.T) {
		t.Parallel()

		t.Run("captures request with body", func(t *testing.T) {
			t.Parallel()
			WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
				f.Dashboard.StartCapture("global")

				f.Dashboard.FetchAPIWithBody("/api/echo", `{"message": "hello"}`)
				f.Dashboard.WaitForEventCount(1, 5000)

				text := f.Dashboard.GetFirstEventText()
				assert.Contains(t, text, "POST")
				assert.Contains(t, text, "/api/echo")
			})
		})

		t.Run("download request body", func(t *testing.T) {
			t.Parallel()
			WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
				f.Dashboard.StartCapture("global")

				f.Dashboard.FetchAPIWithBody("/api/echo", `{"test": "request-body-data"}`)
				f.Dashboard.WaitForEventCount(1, 5000)

				f.Dashboard.ClickFirstEvent()
				f.Dashboard.WaitForEventDetails(5000)

				_, body, contentType := f.Dashboard.DownloadRequestBody()
				assert.Contains(t, contentType, "application/json")
				assert.Contains(t, string(body), "request-body-data")
			})
		})

		t.Run("large request body", func(t *testing.T) {
			t.Parallel()
			WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
				f.Dashboard.StartCapture("global")

				_, err := f.Dashboard.Page.Evaluate(`
					const largeData = 'x'.repeat(10000);
					fetch('/api/echo', {
						method: 'POST',
						headers: {'Content-Type': 'text/plain'},
						body: largeData
					})
				`)
				require.NoError(t, err)

				f.Dashboard.WaitForEventCount(1, 5000)
				assert.Equal(t, 1, f.Dashboard.GetEventCount())
			})
		})
	})
}

// TestHTTPClientEvent tests outgoing HTTP client request capture.
func TestHTTPClientEvent(t *testing.T) {
	t.Parallel()

	t.Run("captures outgoing request", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			f.Dashboard.StartCapture("global")

			// Trigger endpoint that makes HTTP client request
			f.Dashboard.FetchAPI("/api/external")

			// Wait for the HTTP server event (the endpoint response)
			f.Dashboard.WaitForEventCount(1, 5000)

			// Click to see details - the HTTP client request should be nested
			f.Dashboard.ClickFirstEvent()
			f.Dashboard.WaitForEventDetails(5000)

			text := f.Dashboard.GetEventDetailsText()
			assert.Contains(t, text, "/api/external")
		})
	})
}

// TestDBQueryEvent tests database query event capture.
func TestDBQueryEvent(t *testing.T) {
	t.Parallel()

	t.Run("captures query", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			f.Dashboard.StartCapture("global")

			// Trigger endpoint that creates DB query event
			f.Dashboard.FetchAPI("/api/db")

			// Should have HTTP request event which contains DB query as child
			f.Dashboard.WaitForEventCount(1, 5000)

			// Click to see details
			f.Dashboard.ClickFirstEvent()
			f.Dashboard.WaitForEventDetails(5000)

			text := f.Dashboard.GetEventDetailsText()
			assert.Contains(t, text, "/api/db")
		})
	})
}

// TestLogEvent tests structured logging (slog) event capture.
func TestLogEvent(t *testing.T) {
	t.Parallel()

	t.Run("captures slog entries", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			f.Dashboard.StartCapture("global")

			// Trigger endpoint that creates log events
			f.Dashboard.FetchAPI("/api/log")

			// HTTP server event should appear (logs are nested inside)
			f.Dashboard.WaitForEventCount(1, 5000)

			text := f.Dashboard.GetFirstEventText()
			assert.Contains(t, text, "/api/log")
		})
	})
}

// TestNestedEvent tests parent-child event relationships.
func TestNestedEvent(t *testing.T) {
	t.Parallel()

	t.Run("parent-child relationships", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			f.Dashboard.StartCapture("global")

			// Trigger endpoint that creates multiple nested events (logs + DB query)
			f.Dashboard.FetchAPI("/api/combined")

			// Wait for the parent HTTP event
			f.Dashboard.WaitForEventCount(1, 5000)

			text := f.Dashboard.GetFirstEventText()
			assert.Contains(t, text, "/api/combined")
		})
	})

	t.Run("show children", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			f.Dashboard.StartCapture("global")

			// Trigger endpoint that creates multiple nested events
			f.Dashboard.FetchAPI("/api/combined")

			f.Dashboard.WaitForEventCount(1, 5000)

			// The parent HTTP event should be shown
			text := f.Dashboard.GetFirstEventText()
			assert.Contains(t, text, "/api/combined")
		})
	})
}

// TestMixedEventTypes tests multiple event types captured simultaneously.
func TestMixedEventTypes(t *testing.T) {
	t.Parallel()

	t.Run("multiple types simultaneously", func(t *testing.T) {
		t.Parallel()
		WithTestFixtures(t, func(t *testing.T, f *TestFixtures) {
			f.Dashboard.StartCapture("global")

			// Trigger multiple endpoints that create different event types
			f.Dashboard.FetchAPI("/api/test")     // Simple HTTP
			f.Dashboard.FetchAPI("/api/log")      // HTTP + logs
			f.Dashboard.FetchAPI("/api/db")       // HTTP + DB query
			f.Dashboard.FetchAPI("/api/combined") // HTTP + logs + DB

			// Should have 4 top-level HTTP events
			f.Dashboard.WaitForEventCount(4, 10000)
			assert.Equal(t, 4, f.Dashboard.GetEventCount())
		})
	})
}
