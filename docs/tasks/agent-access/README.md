# Task Breakdown: Agent Access to devlog — Phase 1 (JSON API & Local MCP)

Implements **[../../design/agent-access.md](../../design/agent-access.md)** — the Agent JSON API on the devlog dashboard handler plus a `devlog` CLI exposing it to agents over MCP for local development (`--direct` mode). Phase 2 (browser relay) is a separate spec ([agent-access-relay.md](../../design/agent-access-relay.md)) and is **not** covered here.

## Approach

Five vertical slices. The Agent JSON API is the foundation (verified directly via `httptest`, the way `devlog_e2e_test.go` works); the `devlog` CLI is a consumer layer over that API (verified via an in-process MCP client/server transport). Each task is independently mergeable and verified by the spec's own test cases — none is infrastructure-only.

Phase 1 touches **no `.templ` files**, so there is no `templ generate` step — it is pure Go plus the new `./cli` module.

A cross-cutting security note baked into the slicing: **default header redaction ships in Task 1**, not later — a read endpoint exposing headers without masking `Authorization`/`Cookie` by default would be insecure on first merge.

## Tasks (dependency order)

| # | Title | Depends on |
|---|---|---|
| 1 | [Agent API read endpoints (opt-in, list + detail, DTOs, redaction)](./task-1-agent-api-read.md) | — |
| 2 | [Body endpoints, size cap, body redaction](./task-2-body-endpoints.md) | 1 |
| 3 | [Capture control + stats endpoints](./task-3-capture-and-stats.md) | 1 |
| 4 | [CLI module + `--direct` client + MCP read tools](./task-4-cli-direct-mcp.md) | 1, 3 |
| 5 | [CLI MCP body + capture tools](./task-5-cli-body-capture-tools.md) | 2, 3, 4 |

Order: **1 → (2, 3) → 4 → 5**. Tasks 2 and 3 can proceed in parallel once 1 lands.

## Conventions (apply to every task)

- **No build step.** Verify compilation with `go vet ./...`; run tests with `go test ./... -failfast` (and `go test ./dashboard/...` / `go test ./cli/...` for the focused packages).
- **Functional options.** All new dashboard configuration is a `dashboard.HandlerOption` in `dashboard/options.go`, following the existing `WithStorageCapacity`/`WithPathPrefix` pattern. Embedders pass them through `dlog.DashboardHandler(prefix, opts...)` (`devlog.go:128`).
- **Reads go through per-session storage.** `Handler.sessions.Get(sid)` returns a `*collector.CaptureStorage` exposing `GetEvents(limit)`, `GetEvent(id)`, `CaptureMode()`, `IsCapturing()`, `SetCapturing(bool)`. The agent endpoints mirror the existing UI handlers in `dashboard/handler.go`.
- **Event data is a type switch.** `Event.Data` (`collector/event.go:20`) is one of `collector.HTTPServerRequest`, `collector.HTTPClientRequest`, `collector.DBQuery`, `slog.Record`. Follow the switch in `downloadRequestBody` (`dashboard/handler.go:453`).
- **Test fixtures.** Build an instance and drive events through the collectors as in `devlog_e2e_test.go`; for handler-level tests, construct sessions/storages as in `dashboard/session_manager_test.go`. Prefer table-driven tests.
- **Authorization** stays with the embedder's middleware; devlog adds none per-endpoint. The opt-in gate (`WithAgentAPI`) is the only access decision devlog makes.
