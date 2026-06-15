# Task 1 — Agent API read endpoints (opt-in, list + detail, DTOs, redaction)

## Context

You are implementing Phase 1 of the spec at **`docs/design/agent-access.md`** (read the "Agent JSON API" section and the "Architecture & Design Decisions" table). This is the **foundational slice**: the read side of the Agent JSON API on the devlog dashboard handler, gated behind an opt-in option, with redaction built in from the start.

This task has **no dependencies**. It establishes files and patterns that Tasks 2–5 extend.

Repository: `github.com/networkteam/devlog`. No build step — verify with `go vet ./...` and `go test ./dashboard/...`.

## Goal

Behind a new opt-in `dashboard.WithAgentAPI()` option (the agent API is **off by default**), expose:

- `GET /api/agent/v1/s/{sid}/events` — event summaries, newest first, with query filters: `type` (`http_server`|`http_client`|`db_query`|`log`), `since`/`until` (RFC 3339), `limit`, `status` (HTTP status class such as `5xx`), `path` (substring match).
- `GET /api/agent/v1/s/{sid}/events/{eventId}` — full detail including the recursive children tree, with **body metadata only** (no raw bytes — those come in Task 2).

All responses are JSON (no HTMX branch). Errors use a consistent `{"error": "..."}` body. Unknown `{sid}` → 404.

## What to implement

### Options (`dashboard/options.go`)
Follow the existing `HandlerOption` pattern (`WithStorageCapacity`, etc.). Add:
- `WithAgentAPI()` — enables registration of the `/api/agent/v1/` routes. Store a bool on `handlerOptions`.
- `WithAgentRedactor(func(*EventDetail) *EventDetail)` — embedder hook applied to each detail DTO before encoding; returning `nil` suppresses the event entirely (omitted from lists, 404 on detail).
- `WithAgentRedactedHeaders(names ...string)` — extends the default redacted-header set (additive).
- `WithAgentInsecureHeaders()` — disables all built-in header redaction (explicit opt-out).

### Route registration (`dashboard/handler.go`)
In `NewHandler`, register the agent routes on the existing `mux` **only when `WithAgentAPI()` was set**. Mirror the existing route style (`dashboard/handler.go:86-100`). When not enabled, the routes must not exist (so requests 404 naturally).

### Handlers (`dashboard/agent_api.go` — NEW)
- Resolve the session with `h.getSessionID(r)` + `h.sessions.Get(sid)` (see `dashboard/handler.go:127`, `:174`). Nil storage → 404 JSON error.
- Call `h.sessions.UpdateActivity(sid)` on reads so agent polling keeps the session alive (the SSE handler does this — `dashboard/handler.go:365`).
- List: read `storage.GetEvents(limit)` (default a sane limit if absent; cap at `h.truncateAfter`), reverse to newest-first as `loadRecentEvents` does (`dashboard/handler.go:421`), apply filters, map to `EventSummary`, encode.
- Detail: `storage.GetEvent(eventID)` → map to `EventDetail` (recursing children) → run redaction → encode; not found → 404.
- A small JSON error helper for the consistent `{"error": ...}` shape.

### DTOs + mapping (`dashboard/agent_api_types.go` — NEW)
- `EventSummary{ id, type, start, durationMs, ... per-type summary fields }`.
- `EventDetail{ ... full per-type payload, children []EventDetail, requestBody/responseBody metadata }`. Body metadata is an object `{size, contentType, available, redacted}` (the `available`/`redacted`/byte-serving semantics are finalized in Task 2 — here, populate `size`/`contentType`/`available` from the stored `*Body` presence).
- Map via a type switch over `Event.Data` (follow `downloadRequestBody`, `dashboard/handler.go:453`):
  - `collector.HTTPServerRequest` / `collector.HTTPClientRequest`: method, path/URL, status, headers, timings, body metadata.
  - `collector.DBQuery`: `query`, `interpolatedQuery` (nullable `*string`), `args`, `language`, `error`, duration.
  - `slog.Record`: `level`, `message`, and attrs via `record.Attrs(func(slog.Attr) bool)` iteration.
- `type AgentRedactor func(*EventDetail) *EventDetail`.

### Redaction
- **Default header masking** (independent of the hook): replace the *values* of `Authorization`, `Cookie`, `Set-Cookie`, `WWW-Authenticate`, `Proxy-Authenticate`, `Proxy-Authorization` (case-insensitive) with the sentinel string `[REDACTED]`, **preserving the keys**, in every HTTP `EventDetail`. `WithAgentRedactedHeaders` adds names; `WithAgentInsecureHeaders` skips masking entirely.
- Apply order per event: header masking first, then the embedder's `WithAgentRedactor`. A `nil` return drops the event from list responses and yields 404 on the detail endpoint.
- Summaries shown in the list must be derived from the **redacted** detail (map detail → redact → derive summary), so redaction can't be bypassed via the list.

## Acceptance criteria & verification

Implement these table-driven tests in `dashboard/agent_api_test.go` (NEW). Follow the `httptest` setup of `devlog_e2e_test.go` for building an instance and driving fixture events through the collectors; use `dashboard/session_manager_test.go` for session/storage construction patterns. Enable the API with `WithAgentAPI()` except where noted.

| Test | Expectation |
|---|---|
| `TestAgentAPI_Disabled_ByDefault` | Handler built **without** `WithAgentAPI` → any `/api/agent/v1/...` route returns 404 |
| `TestAgentAPI_Events_EmptySession` | Unknown `{sid}` → 404 with JSON error body |
| `TestAgentAPI_Events_ListsSummaries` | Session with HTTP + DB + log events → 200, newest-first, correct `type` per summary |
| `TestAgentAPI_Events_FilterByType` | `?type=db_query` → only DB query summaries |
| `TestAgentAPI_Events_FilterByTimeAndLimit` | `?since=…&limit=1` → time window and limit both respected |
| `TestAgentAPI_EventDetail_HTTPServerWithChildren` | HTTP server event with a DB child → children tree present, `interpolatedQuery` present, durations set, body metadata only (no inline bytes) |
| `TestAgentAPI_EventDetail_LogRecord` | slog event → `level`/`message`/`attrs` mapped |
| `TestAgentAPI_DefaultHeaderRedaction` | Event with `Authorization` + `Cookie`, no redactor configured → values are `[REDACTED]`, keys preserved |
| `TestAgentAPI_RedactedHeaders_Extended` | `WithAgentRedactedHeaders("X-Api-Key")` → custom header masked in addition to defaults |
| `TestAgentAPI_InsecureHeaders_OptOut` | `WithAgentInsecureHeaders()` → `Authorization` value visible |
| `TestAgentAPI_Redactor_DropsEvent` | Redactor returning `nil` for `/auth/*` paths → event omitted from list and 404 on its detail |
| `TestAgentAPI_KeepsSessionAlive` | Repeated list reads past the idle timeout keep the session alive |

Also: `go vet ./...` clean; `go test ./dashboard/...` green.

## Out of scope (later tasks)
- Raw body bytes, size cap, body `available/redacted` 410 behavior → Task 2.
- Capture control and stats endpoints → Task 3.
- CLI / MCP → Tasks 4–5.
