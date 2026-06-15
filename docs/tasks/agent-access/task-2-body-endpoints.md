# Task 2 — Body endpoints, size cap, body redaction

## Context

Phase 1 of **`docs/design/agent-access.md`**. This task adds the raw-body endpoints to the Agent JSON API and finalizes body redaction semantics.

**Depends on Task 1** (`docs/tasks/agent-access/task-1-agent-api-read.md`) — the agent API skeleton, `WithAgentAPI()` gating, the `EventDetail` DTO with body metadata fields, the JSON error helper, and the `WithAgentRedactor` hook already exist. Read that task file to know what to expect.

Repository: `github.com/networkteam/devlog`. Verify with `go vet ./...` and `go test ./dashboard/...`.

## Goal

Add:
- `GET /api/agent/v1/s/{sid}/events/{eventId}/request-body`
- `GET /api/agent/v1/s/{sid}/events/{eventId}/response-body`

serving raw body bytes with the original content type, bounded by a configurable size cap, and honoring redaction (a redacted body is not served).

## What to implement

### Size cap option (`dashboard/options.go`)
- `WithAgentMaxBodyBytes(n uint64)` — default **64 KiB** when unset. When a body exceeds the cap, serve the first `n` bytes and signal truncation (a response header such as `X-Devlog-Truncated: true`, and set `truncated: true` in the body metadata of the detail DTO).

### Body endpoints (`dashboard/agent_api.go`)
- Reuse the type-switch logic of the existing `downloadRequestBody`/`downloadResponseBody` handlers (`dashboard/handler.go:429`, `:484`) to extract `*collector.Body` and content type from `HTTPServerRequest`/`HTTPClientRequest`. Other event types → 400, JSON error.
- Apply the size cap with truncation.
- **Redaction:** build the event's `EventDetail` and run the same redaction pipeline as Task 1 (header masking → `WithAgentRedactor`). If the redactor cleared the body's `available` flag (or returned `nil` for the whole event), the body endpoint returns **410 Gone** with `{"error":"body redacted"}`. Missing body (never captured) → 404.

### Finalize body metadata (`dashboard/agent_api_types.go`)
- The `requestBody`/`responseBody` metadata object is `{size, contentType, available, redacted}`. Define the redaction contract: a redactor that sets `available=false` (or `redacted=true`) on a body marks it as suppressed; the detail DTO reflects `redacted: true`, and the corresponding body endpoint returns 410. Keep `redacted` distinct from `available=false`-because-absent so an agent can tell "redacted" from "no body".

## Acceptance criteria & verification

Extend `dashboard/agent_api_test.go`:

| Test | Expectation |
|---|---|
| `TestAgentAPI_RequestBody_Raw` | Event with a JSON request body → 200, original content type, exact bytes |
| `TestAgentAPI_RequestBody_Truncated` | Body larger than the cap → bytes truncated to the cap, truncation signaled (header + metadata `truncated: true`) |
| `TestAgentAPI_Redactor_RedactsBody` | Redactor clearing request-body availability → detail shows `redacted: true`; the request-body endpoint returns 410 |

Also: a body never captured → 404; non-body event type → 400. `go vet ./...` clean; `go test ./dashboard/...` green.

## Out of scope
- Capture control / stats → Task 3. CLI/MCP → Tasks 4–5.
