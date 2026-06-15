# Task 5 — CLI MCP body + capture tools

## Context

Phase 1 of **`docs/design/agent-access.md`**. Completes the `--direct` MCP surface by adding the body-retrieval and capture-control tools.

**Depends on Tasks 2, 3, and 4** — the body endpoints (Task 2), capture/status endpoints (Task 3), and the CLI/MCP server + direct client + read tools (Task 4) must all exist. Read `docs/tasks/agent-access/task-4-cli-direct-mcp.md` for the established CLI patterns to extend.

Repository: `github.com/networkteam/devlog`. Verify with `go vet ./...` and `go test ./cli/...`.

## Goal

Add these MCP tools to the `relay --direct` server, each wrapping an already-tested API endpoint and accepting the optional `environment` parameter:

- `get_request_body` — `eventId`; fetches `…/events/{eventId}/request-body`. Returns the body (respecting the server's size cap / truncation marker); surfaces a redacted body (410) as a clear tool error/message, not a crash.
- `get_response_body` — `eventId`; fetches `…/events/{eventId}/response-body`, same handling.
- `capture_start` — `mode` (`session`|`global`); calls `…/capture/start`.
- `capture_stop` — calls `…/capture/stop`.
- `capture_status` — calls `…/capture/status`; returns `{active, mode, eventCount}`.

Note: in direct mode `capture_start` accepts `mode` (local dev). The Phase 2 relay locks the mode at pairing and removes the agent-supplied `mode` — do **not** implement that restriction here.

## What to implement

Extend the CLI's tool registration and direct client (from Task 4) with the five tools above. Map raw body bytes to MCP content sensibly (text content for textual content types; for binary, return metadata + a note rather than dumping raw bytes into the model — the size cap from Task 2 still applies server-side). Translate non-2xx API responses (404 missing, 410 redacted, 400 wrong type) into informative tool errors.

## Acceptance criteria & verification

Extend `cli/mcp_test.go` (same httptest + in-process MCP transport setup as Task 4):

| Test | Expectation |
|---|---|
| `TestMCP_CaptureControl` | `capture_start` → drive traffic → `list_events` shows the events; `capture_status` reflects `active`/`eventCount`; `capture_stop` stops capture |
| `TestMCP_GetRequestBody` | `get_request_body` for an event with a body → bytes returned via the tool |
| `TestMCP_GetBody_Redacted` | Server configured with a redactor suppressing a body → the body tool surfaces a redacted/410 result as a clear tool error, not a panic |

Also: `go vet ./...` clean; `go test ./cli/...` and `go test ./... -failfast` green.

## Out of scope
- Browser relay, pairing, body-approval dialogs, multiplexing → Phase 2 (`docs/design/agent-access-relay.md`).
