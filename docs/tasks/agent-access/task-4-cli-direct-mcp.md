# Task 4 — CLI module + `--direct` client + MCP read tools

## Context

Phase 1 of **`docs/design/agent-access.md`** (read the "CLI (`./cli` module) — `--direct` mode" section and the MCP/CLI decision rows). This task creates the `devlog` CLI that exposes the Agent JSON API to AI agents over MCP, for **local development** — no browser, no pairing.

**Depends on Tasks 1 and 3** — the read endpoints (`/events`, `/events/{id}`) and capture/stats endpoints must exist, since the CLI drives a session and reads events through them.

Repository: `github.com/networkteam/devlog`. Verify with `go vet ./...` and `go test ./cli/...`.

## Goal

A new `./cli` Go module producing the `devlog` binary with a `relay` subcommand in `--direct` mode:

```
devlog relay --direct <base-url> [--mcp-port <port>]
```

It runs an MCP server on loopback that an agent (e.g. Claude Code) connects to, translating MCP tool calls into HTTP calls against `<base-url>/api/agent/v1/…` on the developer's local devlog instance.

## What to implement

### Module setup
- New module at `./cli` with its own `go.mod` (module path `github.com/networkteam/devlog/cli`). Add `./cli` to `go.work`.
- Add the MCP Go SDK dependency: `github.com/modelcontextprotocol/go-sdk`. **Before wiring, verify the exact server API** — read the SDK's `mcp` package docs / `examples/http` to confirm the constructor and registration calls (the spec references `StreamableHTTPHandler`, an `http.Handler` serving streamable MCP sessions). Use the real, current signatures; do not guess.
- CLI framework: `spf13/cobra` (already in the dependency graph). A single `relay` command is fine.

### `relay --direct` command
- Bind the MCP server to `127.0.0.1` only. Default the MCP port to a random ephemeral port; print the MCP URL on startup so the user can configure their agent.
- **Host-header hardening:** wrap the MCP `http.Handler` so requests whose `Host` is not a loopback literal (`127.0.0.1[:port]`, `localhost[:port]`, `[::1][:port]`) are rejected (e.g. 403). This blocks DNS-rebinding from a browser. (In Phase 2 the same wrapper protects the relay's MCP endpoint.)
- A "direct client": an HTTP client targeting `<base-url>/api/agent/v1/…`. On first use, generate a session UUIDv4 and start capture (`POST .../capture/start`); register a single synthetic environment named `local`.

### MCP read tools
Register these tools (each takes an optional `environment` parameter that defaults to the sole `local` environment in direct mode):
- `list_events` — params mirror the API filters (`type`, `since`, `until`, `limit`, `status`, `path`); returns summaries.
- `get_event` — `eventId`; returns the detail DTO.
- `get_stats` — returns the stats payload.
- `list_environments` — returns `[{name:"local", mode:...}]`.

Map tool results to MCP structured content. Body and capture-control tools are added in Task 5.

## Acceptance criteria & verification

Tests in `cli/direct_test.go` and `cli/mcp_test.go`. Spin up a real devlog instance with `httptest` (build it with `WithAgentAPI()`, drive fixture events through the collectors as in `devlog_e2e_test.go`), point the direct client at the test server's URL, and exercise MCP through the SDK's in-process client/server transport.

| Test | Expectation |
|---|---|
| `TestDirectMode_ListEvents` | Direct client against an httptest devlog instance: capture start → list → fixture events returned, no browser involved |
| `TestMCP_ListTools` | MCP `tools/list` → `list_events`, `get_event`, `get_stats`, `list_environments` present with schemas |
| `TestMCP_ListAndGetEvent` | `list_events` then `get_event` on a returned id → structured content matches the API payloads |
| `TestMCP_HostValidation` | A request to the MCP endpoint with a non-loopback `Host` header is rejected |

Also: `go vet ./...` clean; `go test ./cli/...` green; `go work` builds with the new module.

## Out of scope
- `get_request_body` / `get_response_body` / `capture_start` / `capture_stop` / `capture_status` MCP tools → Task 5.
- Browser relay, pairing, multiplexing across multiple environments → Phase 2.
