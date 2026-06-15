# Technical Design: Agent Access to devlog — Browser Relay (Phase 2)

Based on [discussion #10](https://github.com/networkteam/devlog/discussions/10), including the author's follow-up comments (CLI-first pairing, multiplexing). Builds on the Agent JSON API defined in **[Phase 1: agent-access.md](./agent-access.md)** — read that first.

## Goals & Requirements

Let an AI agent inspect devlog data on a **deployed (stage/production)** instance, so a developer debugging a production bug can give the agent the same request/SQL/log context they'd read in the dashboard — complementing the Sentry MCP. The agent reaches the deployed instance **through the developer's own authenticated browser tab**; devlog never gains its own agent auth and never validates IdP tokens.

Because this exposes captured production traffic to an LLM, Phase 2 is security-sensitive. The design centers on five guarantees:

1. **The developer, not the agent, controls capture.** The agent has **no** capture-control tools at all — start/stop and the session vs global mode are managed entirely by the developer in the dashboard UI. The agent only attaches to (piggy-backs on) the developer's existing session.
2. **Bodies are never sent without explicit per-request human approval** in the dashboard.
3. **Opt-in.** The relay-facing surface is off unless the embedder enables it.
4. **The browser tab owns the connection's lifetime.** Closing the tab tears everything down immediately.
5. **The local MCP/relay endpoints are hardened** against other local processes and DNS-rebinding.

**Out of scope:** push/streaming channels to the agent, scoped writes beyond capture control, broker transport.

## High-Level Architecture

```
 deployed instance                       developer machine
┌─────────────────────────┐             ┌──────────────────────────────────────┐
│ Host app                │             │  Browser tab (authenticated)         │
│  └ auth middleware      │   HTTPS     │   devlog dashboard                   │
│     └ devlog dashboard  │◄────────────│   + agent-relay.js                   │
│        └ /api/agent/v1/ │  fetch()    │     · forwards ONLY /api/agent/v1/*  │
│          (JSON+redact,  │  w/ creds   │     · shows body-approval dialogs    │
│           Phase 1)      │             │     · stop beacon on pagehide        │
└─────────────────────────┘             └───────────────┬──────────────────────┘
                                          ▲              │ WebSocket (loopback)
                            body-approval  │             ▼
                            request frame  │  ┌──────────────────────────────────────┐
                                           └──│  devlog relay   (CLI, 127.0.0.1 only)│
                                              │   · HMAC pairing (binds sid+origin)  │
                                              │   · connection registry by origin    │
                                              │   · MCP server (streamable HTTP)     │
                                              └───────────────┬──────────────────────┘
                                                              │ MCP
                                                              ▼
                                                        AI agent (Claude Code, …)
```

Every forwarded request passes through the embedder's auth middleware exactly like a dashboard click. The tab is the credential holder *and* the lifetime owner.

## Architecture & Design Decisions

| Decision | Choice | Rationale / Grounding |
|---|---|---|
| Relay surface availability | Opt-in: dashboard relay UI (button/dialog/badge + `agent-relay.js`) rendered only when the embedder sets `dashboard.WithAgentRelay()`. Independent of Phase 1's `WithAgentAPI()`, which it requires | Egress-sensitive; conservative default (Guarantee 3) |
| **No agent capture control** | The agent has no `capture_start`/`capture_stop` tools (same as Phase 1). The developer starts/stops capture and chooses session vs global **in the dashboard**, then connects the agent, which attaches to that session via its `sid`. The agent only reads (`capture_status` plus the events/bodies/stats reads). | Closes the sharpest risk — in global mode a session captures *all* users' traffic (`capture-session-system.md`) — by keeping capture entirely a human, dashboard-side decision. No mode-locking, mode-in-handshake, or escalation checks are needed (Guarantee 1). |
| **Body retrieval requires dashboard approval** | `get_request_body` / `get_response_body` do not fetch immediately. The CLI sends a body-approval request frame to the paired tab; the dashboard shows an "Agent requests the {request\|response} body for `{method} {path}` — Allow once / Allow for this session / Deny" dialog. Only on Allow does the CLI forward the body fetch. Deny or 30 s timeout → the MCP tool returns an error to the agent | Bodies are the highest-value, highest-risk payload; a human gate at the authenticated surface prevents silent bulk exfiltration (Guarantee 2). Independent of any agent-client-side tool approval, which can be allowlisted away |
| **Tab = lifetime owner** | Three teardown layers (see below) ensure no agent activity outlives the tab | Guarantee 4 |
| MCP endpoint hardening | `/mcp` rejects requests whose `Host` is not a loopback literal (DNS-rebinding defense); the WS `/relay` endpoint validates `Origin` against paired/`--allow-origin` origins via `websocket.AcceptOptions.OriginPatterns` | Guarantee 5. The MCP endpoint is otherwise unauthenticated on loopback (standard); the SDK's `StreamableHTTPHandler` also tracks session IDs to prevent hijacking |
| Pairing protocol | CLI-first HMAC mutual challenge–response; secret never transmitted; precise wire format below | RFC author's follow-up: avoids fixed-port squatting and secret exposure via argv/shell history |
| Multiplexing | CLI keeps a connection registry keyed by page origin; MCP tools take an optional `environment` parameter (origin hostname), defaulting to the single connection when only one exists | RFC author's follow-up; **kept** — we need to compare stage vs production in one agent session |
| WebSocket library (CLI side) | `github.com/coder/websocket` (ISC) | Research: actively maintained (v1.8.14 Sep 2025, commits through Mar 2026), native `context.Context`, `wsjson` helpers, zero deps, safe concurrent writes, `OriginPatterns` origin check built in ([pkg.go.dev](https://pkg.go.dev/github.com/coder/websocket), [comparison](https://websocket.org/guides/languages/go/)) |
| Browser relay JS | Vanilla JS `dashboard/static/agent-relay.js`, dialog + button + approval modal in `header.templ` | Convention: dashboard uses templ + static assets (`dashboard/static/assets.go`), no JS framework |
| Loopback / mixed-content model | No PNA/CORS preflight handling; rely on loopback being "potentially trustworthy" | Research: Chrome's PNA preflights were replaced by the Local Network Access *permission prompt* in Chrome 142 (Oct 2025); WebSockets are not yet gated by LNA (crbug.com/421156866), so the relay works prompt-free today; when WS is gated, the IP-literal target yields a one-time prompt, no code change ([Chrome blog](https://developer.chrome.com/blog/local-network-access)) |
| Browser support | Chrome/Edge/Firefox supported; Safari unsupported — the dialog detects Safari and shows a hint | Research: `ws://127.0.0.1` from HTTPS is not mixed content in Chrome/Firefox (Firefox since 2020, [bug 1376309](https://bugzilla.mozilla.org/show_bug.cgi?id=1376309)); WebKit still blocks loopback as mixed content ([bug 171934](https://bugs.webkit.org/show_bug.cgi?id=171934), open mid-2026). No `wss://` self-signed workaround is designed |

## Pairing & Handshake (precise wire format)

Transport is `ws://127.0.0.1:<port>` — unencrypted loopback. The protocol therefore never transmits the secret and binds every security-relevant field into the MAC.

**Connect string** (printed by the CLI): `dlr1:<port>:<base64url(S)>` where `S` is 32 bytes from a CSPRNG (`crypto/rand`). `S` lives for the CLI process lifetime and is reused across origin pairings. The developer pastes this into the dashboard dialog. (Capture mode is not chosen here — it is whatever the developer already set when starting capture in the dashboard.)

**Handshake** — all frames JSON; `||` denotes length-prefixed concatenation; MAC is HMAC-SHA256 over the byte transcript:

1. **CLI → Page** `{"t":"challenge","ns":"<base64url(Ns)>"}` — `Ns` = 32 random bytes (`crypto/rand`).
2. **Page → CLI** `{"t":"auth","nc":"<base64url(Nc)>","origin":"<page origin>","sid":"<dashboard sid>","mac":"<base64url(Mc)>"}` — `Nc` = 32 random bytes (`crypto.getRandomValues`); `Mc = HMAC(S, "devlog-relay-v1:client" || Ns || Nc || origin || sid)`.
3. **CLI** verifies `Mc` in constant time (`hmac.Equal`) and that `origin` matches the allowlist. On success **CLI → Page** `{"t":"auth-ok","mac":"<base64url(Ms)>"}` — `Ms = HMAC(S, "devlog-relay-v1:server" || Ns || Nc)`.
4. **Page** verifies `Ms`. Only now is the channel trusted by both ends.

Properties:

- **Secret never on the wire** — only HMACs of fresh nonces; HMAC preimage resistance protects `S` from a loopback eavesdropper (who is already a privileged local attacker).
- **Transcript binding** — `origin` and `sid` are inside `Mc`, so a tampered origin or sid fails verification (the agent reads exactly the session the page authenticated for).
- **Mutual auth & no reflection** — distinct domain-separation labels (`:client` / `:server`) prevent reflecting the server's challenge back as a client response.
- **Replay-resistant** — fresh `Ns`+`Nc` per handshake; a replayed `auth` fails against a new `Ns`.
- **Bounded** — 5 s handshake timeout; any field mismatch, bad MAC, or disallowed origin closes the socket. Constant-time comparison on both ends.

The CLI records `{origin → {ws conn, dashboard sid}}` in the registry. Re-pairing the same origin replaces the entry.

## Tab Lifetime & Teardown

**Invariant: the agent has no path to devlog except through a live browser tab.** Every agent request is a `fetch()` issued by that tab; there is no other route. Two layers enforce prompt teardown when the tab closes:

1. **WS close → environment dropped.** When the page's WebSocket closes (tab close, reload, navigation), the CLI removes that origin from the registry immediately. MCP tools targeting it return `environment disconnected`. This is the relay-specific teardown — the agent loses all access the instant the tab goes away.
2. **Dashboard session lifecycle → capture ends.** The session belongs to the dashboard tab, not the relay. Its existing lifecycle applies unchanged: the SSE keep-alive stops when the tab closes, and the `SessionManager` idle timeout then reclaims the session (stopping capture). The relay never starts or stops capture, so it adds no capture-control beacon of its own.

The "Agent connected" badge is driven by the live WS, so it disappears exactly when access ends.

## Implementation Changes

### CLI (`./cli` module) — browser-relay mode

`devlog relay [--mcp-port <port>] [--relay-port <port>] [--allow-origin <origin>...]` (no `--direct`)

- Binds `127.0.0.1` only; one HTTP server with `/relay` (WebSocket for browser tabs) and `/mcp` (MCP `StreamableHTTPHandler`). Random ephemeral ports by default; prints connect string + MCP URL on startup.
- Runs the pairing handshake on each new `/relay` socket; maintains the origin registry.
- Forwarding frames (JSON over WS): `{id, method, path, body?}` → `{id, status, contentType, body}` (`body` base64 for binary). **Path must start with `/api/agent/v1/`** and must not contain `..` after normalization — enforced on **both** sides (page allowlist is the real security boundary, since the page holds the credentials; CLI never emits other paths).
- Body-approval frames: `{id, t:"approve-body", which, method, path}` → `{id, t:"approve-body-result", decision}` before any body fetch is forwarded.
- MCP tools as in Phase 1 (all read-only; no `capture_start`/`capture_stop`), with the `environment` parameter now meaningful. `list_environments` reports each origin and its attached `sid`.

### Dashboard (server side)

- `dashboard/static/agent-relay.js` — **NEW**: parses connect string, opens `ws://127.0.0.1:<port>/relay`, runs the handshake (sending the tab's `sid`), forwards allowlisted frames via `fetch(pathPrefix + path, {credentials:'same-origin'})`, renders body-approval modals, auto-reconnects with backoff while connected, exposes a disconnect button. (It does not touch capture — that stays in the dashboard's own controls.)
- `dashboard/views/header.templ` — **NEW** UI: "Connect agent" button, pairing dialog (connect-string input + Safari hint), body-approval modal, persistent "Agent connected" badge.
- `dashboard/options.go` — `WithAgentRelay()` (gates rendering of the relay UI; requires `WithAgentAPI()`).
- `dashboard/handler.go` — serve `agent-relay.js`; render relay UI only under `WithAgentRelay()`.

### Files to Modify

| File | Changes |
|---|---|
| `cli/` | Relay (browser) mode: pairing, registry, forwarding, body-approval round-trip, Origin/Host hardening |
| `dashboard/static/agent-relay.js` | **NEW** — pairing + forwarding + approval + teardown client |
| `dashboard/views/header.templ` | Connect-agent button, pairing dialog, approval modal, connected badge |
| `dashboard/options.go` | `WithAgentRelay` |
| `dashboard/handler.go` | Serve relay JS; conditional relay UI |

## Test Cases

### 1. CLI pairing & forwarding (`cli/relay_test.go` — NEW)

| Test | Fixture | Action | Expectation |
|---|---|---|---|
| `TestPairing_ValidSecret` | Relay + test WS client with secret | Full handshake | Paired; registry records origin + attached `sid` |
| `TestPairing_WrongSecret` | Relay | Handshake with wrong `Mc` | Socket closed, not registered |
| `TestPairing_TamperedSid` | Relay | Valid-looking `auth` but `sid` altered after MAC | MAC verification fails, socket closed |
| `TestPairing_DisallowedOrigin` | Relay with `--allow-origin` | Handshake from other origin | Rejected |
| `TestForwarding_RoundTrip` | Paired fake browser echoing frames | Forward GET events | Frame correlation by `id`, body decoded |
| `TestForwarding_RejectsNonAllowlistedPath` | Paired client | Internal request to `/s/{sid}/event-list` and `/api/agent/v1/../x` | Refused before emitting a frame |
| `TestNoCaptureControlTools` | Paired client | MCP `tools/list` | `capture_start`/`capture_stop` absent |
| `TestRegistry_MultipleOrigins` | Two paired clients (origins A, B) | Forward with `environment=B` | Routed to B; missing param with 2 origins → error listing environments |
| `TestTeardown_WSCloseDropsEnvironment` | Paired client | Close WS | Environment removed immediately; subsequent tool call errors `disconnected` |
| `TestMCP_HostValidation` | Relay MCP server | Request with non-loopback `Host` | Rejected |
| `TestMCP_WSOriginValidation` | Relay `/relay` | WS upgrade with bad `Origin` | Rejected |

### 2. Body approval (`cli/approval_test.go` — NEW)

| Test | Fixture | Action | Expectation |
|---|---|---|---|
| `TestBodyApproval_Allowed` | Paired client auto-approving | `get_request_body` | Approval frame sent; body fetched and returned |
| `TestBodyApproval_Denied` | Paired client denying | `get_request_body` | No body fetch forwarded; tool returns error |
| `TestBodyApproval_Timeout` | Paired client that never answers | `get_request_body` | Tool errors after 30 s; no fetch |
| `TestBodyApproval_AllowForSession` | Paired client approving "for session" | Two `get_request_body` calls | One approval prompt; both bodies served |

### 3. Acceptance / E2E (`acceptance/agent_relay_test.go` — NEW; reuse `testapp.go` + playwright harness)

| Test | Fixture | Action | Expectation |
|---|---|---|---|
| `TestAgentRelay_EndToEnd` | Test app + dashboard in browser + `devlog relay` in-process | Connect agent (mode=session), generate traffic, MCP `list_events`, then `get_request_body` with dashboard approval | Events delivered through tunnel; badge visible; body served only after approval |
| `TestAgentRelay_TabCloseTearsDown` | As above, paired and capturing | Close the dashboard tab | Capture stops promptly (stop beacon), badge gone, agent tool calls error |

## Notes on Residual Risk (accepted)

- A privileged local attacker who can read loopback traffic sees nonces/MACs but cannot derive the ephemeral secret or forge a handshake; `Host`/`Origin` checks block the realistic browser/DNS-rebinding vectors. This is the standard local-MCP threat model.
- `global` mode still captures all users' traffic — but only when the developer chooses it in the dashboard; the agent cannot. Default header redaction, metadata-only event listings + the body size cap, and per-body human approval bound what reaches the LLM. Embedders handling regulated data should add a `WithAgentRedactor` and consider leaving the relay off for production.
