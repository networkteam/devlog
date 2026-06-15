package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/devlog/dashboard"
)

// connectMCP wires an in-memory MCP client to a server backed by reg.
func connectMCP(t *testing.T, reg *registry) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	server := newMCPServer(reg)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func toolText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			if res.IsError {
				t.Fatalf("tool returned error: %s", tc.Text)
			}
			return tc.Text
		}
	}
	t.Fatal("no text content in tool result")
	return ""
}

// firstText returns the first text content without failing on IsError, for tests
// that assert on error results.
func firstText(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	return res
}

func TestMCP_ListTools(t *testing.T) {
	baseURL, _ := newTestDevlog(t)
	sid := startSession(t, baseURL, "global")
	reg := newRegistry()
	reg.add("local", newDirectClient(baseURL, sid))

	cs := connectMCP(t, reg)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := make(map[string]bool)
	for _, tl := range res.Tools {
		got[tl.Name] = true
	}
	for _, want := range []string{"list_events", "get_event", "get_stats", "list_environments"} {
		if !got[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestMCP_ListAndGetEvent(t *testing.T) {
	baseURL, appURL := newTestDevlog(t)
	sid := startSession(t, baseURL, "global")
	dc := newDirectClient(baseURL, sid)
	reg := newRegistry()
	reg.add("local", dc)

	cs := connectMCP(t, reg)
	ctx := context.Background()

	// Capture is already on (started by the simulated dashboard); drive traffic.
	resp, err := http.Get(appURL)
	if err != nil {
		t.Fatalf("app request: %v", err)
	}
	resp.Body.Close()

	// list_events via MCP.
	listRes, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_events", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool list_events: %v", err)
	}
	var summaries []map[string]any
	if err := json.Unmarshal([]byte(toolText(t, listRes)), &summaries); err != nil {
		t.Fatalf("decode summaries: %v", err)
	}
	if len(summaries) == 0 {
		t.Fatal("expected at least one event")
	}
	id, _ := summaries[0]["id"].(string)
	if id == "" {
		t.Fatal("expected an event id")
	}

	// get_event via MCP.
	getRes, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_event", Arguments: map[string]any{"eventId": id}})
	if err != nil {
		t.Fatalf("CallTool get_event: %v", err)
	}
	detailText := toolText(t, getRes)
	var detail map[string]any
	if err := json.Unmarshal([]byte(detailText), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail["id"] != id {
		t.Errorf("detail id mismatch: got %v want %s", detail["id"], id)
	}

	// Structured content matches the direct API payload.
	raw, err := dc.getEvent(ctx, id)
	if err != nil {
		t.Fatalf("direct getEvent: %v", err)
	}
	var apiDetail map[string]any
	if err := json.Unmarshal(raw, &apiDetail); err != nil {
		t.Fatalf("decode api detail: %v", err)
	}
	if apiDetail["id"] != detail["id"] || apiDetail["type"] != detail["type"] {
		t.Errorf("tool payload does not match API payload")
	}
}

func TestMCP_CaptureStatus_ReadOnly(t *testing.T) {
	baseURL, appURL := newTestDevlog(t)
	sid := startSession(t, baseURL, "global")
	reg := newRegistry()
	reg.add("local", newDirectClient(baseURL, sid))
	cs := connectMCP(t, reg)

	resp, err := http.Get(appURL)
	if err != nil {
		t.Fatalf("app request: %v", err)
	}
	resp.Body.Close()

	statusRes := callTool(t, cs, "capture_status", map[string]any{})
	var status map[string]any
	if err := json.Unmarshal([]byte(toolText(t, statusRes)), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status["active"] != true || status["mode"] != "global" {
		t.Errorf("unexpected status: %v", status)
	}
	if n, _ := status["eventCount"].(float64); n < 1 {
		t.Errorf("expected eventCount >= 1, got %v", status["eventCount"])
	}

	// Capture control tools are not exposed to the agent.
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tl := range tools.Tools {
		if tl.Name == "capture_start" || tl.Name == "capture_stop" {
			t.Errorf("agent must not expose %q", tl.Name)
		}
	}
}

func TestMCP_GetRequestBody(t *testing.T) {
	baseURL, appURL := newTestDevlog(t)
	sid := startSession(t, baseURL, "global")
	dc := newDirectClient(baseURL, sid)
	reg := newRegistry()
	reg.add("local", dc)
	cs := connectMCP(t, reg)

	// POST with a body so a request body is captured.
	payload := `{"hello":"body"}`
	resp, err := http.Post(appURL, "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("app post: %v", err)
	}
	resp.Body.Close()

	listRes := callTool(t, cs, "list_events", map[string]any{})
	var summaries []map[string]any
	if err := json.Unmarshal([]byte(toolText(t, listRes)), &summaries); err != nil {
		t.Fatalf("decode summaries: %v", err)
	}
	if len(summaries) == 0 {
		t.Fatal("expected a captured event")
	}
	id, _ := summaries[0]["id"].(string)

	bodyRes := callTool(t, cs, "get_request_body", map[string]any{"eventId": id})
	got := toolText(t, bodyRes)
	if got != payload {
		t.Errorf("request body mismatch: got %q want %q", got, payload)
	}
}

func TestMCP_GetBody_Redacted(t *testing.T) {
	redactor := func(d *dashboard.EventDetail) *dashboard.EventDetail {
		if d.RequestBody != nil {
			d.RequestBody.Available = false // suppress request bodies
		}
		return d
	}
	baseURL, appURL := newTestDevlogWith(t, dashboard.WithAgentRedactor(redactor))
	sid := startSession(t, baseURL, "global")
	dc := newDirectClient(baseURL, sid)
	reg := newRegistry()
	reg.add("local", dc)
	cs := connectMCP(t, reg)

	resp, err := http.Post(appURL, "application/json", strings.NewReader(`{"secret":"x"}`))
	if err != nil {
		t.Fatalf("app post: %v", err)
	}
	resp.Body.Close()

	listRes := callTool(t, cs, "list_events", map[string]any{})
	var summaries []map[string]any
	if err := json.Unmarshal([]byte(toolText(t, listRes)), &summaries); err != nil {
		t.Fatalf("decode summaries: %v", err)
	}
	if len(summaries) == 0 {
		t.Fatal("expected a captured event")
	}
	id, _ := summaries[0]["id"].(string)

	bodyRes := callTool(t, cs, "get_request_body", map[string]any{"eventId": id})
	if !bodyRes.IsError {
		t.Fatalf("expected an error result for a redacted body, got: %s", firstText(bodyRes))
	}
	if msg := firstText(bodyRes); !strings.Contains(msg, "redacted") && !strings.Contains(msg, "410") {
		t.Errorf("expected redaction message, got %q", msg)
	}
}

func TestMCP_HostValidation(t *testing.T) {
	guarded := loopbackHostGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		host string
		want int
	}{
		{"evil.example.com", http.StatusForbidden},
		{"127.0.0.1:4711", http.StatusOK},
		{"localhost:4711", http.StatusOK},
		{"[::1]:4711", http.StatusOK},
	}
	for _, tc := range tests {
		req := httptest.NewRequest(http.MethodPost, "http://example/mcp", nil)
		req.Host = tc.host
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("host %q: got %d, want %d", tc.host, rec.Code, tc.want)
		}
	}
}
