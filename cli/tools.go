package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool input types. Each read tool accepts an optional environment selector.

type listEventsInput struct {
	Environment string `json:"environment,omitempty" jsonschema:"target environment; optional when only one is connected"`
	Type        string `json:"type,omitempty" jsonschema:"filter by event type: http_server, http_client, db_query or log"`
	Since       string `json:"since,omitempty" jsonschema:"only events at or after this RFC3339 timestamp"`
	Until       string `json:"until,omitempty" jsonschema:"only events at or before this RFC3339 timestamp"`
	Status      string `json:"status,omitempty" jsonschema:"filter by HTTP status class (e.g. 5xx) or exact code"`
	Path        string `json:"path,omitempty" jsonschema:"substring match on request path or URL"`
	Limit       int    `json:"limit,omitempty" jsonschema:"maximum number of events to return"`
}

type getEventInput struct {
	Environment string `json:"environment,omitempty" jsonschema:"target environment; optional when only one is connected"`
	EventID     string `json:"eventId" jsonschema:"the id of the event to fetch"`
}

type statsInput struct {
	Environment string `json:"environment,omitempty" jsonschema:"target environment; optional when only one is connected"`
}

type bodyInput struct {
	Environment string `json:"environment,omitempty" jsonschema:"target environment; optional when only one is connected"`
	EventID     string `json:"eventId" jsonschema:"the id of the event whose body to fetch"`
}

type captureControlInput struct {
	Environment string `json:"environment,omitempty" jsonschema:"target environment; optional when only one is connected"`
}

type listEnvironmentsInput struct{}

// registerTools attaches the read-only MCP tools to the server.
func registerTools(server *mcp.Server, reg *registry) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_events",
		Description: "List captured devlog events (newest first) with optional filters.",
	}, reg.toolListEvents)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_event",
		Description: "Get the full detail of a single devlog event, including child events.",
	}, reg.toolGetEvent)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_stats",
		Description: "Get devlog memory and event statistics.",
	}, reg.toolGetStats)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_request_body",
		Description: "Fetch the captured request body of an event (subject to redaction and a size cap).",
	}, reg.toolGetRequestBody)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_response_body",
		Description: "Fetch the captured response body of an event (subject to redaction and a size cap).",
	}, reg.toolGetResponseBody)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "capture_status",
		Description: "Report whether capture is active, the mode, and the captured event count. Capture is started and stopped by the user in the devlog dashboard, not by the agent.",
	}, reg.toolCaptureStatus)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_environments",
		Description: "List the devlog environments this relay is connected to.",
	}, reg.toolListEnvironments)
}

func (r *registry) toolListEvents(ctx context.Context, _ *mcp.CallToolRequest, in listEventsInput) (*mcp.CallToolResult, any, error) {
	c, err := r.resolve(in.Environment)
	if err != nil {
		return errorResult(err), nil, nil
	}
	raw, err := c.listEvents(ctx, in)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return jsonResult(raw), nil, nil
}

func (r *registry) toolGetEvent(ctx context.Context, _ *mcp.CallToolRequest, in getEventInput) (*mcp.CallToolResult, any, error) {
	c, err := r.resolve(in.Environment)
	if err != nil {
		return errorResult(err), nil, nil
	}
	raw, err := c.getEvent(ctx, in.EventID)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return jsonResult(raw), nil, nil
}

func (r *registry) toolGetStats(ctx context.Context, _ *mcp.CallToolRequest, in statsInput) (*mcp.CallToolResult, any, error) {
	c, err := r.resolve(in.Environment)
	if err != nil {
		return errorResult(err), nil, nil
	}
	raw, err := c.getStats(ctx)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return jsonResult(raw), nil, nil
}

func (r *registry) toolGetRequestBody(ctx context.Context, _ *mcp.CallToolRequest, in bodyInput) (*mcp.CallToolResult, any, error) {
	return r.getBody(ctx, in.Environment, in.EventID, "request")
}

func (r *registry) toolGetResponseBody(ctx context.Context, _ *mcp.CallToolRequest, in bodyInput) (*mcp.CallToolResult, any, error) {
	return r.getBody(ctx, in.Environment, in.EventID, "response")
}

func (r *registry) getBody(ctx context.Context, environment, eventID, side string) (*mcp.CallToolResult, any, error) {
	c, err := r.resolve(environment)
	if err != nil {
		return errorResult(err), nil, nil
	}
	br, err := c.getBody(ctx, eventID, side)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return bodyToolResult(br), nil, nil
}

func (r *registry) toolCaptureStatus(ctx context.Context, _ *mcp.CallToolRequest, in captureControlInput) (*mcp.CallToolResult, any, error) {
	c, err := r.resolve(in.Environment)
	if err != nil {
		return errorResult(err), nil, nil
	}
	raw, err := c.captureStatus(ctx)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return jsonResult(raw), nil, nil
}

func (r *registry) toolListEnvironments(_ context.Context, _ *mcp.CallToolRequest, _ listEnvironmentsInput) (*mcp.CallToolResult, any, error) {
	data, err := json.Marshal(r.list())
	if err != nil {
		return errorResult(err), nil, nil
	}
	return jsonResult(data), nil, nil
}

// bodyToolResult renders a fetched body as MCP content: textual bodies inline as
// text; binary bodies return a descriptive note instead of raw bytes so we don't
// dump binary into the model.
func bodyToolResult(br *bodyResult) *mcp.CallToolResult {
	if isTextualContentType(br.contentType) {
		text := string(br.data)
		if br.truncated {
			text += "\n\n[devlog: body truncated to the configured size cap]"
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
	}
	note := fmt.Sprintf("binary body not inlined: content-type %q, %d bytes", br.contentType, len(br.data))
	if br.truncated {
		note += " (truncated to the configured size cap)"
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: note}}}
}

func isTextualContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if ct == "" {
		return true // unknown/short bodies: treat as text
	}
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch {
	case strings.HasPrefix(ct, "text/"):
		return true
	case strings.Contains(ct, "json"),
		strings.Contains(ct, "xml"),
		strings.Contains(ct, "x-www-form-urlencoded"),
		strings.Contains(ct, "graphql"):
		return true
	default:
		return false
	}
}

func jsonResult(raw json.RawMessage) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}},
	}
}

func errorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}
