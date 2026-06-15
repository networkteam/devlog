# Task 3 — Capture control + stats endpoints

## Context

Phase 1 of **`docs/design/agent-access.md`**. Adds capture lifecycle control and a stats endpoint to the Agent JSON API.

**Depends on Task 1** (`docs/tasks/agent-access/task-1-agent-api-read.md`) — agent API skeleton, `WithAgentAPI()` gating, JSON error helper. Can be done in parallel with Task 2.

Repository: `github.com/networkteam/devlog`. Verify with `go vet ./...` and `go test ./dashboard/...`.

## Goal

Add:
- `POST /api/agent/v1/s/{sid}/capture/start` — JSON body `{"mode":"session"|"global"}`. Create/resume the session's capture.
- `POST /api/agent/v1/s/{sid}/capture/stop` — pause capture, keep the session and events.
- `GET /api/agent/v1/s/{sid}/capture/status` — `{active, mode, eventCount}`.
- `GET /api/agent/v1/stats` — the same data the existing `getStats` JSON branch returns.

## What to implement (`dashboard/agent_api.go`)

- **start:** parse `mode` from the JSON body (default `session`) using `collector.ParseCaptureModeOrDefault`. Create/resume via `h.sessions.GetOrCreate(sid, mode)` and `storage.SetCapturing(true)` / `storage.SetCaptureMode(mode)` — mirror the existing `captureStart` logic (`dashboard/handler.go:547`) **but without any cookie handling** (agents have no cookies). On capacity errors from `GetOrCreate`, return 503 JSON error (as the UI handler does).
- **stop:** `storage.SetCapturing(false)`, keep storage intact (mirror `captureStop`, `dashboard/handler.go:587`). No session → return inactive status, not an error.
- **status:** `{active: storage.IsCapturing(), mode: storage.CaptureMode().String(), eventCount: len(storage.GetEvents(<cap>))}`. No session → `{active:false, mode:"session", eventCount:0}`.
- **stats:** reuse `h.eventAggregator.CalculateStats()` and the `StatsResponse` shape from `getStats` (`dashboard/handler.go:721`); JSON only.

Note: in Phase 1 `mode` is agent-selectable (local dev). Phase 2 locks it at pairing — do not add that restriction here.

## Acceptance criteria & verification

Extend `dashboard/agent_api_test.go`:

| Test | Expectation |
|---|---|
| `TestAgentAPI_CaptureLifecycle` | Fresh client-generated `{sid}` (UUIDv4): POST start (`global`) → session created & capturing; drive traffic → events captured; GET status → `active:true, mode:"global", eventCount>0`; POST stop → `active:false`, events retained |
| `TestAgentAPI_Stats` | GET `/api/agent/v1/stats` → JSON with memory/session/event counts matching `CalculateStats()` |

Also: start with an invalid mode → 400 JSON error; `go vet ./...` clean; `go test ./dashboard/...` green.

## Out of scope
- CLI / MCP → Tasks 4–5.
