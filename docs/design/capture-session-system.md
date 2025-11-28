# Technical Design: Capture Session System

## Goal

Enable on-demand event collection in Devlog. By default, no events are collected. Users can enable capture through the
dashboard UI. Each user gets their own isolated storage with a configurable capture mode:

1. **Session Mode**: Collects events only for HTTP requests with the user's session cookie
2. **Global Mode**: Collects all events (but stored per-user, so clearing doesn't affect others)

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              CAPTURE FLOW                                   │
│                         (Request → Event → Storage)                         │
└─────────────────────────────────────────────────────────────────────────────┘

    HTTP Request
         │
         ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                     HTTPServerCollector.Middleware                          │
│                                                                             │
│  1. Extract session cookie → ctx = WithSessionID(ctx, id)                   │
│  2. shouldCapture := aggregator.ShouldCapture(ctx)  ◄───────────────────┐   │
│                                                                         │   │
│  if !shouldCapture { next.ServeHTTP(w,r); return }   // early bailout   │   │
│                                                                         │   │
│  3. ctx = aggregator.StartEvent(ctx)  // adds GroupID                   │   │
│  4. Capture request body, wrap response writer                          │   │
│  5. next.ServeHTTP(wrappedWriter, r.WithContext(ctx))                   │   │
│  6. aggregator.EndEvent(ctx, httpServerRequest)                         │   │
└─────────────────────────────────────────────────────────────────────────────┘
         │                                                                 │
         │ ctx carries: SessionID + GroupID                                │
         ▼                                                                 │
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Child Collectors                                   │
│                   (DB Query, Log, HTTP Client)                              │
│                                                                             │
│  if !aggregator.ShouldCapture(ctx) { return }  ◄────────────────────────┤   │
│                                                                         │   │
│  aggregator.CollectEvent(ctx, data)                                     │   │
│  // Uses GroupID from ctx to link as child event                        │   │
└─────────────────────────────────────────────────────────────────────────────┘
         │                                                                │
         ▼                                                                │
┌─────────────────────────────────────────────────────────────────────────────┐
│                          EventAggregator                                    │
│                                                                             │
│  ShouldCapture(ctx) bool  ◄─────────────────────────────────────────────┘   │
│  - Iterates registered storages                                             │
│  - Returns true if any storage.ShouldCapture(ctx) returns true              │
│                                                                             │
│  RegisterStorage(s) / UnregisterStorage(id) / GetStorage(id)                │
│  StartEvent(ctx) / EndEvent(ctx, data) / CollectEvent(ctx, data)            │
└─────────────────────────────────────────────────────────────────────────────┘
         │
         │ Dispatch: for each storage that ShouldCapture(ctx)
         ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                      CaptureStorages (per user)                             │
│                                                                             │
│  ┌───────────────────┐  ┌───────────────────┐  ┌───────────────────┐        │
│  │ CaptureStorage    │  │ CaptureStorage    │  │ CaptureStorage    │        │
│  │ (User A)          │  │ (User B)          │  │ (User C)          │        │
│  ├───────────────────┤  ├───────────────────┤  ├───────────────────┤        │
│  │ sessionID: uuid-A │  │ sessionID: uuid-B │  │ sessionID: uuid-C │        │
│  │ mode: session     │  │ mode: global      │  │ mode: session     │        │
│  ├───────────────────┤  ├───────────────────┤  ├───────────────────┤        │
│  │ ShouldCapture:    │  │ ShouldCapture:    │  │ ShouldCapture:    │        │
│  │ ctx.session == A  │  │ return true       │  │ ctx.session == C  │        │
│  ├───────────────────┤  ├───────────────────┤  ├───────────────────┤        │
│  │ RingBuffer        │  │ RingBuffer        │  │ RingBuffer        │        │
│  │ Notifier ─────────│► │ Notifier ─────────│► │ Notifier ─────────│►       │
│  └───────────────────┘  └───────────────────┘  └───────────────────┘        │
└─────────────────────────────────────────────────────────────────────────────┘
         │                        │                        │
         │                        │                        │
         ▼                        ▼                        ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         NOTIFICATION FLOW                                   │
│                      (Storage → SSE → Dashboard)                            │
└─────────────────────────────────────────────────────────────────────────────┘
         │                        │                        │
         │ Subscribe(ctx)         │ Subscribe(ctx)         │ Subscribe(ctx)
         ▼                        ▼                        ▼
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  SSE Handler    │    │  SSE Handler    │    │  SSE Handler    │
│  (User A)       │    │  (User B)       │    │  (User C)       │
│                 │    │                 │    │                 │
│  GET /events-sse│    │GET /events-sse  │    │GET /events-sse  │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                        │                        │
         ▼                        ▼                        ▼
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Dashboard     │    │   Dashboard     │    │   Dashboard     │
│   (User A)      │    │   (User B)      │    │   (User C)      │
│   Session Mode  │    │   Global Mode   │    │   Session Mode  │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

## Data Flow Summary

### Capture Flow (Request → Storage)

1. **Middleware** extracts session cookie, adds to context
2. **Middleware** asks `aggregator.ShouldCapture(ctx)` - O(1) check
3. If capturing: do expensive work (body capture, response wrap)
4. **Aggregator** creates Event, dispatches to matching storages
5. **Storage** adds event pointer to its ring buffer

### Notification Flow (Storage → Dashboard)

1. **Storage** notifies its subscribers when event is added
2. **SSE Handler** receives notification, sends to browser
3. **Dashboard** updates event list in real-time

## EventAggregator

Central coordinator (no storage, just dispatch). Treats all storages uniformly via the `EventStorage` interface:

- `ShouldCapture(ctx)` - Iterates storages, returns true if any wants to capture
- `StartEvent(ctx)` / `EndEvent(ctx, data)` / `CollectEvent(ctx, data)` - Event lifecycle
- `RegisterStorage(s)` / `UnregisterStorage(id)` / `GetStorage(id)` - Uniform storage management

## EventStorage Interface

```go
type EventStorage interface {
    ID() uuid.UUID
    ShouldCapture(ctx context.Context) bool
    Add(event *Event)
    GetEvent(id uuid.UUID) (*Event, bool)
    GetEvents(limit uint64) []*Event
    Subscribe(ctx context.Context) <-chan *Event
    Clear()
    Close()
}
```

## CaptureStorage

Single storage type with configurable capture mode:

```go
type CaptureMode int
const (
    CaptureModeSession CaptureMode = iota  // only matching session
    CaptureModeGlobal                       // all requests
)
```

- `sessionID`: Identifies the storage owner (from cookie)
- `captureMode`: Session or Global
- `ShouldCapture(ctx)`:
  - If mode == session: return `ctx.sessionID == s.sessionID`
  - If mode == global: return `true`
- `SetCaptureMode(mode)` / `CaptureMode()`: Toggle capture mode
- Created when user clicks "Start Capture"
- Destroyed after idle timeout or explicit stop
- Each user gets their own storage (clearing is isolated)

## Capture Lifecycle

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          CAPTURE LIFECYCLE                                  │
└─────────────────────────────────────────────────────────────────────────────┘

START                         ACTIVE                           CLEANUP
  │                             │                                │
  ▼                             ▼                                ▼
┌──────────────┐         ┌─────────────┐                 ┌──────────────┐
│ POST         │ ─────── │ SSE         │ ─────────────── │ Idle Timeout │
│ /capture     │ cookie  │ connected   │   disconnect    │ (30s)        │
│ /start       │         │             │                 │              │
└──────────────┘         └─────────────┘                 └──────────────┘
       │                        │                               │
       ▼                        ▼                               ▼
  Create storage          Keep-alive via                  Unregister
  Register with           SSE connection                  storage from
  aggregator              (or explicit                    aggregator
  Set cookie              "Stop" button)                  → GC cleanup
                               │
                               ▼
                    ┌─────────────────────┐
                    │ POST /capture/mode  │
                    │ Toggle: session ↔   │
                    │ global              │
                    └─────────────────────┘
```

## Key Design Decisions

| Decision               | Choice                                      | Rationale                                            |
|------------------------|---------------------------------------------|------------------------------------------------------|
| Breaking API change    | Yes                                         | v0.2.1 exists, users use high-level `devlog` package |
| Dashboard skip         | Use SkipPaths                               | Add dashboard routes to HTTPServerOptions.SkipPaths  |
| Storage model          | Single CaptureStorage with mode per user    | Each user isolated, clearing doesn't affect others   |
| Storage-driven capture | Each storage decides via ShouldCapture(ctx) | Clean separation, extensible                         |
| Separate buffers       | Each storage has own RingBuffer             | Isolation, independent capacity                      |
| Shared events          | Storages hold *Event pointers               | Memory efficient, GC handles cleanup                 |
| Cookie scope           | Application root (`/`)                      | Cookie sent with all app requests                    |
| Cookie name            | `devlog_session`                            | Clear purpose, HttpOnly for security                 |
| UI placement           | Header bar                                  | Always visible, quick access                         |
| Idle timeout           | 30s after SSE disconnect                    | Allows page reload without losing session            |

## API Endpoints (New)

| Method | Path              | Purpose                                                 |
|--------|-------------------|---------------------------------------------------------|
| POST   | `/capture/start`  | Create CaptureStorage for user, set cookie, register    |
| POST   | `/capture/stop`   | Unregister storage, clear cookie                        |
| POST   | `/capture/mode`   | Set capture mode (session or global) for user's storage |
| GET    | `/capture/status` | Get current capture state (active, mode)                |

## Configuration

```go
type Options struct {
    // ... existing fields ...
    
    // Capture configuration (if nil, no capture by default)
    CaptureOptions *CaptureOptions
}

type CaptureOptions struct {
    StorageCapacity    uint64        // Events per storage (default: 1000)
    SessionIdleTimeout time.Duration // Cleanup delay after disconnect (default: 30s)
}
```

## Files to Modify

| File                            | Changes                                                                         |
|---------------------------------|---------------------------------------------------------------------------------|
| `collector/event_storage.go`    | **NEW** - EventStorage interface and implementations                            |
| `collector/event_aggregator.go` | **RENAME** from event_collector.go - storage registry, dispatch to storages     |
| `collector/session_context.go`  | **NEW** - Context helpers for session ID                                        |
| `collector/http_server.go`      | Extract cookie, add session ID to context                                       |
| `dashboard/handler.go`          | New endpoints, storage-aware event fetching                                     |
| `dashboard/views/header.templ`  | Capture control buttons in header                                               |
| `devlog.go`                     | CaptureOptions, storage initialization, rename EventCollector → EventAggregator |

## TDD Test Cases

### 1. CaptureStorage Tests (`collector/event_storage_test.go` - NEW)

| Test                                                       | Fixture                          | Action                             | Expectation               |
|------------------------------------------------------------|----------------------------------|------------------------------------|---------------------------|
| `TestCaptureStorage_ShouldCapture_SessionMode_NoSessionInCtx`  | CaptureStorage(session mode)     | ShouldCapture(ctx without session) | Returns false             |
| `TestCaptureStorage_ShouldCapture_SessionMode_WrongSession`    | CaptureStorage(uuid-A, session)  | ShouldCapture(ctx with uuid-B)     | Returns false             |
| `TestCaptureStorage_ShouldCapture_SessionMode_MatchingSession` | CaptureStorage(uuid-A, session)  | ShouldCapture(ctx with uuid-A)     | Returns true              |
| `TestCaptureStorage_ShouldCapture_GlobalMode_AlwaysTrue`       | CaptureStorage(uuid-A, global)   | ShouldCapture(any ctx)             | Returns true              |
| `TestCaptureStorage_SetCaptureMode`                            | CaptureStorage                   | SetCaptureMode(mode)               | CaptureMode() returns mode|
| `TestCaptureStorage_Add_StoresEvent`                           | CaptureStorage                   | Add(event)                         | GetEvents() returns event |
| `TestCaptureStorage_RingBuffer_Capacity`                       | CaptureStorage capacity=5        | Add 10 events                      | Only last 5 kept          |
| `TestCaptureStorage_Subscribe_ReceivesEvents`                  | CaptureStorage                   | Subscribe, Add event               | Channel receives event    |
| `TestCaptureStorage_GetEvent_ByID`                             | CaptureStorage with events       | GetEvent(id)                       | Returns correct event     |

### 2. EventAggregator Tests (`collector/event_aggregator_test.go` - ADAPT existing)

| Test                                                            | Fixture                                   | Action                                  | Expectation                |
|-----------------------------------------------------------------|-------------------------------------------|-----------------------------------------|----------------------------|
| `TestEventAggregator_ShouldCapture_NoStorages`                  | Aggregator without storages               | ShouldCapture(ctx)                      | Returns false              |
| `TestEventAggregator_ShouldCapture_SessionModeMatch`            | Aggregator + Storage(uuid-A, session)     | ShouldCapture(ctx with uuid-A)          | Returns true               |
| `TestEventAggregator_ShouldCapture_SessionModeNoMatch`          | Aggregator + Storage(uuid-A, session)     | ShouldCapture(ctx with uuid-B)          | Returns false              |
| `TestEventAggregator_ShouldCapture_GlobalMode`                  | Aggregator + Storage(uuid-A, global)      | ShouldCapture(any ctx)                  | Returns true               |
| `TestEventAggregator_CollectEvent_DispatchesToMatchingStorages` | Aggregator + Storage(A, session) + Storage(B, global) | CollectEvent(ctx with uuid-A) | Event in both storages     |
| `TestEventAggregator_CollectEvent_MultipleGlobalStorages`       | Aggregator + Storage(A, global) + Storage(B, global)  | CollectEvent(ctx)             | Event in both storages     |
| `TestEventAggregator_CollectEvent_NoCapture_NoDispatch`         | Aggregator + Storage(A, session)          | CollectEvent(ctx with uuid-B)           | No events stored           |
| `TestEventAggregator_RegisterUnregister_Storage`                | Aggregator                                | Register, Unregister                    | Storage added/removed      |
| `TestEventAggregator_StartEndEvent_WithCapture`                 | Aggregator + Storage(global)              | StartEvent, EndEvent                    | Event with timing stored   |
| `TestEventAggregator_StartEndEvent_NoCapture`                   | Aggregator (no storages)                  | StartEvent, EndEvent                    | No events stored           |
| `TestEventAggregator_NestedEvents_WithCapture`                  | Aggregator + Storage(global)              | Start parent, collect child, end parent | Parent has child           |

### 3. HTTPServerCollector Tests (`collector/http_server_test.go` - ADAPT)

| Test                                                           | Fixture                                        | Action                             | Expectation                       |
|----------------------------------------------------------------|------------------------------------------------|------------------------------------|-----------------------------------|
| `TestHTTPServer_Middleware_ExtractsCookie`                     | Middleware + Storage                           | Request with devlog_session cookie | Session ID in context             |
| `TestHTTPServer_Middleware_NoCookie_NoSessionInCtx`            | Middleware                                     | Request without cookie             | No session ID in context          |
| `TestHTTPServer_Middleware_NoCapture_SkipsExpensiveWork`       | Middleware + no storages                       | Request                            | No body capture, fast passthrough |
| `TestHTTPServer_Middleware_Capture_CapturesBody`               | Middleware + Storage(global)                   | Request with body                  | Body captured in event            |
| `TestHTTPServer_Middleware_SessionCapture_OnlyMatchingSession` | Middleware + Storage(A, session) + Storage(B, session) | Request with session A cookie | Event only in Storage A           |

### 4. Session Context Tests (`collector/session_context_test.go` - NEW)

| Test                              | Fixture              | Action                    | Expectation                       |
|-----------------------------------|----------------------|---------------------------|-----------------------------------|
| `TestWithSessionID_AddsToContext` | Empty context        | WithSessionID(ctx, uuid)  | SessionIDFromContext returns uuid |
| `TestSessionIDFromContext_NotSet` | Empty context        | SessionIDFromContext(ctx) | Returns uuid.Nil, false           |
| `TestSessionIDFromContext_Set`    | Context with session | SessionIDFromContext(ctx) | Returns correct uuid, true        |

### 5. Dashboard Handler Tests (`dashboard/handler_test.go` - NEW or ADAPT)

| Test                                         | Fixture                        | Action                      | Expectation                           |
|----------------------------------------------|--------------------------------|-----------------------------|---------------------------------------|
| `TestHandler_CaptureStart_CreatesStorage`    | Handler + Aggregator           | POST /capture/start         | CaptureStorage registered, cookie set |
| `TestHandler_CaptureStart_SetsCookie`        | Handler                        | POST /capture/start         | Response has devlog_session cookie    |
| `TestHandler_CaptureStop_UnregistersStorage` | Handler + active capture       | POST /capture/stop          | CaptureStorage unregistered           |
| `TestHandler_CaptureStop_ClearsCookie`       | Handler + active capture       | POST /capture/stop          | Cookie cleared (MaxAge=-1)            |
| `TestHandler_CaptureMode_SetsSessionMode`    | Handler + active capture       | POST /capture/mode (session)| Storage mode is session               |
| `TestHandler_CaptureMode_SetsGlobalMode`     | Handler + active capture       | POST /capture/mode (global) | Storage mode is global                |
| `TestHandler_CaptureStatus_ReturnsState`     | Handler                        | GET /capture/status         | JSON with active/mode state           |
| `TestHandler_EventsSSE_FromUserStorage`      | Handler + Storage with events  | GET /events-sse with cookie | Receives events from user's storage   |
| `TestHandler_EventList_FromUserStorage`      | Handler + Storage with events  | GET /event-list with cookie | Returns user's events                 |

### 6. Integration / E2E Tests (`devlog_e2e_test.go` - ADAPT)

| Test                                          | Fixture                          | Action                                      | Expectation                       |
|-----------------------------------------------|----------------------------------|---------------------------------------------|-----------------------------------|
| `TestE2E_NoCapture_ByDefault`                 | Devlog instance (default config) | Make HTTP request                           | No events captured                |
| `TestE2E_SessionMode_OnlyMatchingRequests`    | Devlog + capture (session mode)  | Requests with/without cookie                | Only cookie requests captured     |
| `TestE2E_GlobalMode_CapturesAll`              | Devlog + capture (global mode)   | Make HTTP requests                          | All requests captured             |
| `TestE2E_Capture_CapturesChildEvents`         | Devlog + capture                 | Request with DB query + log                 | HTTP + DB + log in same storage   |
| `TestE2E_Capture_Cleanup_AfterTimeout`        | Devlog + capture                 | Start capture, disconnect SSE, wait timeout | Storage cleaned up                |
| `TestE2E_MultipleUsers_IsolatedStorages`      | Devlog                           | Two users, each captures                    | Each has own storage, clear isolated |
| `TestE2E_MultipleUsers_GlobalMode_EachGetsAllEvents` | Devlog                    | Two users in global mode, make request      | Both get the event in their storage |
