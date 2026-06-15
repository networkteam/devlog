package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// directClient talks to a local devlog Agent JSON API over plain HTTP. It is
// attached to an existing capture session (owned by a dashboard browser tab); it
// never creates sessions of its own.
type directClient struct {
	agentBase string // <base-url>/api/agent/v1
	httpc     *http.Client
	sid       string // attached session id
}

func newDirectClient(baseURL, sid string) *directClient {
	return &directClient{
		agentBase: agentBaseURL(baseURL),
		httpc:     defaultHTTPClient(),
		sid:       sid,
	}
}

func agentBaseURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/api/agent/v1"
}

func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *directClient) sessionURL(suffix string) string {
	return c.agentBase + "/s/" + c.sid + suffix
}

func (c *directClient) listEvents(ctx context.Context, in listEventsInput) (json.RawMessage, error) {
	q := url.Values{}
	if in.Type != "" {
		q.Set("type", in.Type)
	}
	if in.Since != "" {
		q.Set("since", in.Since)
	}
	if in.Until != "" {
		q.Set("until", in.Until)
	}
	if in.Status != "" {
		q.Set("status", in.Status)
	}
	if in.Path != "" {
		q.Set("path", in.Path)
	}
	if in.Limit > 0 {
		q.Set("limit", strconv.Itoa(in.Limit))
	}
	u := c.sessionURL("/events")
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return c.expectOK(c.getRaw(ctx, u))
}

func (c *directClient) getEvent(ctx context.Context, eventID string) (json.RawMessage, error) {
	u := c.sessionURL("/events/" + url.PathEscape(eventID))
	return c.expectOK(c.getRaw(ctx, u))
}

func (c *directClient) getStats(ctx context.Context) (json.RawMessage, error) {
	return c.expectOK(c.getRaw(ctx, c.agentBase+"/stats"))
}

func (c *directClient) captureStatus(ctx context.Context) (json.RawMessage, error) {
	return c.expectOK(c.getRaw(ctx, c.sessionURL("/capture/status")))
}

// bodyResult is a fetched request/response body plus its content type and whether
// it was truncated by the server's size cap.
type bodyResult struct {
	data        []byte
	contentType string
	truncated   bool
}

// getBody fetches the raw request or response body (side is "request" or
// "response"). Non-200 responses (404 missing, 410 redacted, 400 wrong type)
// become errors carrying the server's message.
func (c *directClient) getBody(ctx context.Context, eventID, side string) (*bodyResult, error) {
	u := c.sessionURL("/events/" + url.PathEscape(eventID) + "/" + side + "-body")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp.StatusCode, data)
	}
	return &bodyResult{
		data:        data,
		contentType: resp.Header.Get("Content-Type"),
		truncated:   resp.Header.Get("X-Devlog-Truncated") == "true",
	}, nil
}

func (c *directClient) getRaw(ctx context.Context, u string) (json.RawMessage, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return json.RawMessage(data), resp.StatusCode, nil
}

func (c *directClient) expectOK(raw json.RawMessage, status int, err error) (json.RawMessage, error) {
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound && isNoSessionError(raw) {
		return nil, fmt.Errorf("the devlog session %s is no longer active (dashboard closed?); reconnect the relay", c.sid)
	}
	if status != http.StatusOK {
		return nil, apiError(status, raw)
	}
	return raw, nil
}

func isNoSessionError(raw json.RawMessage) bool {
	var e struct {
		Error string `json:"error"`
	}
	return json.Unmarshal(raw, &e) == nil && strings.Contains(e.Error, "no capture session")
}

func apiError(status int, raw json.RawMessage) error {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Error != "" {
		return fmt.Errorf("agent API error (%d): %s", status, e.Error)
	}
	return fmt.Errorf("agent API error: status %d", status)
}

// sessionInfo mirrors one entry of GET /api/agent/v1/sessions.
type sessionInfo struct {
	SessionID       string `json:"sessionId"`
	Mode            string `json:"mode"`
	Capturing       bool   `json:"capturing"`
	EventCount      int    `json:"eventCount"`
	LastActiveMsAgo int64  `json:"lastActiveMsAgo"`
}

// fetchSessions lists the active capture sessions of the devlog instance at baseURL.
func fetchSessions(ctx context.Context, baseURL string, httpc *http.Client) ([]sessionInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, agentBaseURL(baseURL)+"/sessions", nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp.StatusCode, data)
	}
	var sessions []sessionInfo
	if err := json.Unmarshal(data, &sessions); err != nil {
		return nil, fmt.Errorf("decoding sessions: %w", err)
	}
	return sessions, nil
}
