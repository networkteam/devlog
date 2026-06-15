# Technical Design: Agent Access to devlog — JSON API & Local MCP (Phase 1)

Based on the concept in [discussion #10 — RFC: Agent Access to devlog via Browser-Relayed Tunnel](https://github.com/networkteam/devlog/discussions/10).

This is **Phase 1 of two specs**:

- **Phase 1 (this doc)** — the Agent JSON API on the devlog handler, plus a `devlog` CLI that exposes it to agents over MCP for **local development** (`--direct` mode). No browser, no pairing, no production data egress.
- **Phase 2 ([agent-access-relay.md](./agent-access-relay.md))** — the browser-relayed tunnel that lets an agent reach **deployed (stage/production)** devlog instances through the developer's authenticated browser tab. Builds on the API defined here.

The split exists because the two phases have very different risk profiles: Phase 1 touches only the developer's own machine; Phase 2 exposes captured production traffic to an LLM and is security-sensitive. Phase 1 is independently useful and shippable.

## Goals & Requirements

Enable AI coding agents (Claude Code, Cursor, …) to inspect devlog data — captured requests, SQL queries, logs, timings — on a **local dev instance**, without coupling to the embedder's auth.

Constraints from the RFC that apply already in Phase 1:

- devlog is an embedded handler. The agent API must inherit the embedder's middleware, never validate tokens itself.
- Agent responses end up at LLM providers — sensitive data must be redactable before it leaves the process.
- Read-only by default; the only mutation is capture control (start/stop), scoped to the agent's own session.

**Out of scope** (Phase 2 or the RFC's optional phase 4): browser relay, deployed-instance access, push/streaming channels, scoped writes beyond capture control, broker transport.

## High-Level Architecture (Phase 1)

```
 developer machine (local dev)
┌──────────────────────────────────────────────────────────────┐
│  Host app (dev)                                               │
│   └ devlog dashboard handler                                  │
│      └ /api/agent/v1/   (JSON + redaction)  ◄──── HTTP ───┐   │
│                                                           │   │
│  devlog relay --direct http://localhost:PORT/_devlog      │   │
│   └ MCP server (streamable HTTP, loopback) ───────────────┘   │
│        ▲                                                       │
│        │ MCP                                                   │
│   AI agent (Claude Code, …)                                   │
└──────────────────────────────────────────────────────────────┘
```

In `--direct` mode the CLI calls the JSON API itself over plain HTTP. There is no browser hop and no credential relaying — it is the developer's own local instance.

## Architecture & Design Decisions

| Decision | Choice | Rationale / Grounding |
|---|---|---|
| Agent API placement | New routes on the existing dashboard `http.ServeMux` under `/api/agent/v1/` | Inherits the embedder's middleware for free (RFC requirement). Convention: route registration in `dashboard/handler.go` (`NewHandler`) |
| API session scoping | Session ID in path: `/api/agent/v1/s/{sid}/…` | Convention: existing UI routes `/s/{sid}/…` in `dashboard/handler.go:87-100`; storage is per-session (`SessionManager`). |
| **Attach, don't create** | The CLI does not mint a session. It lists active sessions (`GET /api/agent/v1/sessions`) and **attaches** to an existing dashboard session, so the agent and the developer's dashboard tab see the same events. The browser tab owns the session's lifetime (its SSE keep-alive); no agent-side heartbeat or cleanup. | Matches the Phase 2 relay, where the paired tab's `sid` is shared by construction. Avoids the 30s idle-timeout reclaiming an agent-owned session and the ownership/cleanup questions that come with one. |
| Session selection (`--direct`) | One active session → auto-attach; several → interactive prompt; none → wait until one appears. `--session <sid>` overrides non-interactively. | Keeps the common case zero-config while staying scriptable. |
| API package layout | New files in the `dashboard` package (`agent_api.go`, `agent_api_types.go`) | Convention: `dashboard` is a flat package; handlers need unexported access to `SessionManager`. A sub-package would force exporting session internals |
| DTOs instead of rendering `collector.Event` directly | Explicit `EventSummary` / `EventDetail` JSON structs mapped via type switch on `Event.Data` | `Event.Data` is `any` (`collector/event.go:20`) holding `HTTPServerRequest`, `HTTPClientRequest`, `DBQuery`, `slog.Record`; same type-switch pattern as `downloadRequestBody` (`dashboard/handler.go:453`). DTOs give a stable wire format and a single place for redaction |
| **Agent API availability** | **Opt-in: `dashboard.WithAgentAPI()`. Off by default.** | The dashboard UI shows data to a human on screen; the agent API ships it to a third-party LLM — a different risk profile, so default-on is not safe even behind the same auth. Opt-in is the conservative default; embedders enable it deliberately |
| **Bodies never inlined** | List and detail responses carry only body **metadata** (`{size, contentType, available, redacted}`). Raw bytes are served only by the dedicated body endpoints / MCP tools | Smallest default egress: the agent receives bodies only when it explicitly asks. In Phase 2 those body tools are additionally gated behind dashboard confirmation |
| Body size cap | Body endpoints truncate to `WithAgentMaxBodyBytes` (default 64 KiB) with an explicit `truncated: true` marker | Avoids shipping multi-MB or binary blobs to the LLM; assumption — tune default during implementation |
| Redaction hook | `dashboard.WithAgentRedactor(func(*EventDetail) *EventDetail)` — mutate and return, or return `nil` to suppress the event entirely. Applied before encoding; summaries derive from redacted details | Research: mirrors Sentry's `BeforeSend` (scrub-or-drop in one hook, [Sentry data scrubbing](https://develop.sentry.dev/sdk/foundations/data-scrubbing/)). Operating on DTOs (not `collector` types) means redaction can't corrupt stored events |
| Default header redaction | Independent of the hook, always mask values of `Authorization`, `Cookie`, `Set-Cookie`, `WWW-Authenticate`, `Proxy-Authenticate`, `Proxy-Authorization` with `[REDACTED]` (key preserved). `WithAgentRedactedHeaders(names...)` extends; `WithAgentInsecureHeaders()` is the explicit opt-out | Research: default set and additive-extend/loud-opt-out pattern from [otelhttptrace](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace); value-masked-key-preserved per [OTel HTTP semconv](https://opentelemetry.io/docs/specs/semconv/http/http-spans/) |
| Redaction observability | Masked values use sentinels; suppressed bodies get `redacted: true` — never silent omission | Research: an LLM consumer must distinguish "absent" from "redacted" to avoid false inferences |
| CLI location | New Go module `./cli` in `go.work`, binary `devlog`, subcommand `relay` (with `--direct` here; browser mode added in Phase 2) | Convention: separate modules for non-library code (`./acceptance`, `./example`, `./dbadapter/sqllogger` in `go.work`). Keeps the library's `go.mod` free of CLI deps |
| CLI framework | `spf13/cobra` | Already in the dependency graph as indirect dep; stdlib `flag` would also do — keep cobra only if more commands are expected, otherwise simplify during implementation |
| MCP server | Official SDK `github.com/modelcontextprotocol/go-sdk` (v1.x), `StreamableHTTPHandler` on loopback | Research: verified the SDK ships `StreamableHTTPHandler` — a persistent `http.Handler` for networked deployments with per-session tracking ([streamable.go](https://github.com/modelcontextprotocol/go-sdk/blob/main/mcp/streamable.go), [examples/http](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/examples/http)). HTTP (not stdio) is required because the relay is a long-running server reachable by both the agent and, in Phase 2, the browser |
| No agent capture control | The agent **cannot start or stop capture**, in any mode. Capture (and its mode) is managed by the user in the dashboard UI; the agent only piggy-backs on a user-managed session. Only `capture_status` (read-only) is exposed, plus the events/bodies/stats reads. | Keeps the human in control of what is captured (matters most for Phase 2's production traffic), removes the need for mode-locking/pairing-mode machinery, and matches the attach model — the session is the dashboard's, the agent observes it. |
| Dashboard "agent connected" badge | **Deferred** — a follow-up that shows in the dashboard when an agent is attached to a session. Needs server-side attach tracking + a templ change. | Nice-to-have for visibility; not required for the attach flow to work. |

## Implementation Changes

### Agent JSON API (`dashboard` package)

New endpoints registered in `NewHandler`, only when `WithAgentAPI()` is set (all JSON, no HTMX branch):

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/agent/v1/s/{sid}/events` | Event summaries, newest first. Query params: `type` (`http_server`, `http_client`, `db_query`, `log`), `since`/`until` (RFC 3339), `limit`, `status` (HTTP status class, e.g. `5xx`), `path` (substring match) |
| GET | `/api/agent/v1/s/{sid}/events/{eventId}` | Full detail incl. children tree: SQL with `InterpolatedQuery`, log records, timings, headers, **body metadata only** |
| GET | `/api/agent/v1/s/{sid}/events/{eventId}/request-body` | Raw body (≤ cap), original content type (logic of `downloadRequestBody`) |
| GET | `/api/agent/v1/s/{sid}/events/{eventId}/response-body` | Raw body, ditto |
| GET | `/api/agent/v1/s/{sid}/capture/status` | `{active, mode, eventCount}` (read-only; no agent start/stop) |
| GET | `/api/agent/v1/sessions` | List active capture sessions (`sid`, mode, capturing, eventCount, lastActiveMsAgo) so the relay can attach |
| GET | `/api/agent/v1/stats` | Same data as existing `getStats` JSON branch |

Notes:

- `{sid}` not found → 404 with JSON error body `{"error": "..."}`; consistent error shape on all endpoints.
- Event list endpoints call `sessions.UpdateActivity(sid)` so agent polling keeps the session alive (as the SSE handler does).
- DTO mapping (`agent_api_types.go`): `EventSummary{id, type, start, durationMs, summary fields per type}`; `EventDetail` embeds the full per-type payload (`method/path/status/headers` for HTTP, `query/interpolatedQuery/args/language/error` for DB, `level/message/attrs` for logs via `slog.Record.Attrs` iteration) plus recursive `children`, and `requestBody`/`responseBody` metadata objects `{size, contentType, available, redacted}`.
- Redaction order per event: built-in header masking first, then the embedder's redactor (`nil` return → event omitted from lists, 404 on detail). A redacted body returns 410 Gone with a JSON error so agents see "redacted", not "missing".

### CLI (`./cli` module, binary `devlog`) — `--direct` mode

`devlog relay --direct <base-url> [--mcp-port <port>]`

- Binds `127.0.0.1` only; serves MCP at `/mcp` via the SDK's `StreamableHTTPHandler`. Random ephemeral MCP port by default; prints the MCP URL on startup.
- Issues plain HTTP requests to `<base-url>/api/agent/v1/…`. On startup, selects an existing session via `GET /api/agent/v1/sessions` (auto / prompt / wait, or `--session`) and attaches to it; registers a synthetic environment `local`. It never creates a session.
- MCP tools (all read-only): `list_events`, `get_event`, `get_request_body`, `get_response_body`, `capture_status`, `get_stats`, `list_environments`. No `capture_start`/`capture_stop` — capture is user-managed in the dashboard. (The `environment` parameter and multiplexing matter in Phase 2; in `--direct` there is one environment `local`.)
- MCP endpoint is unauthenticated on loopback (standard for local MCP servers) **but validates the `Host` header is a loopback literal** to block DNS-rebinding from a browser; data exposure is further bounded by the redaction hook server-side.

### Files to Modify

| File | Changes |
|---|---|
| `dashboard/agent_api.go` | **NEW** — agent endpoint handlers |
| `dashboard/agent_api_types.go` | **NEW** — DTOs, type-switch mapping, redactor type |
| `dashboard/handler.go` | Register `/api/agent/v1/` routes when `WithAgentAPI()` is set |
| `dashboard/options.go` | `WithAgentAPI`, `WithAgentRedactor`, `WithAgentRedactedHeaders`, `WithAgentInsecureHeaders`, `WithAgentMaxBodyBytes` |
| `cli/` | **NEW module** — `main.go`, `relay` command (`--direct`), MCP server + tool definitions, direct HTTP client |
| `go.work` | Add `./cli` |

## Test Cases

### 1. Agent API handler tests (`dashboard/agent_api_test.go` — NEW; follow `TestE2E` setup in `devlog_e2e_test.go`: build an `Instance` with `WithAgentAPI()`, collect fixture events through the aggregator, serve via `httptest`)

| Test | Fixture | Action | Expectation |
|---|---|---|---|
| `TestAgentAPI_Disabled_ByDefault` | Handler without `WithAgentAPI` | GET any agent route | 404 |
| `TestAgentAPI_Events_EmptySession` | Handler, no session | GET `/api/agent/v1/s/{sid}/events` | 404, JSON error body |
| `TestAgentAPI_Events_ListsSummaries` | Session with HTTP+DB+log events | GET events | 200, newest first, correct `type` per event |
| `TestAgentAPI_Events_FilterByType` | Mixed events | GET `?type=db_query` | Only DB query summaries |
| `TestAgentAPI_Events_FilterByTimeAndLimit` | Events with known timestamps | GET `?since=…&limit=1` | Window + limit respected |
| `TestAgentAPI_EventDetail_HTTPServerWithChildren` | Request event with DB child | GET detail | Children tree, `interpolatedQuery`, durations, body metadata only (no inline bytes) |
| `TestAgentAPI_EventDetail_LogRecord` | slog event with attrs | GET detail | level/message/attrs mapped |
| `TestAgentAPI_RequestBody_Raw` | Event with JSON request body | GET request-body | 200, original content type, exact bytes |
| `TestAgentAPI_RequestBody_Truncated` | Event with body > cap | GET request-body | Truncated to cap, `truncated: true` signaled |
| `TestAgentAPI_DefaultHeaderRedaction` | Event with `Authorization` + `Cookie`, no redactor | GET detail | Values `[REDACTED]`, keys preserved |
| `TestAgentAPI_RedactedHeaders_Extended` | `WithAgentRedactedHeaders("X-Api-Key")` | GET detail | Custom header masked in addition to defaults |
| `TestAgentAPI_InsecureHeaders_OptOut` | `WithAgentInsecureHeaders()` | GET detail | `Authorization` value visible |
| `TestAgentAPI_Redactor_DropsEvent` | Redactor returning `nil` for `/auth/*` | GET list + detail + body | Omitted from list, 404 on detail and body |
| `TestAgentAPI_Redactor_RedactsBody` | Redactor clearing request-body availability | GET detail + request-body | Detail shows `redacted: true`; body endpoint returns 410 |
| `TestAgentAPI_CaptureLifecycle` | Fresh sid (client-generated UUID) | POST start (global) → traffic → GET status → POST stop | Session created, events counted, capture stops |
| `TestAgentAPI_KeepsSessionAlive` | Session near idle timeout | GET events repeatedly past timeout | Session not cleaned up |

### 2. CLI direct-mode + MCP tests (`cli/direct_test.go`, `cli/mcp_test.go` — NEW; `httptest` devlog instance + in-process MCP client/server transport from the SDK)

| Test | Fixture | Action | Expectation |
|---|---|---|---|
| `TestDirectMode_ListEvents` | `httptest` devlog instance; a session is created (simulating the dashboard) | `--direct` client attaches to the session → list | Events returned without any browser |
| `TestMCP_ListTools` | Relay in `--direct` mode | MCP `tools/list` | All tools present with schemas |
| `TestMCP_ListAndGetEvent` | devlog instance with fixture events | `list_events` → `get_event` | Structured content matches API payloads |
| `TestMCP_CaptureStatus_ReadOnly` | Session created via dashboard + traffic | `capture_status` + `tools/list` | Status reported; `capture_start`/`capture_stop` not exposed |
| `TestMCP_HostValidation` | Relay MCP server | Request with non-loopback `Host` header | Rejected (DNS-rebinding defense) |
