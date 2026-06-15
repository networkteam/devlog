package main

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/networkteam/devlog"
	"github.com/networkteam/devlog/collector"
	"github.com/networkteam/devlog/dashboard"
)

// newTestDevlog starts an httptest server hosting a collected app plus the devlog
// dashboard with the agent API enabled. It returns the dashboard base URL (the
// --direct target) and the app URL whose traffic is captured.
func newTestDevlog(t *testing.T) (baseURL, appURL string) {
	return newTestDevlogWith(t)
}

func newTestDevlogWith(t *testing.T, agentOpts ...dashboard.HandlerOption) (baseURL, appURL string) {
	t.Helper()

	dlog := devlog.NewWithOptions(devlog.Options{
		HTTPServerOptions: &collector.HTTPServerOptions{
			CaptureRequestBody:  true,
			CaptureResponseBody: true,
			MaxBodySize:         1 << 20,
		},
	})
	t.Cleanup(dlog.Close)

	app := dlog.CollectHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	opts := append([]dashboard.HandlerOption{dashboard.WithAgentAPI()}, agentOpts...)
	outer := http.NewServeMux()
	outer.Handle("/_devlog/", http.StripPrefix("/_devlog", dlog.DashboardHandler("/_devlog", opts...)))
	outer.Handle("/", app)

	srv := httptest.NewServer(outer)
	t.Cleanup(srv.Close)

	return srv.URL + "/_devlog", srv.URL + "/"
}

// startSession simulates a dashboard browser tab creating a capture session via
// the dashboard's own capture/start endpoint, returning the session id the relay
// attaches to. (The agent API has no start endpoint — capture is user-managed.)
func startSession(t *testing.T, baseURL, mode string) string {
	t.Helper()
	sid := newTestSID()
	resp, err := http.PostForm(
		strings.TrimRight(baseURL, "/")+"/s/"+sid+"/capture/start",
		url.Values{"mode": {mode}},
	)
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start session: status %d", resp.StatusCode)
	}
	return sid
}

func newTestSID() string {
	var b [16]byte
	_, _ = cryptorand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func TestDirectMode_ListEvents(t *testing.T) {
	baseURL, appURL := newTestDevlog(t)
	ctx := context.Background()

	// Simulate the dashboard creating a capture session.
	sid := startSession(t, baseURL, "global")

	// Drive captured traffic through the collected app.
	resp, err := http.Get(appURL)
	if err != nil {
		t.Fatalf("app request: %v", err)
	}
	resp.Body.Close()

	// The relay attaches to the existing session.
	c := newDirectClient(baseURL, sid)
	raw, err := c.listEvents(ctx, listEventsInput{})
	if err != nil {
		t.Fatalf("listEvents: %v", err)
	}

	var summaries []map[string]any
	if err := json.Unmarshal(raw, &summaries); err != nil {
		t.Fatalf("decode summaries: %v", err)
	}
	if len(summaries) == 0 {
		t.Fatal("expected at least one captured event")
	}
	if summaries[0]["type"] != "http_server" {
		t.Errorf("expected http_server event, got %v", summaries[0]["type"])
	}
}

func TestSelectSession_SingleAutoSelect(t *testing.T) {
	baseURL, _ := newTestDevlog(t)
	sid := startSession(t, baseURL, "global")

	got, err := selectSession(context.Background(), baseURL, "")
	if err != nil {
		t.Fatalf("selectSession: %v", err)
	}
	if got != sid {
		t.Errorf("auto-selected %s, want the only session %s", got, sid)
	}
}

func TestSelectSession_ExplicitOverride(t *testing.T) {
	baseURL, _ := newTestDevlog(t)
	got, err := selectSession(context.Background(), baseURL, "explicit-sid")
	if err != nil {
		t.Fatalf("selectSession: %v", err)
	}
	if got != "explicit-sid" {
		t.Errorf("explicit sid not honored, got %s", got)
	}
}

func TestExpectOK_SessionGoneIsClearError(t *testing.T) {
	baseURL, _ := newTestDevlog(t)
	// Attach to a session id that was never created.
	c := newDirectClient(baseURL, newTestSID())
	_, err := c.listEvents(context.Background(), listEventsInput{})
	if err == nil {
		t.Fatal("expected an error for a missing session")
	}
	if !strings.Contains(err.Error(), "no longer active") {
		t.Errorf("expected a clear session-ended error, got %v", err)
	}
}
