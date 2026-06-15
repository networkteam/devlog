package dashboard

import "time"

// handlerOptions holds configuration for a dashboard Handler.
// This is unexported; use HandlerOption functions to configure.
type handlerOptions struct {
	// PathPrefix is where the handler is mounted (e.g. "/_devlog").
	PathPrefix string
	// TruncateAfter limits the number of events shown in the event list.
	TruncateAfter uint64
	// StorageCapacity is the number of events per user storage.
	StorageCapacity uint64
	// SessionIdleTimeout is how long to wait after SSE disconnect before cleanup.
	SessionIdleTimeout time.Duration
	// MaxSessions is the maximum number of concurrent sessions (0 = unlimited).
	MaxSessions int

	// AgentAPI enables the read-only agent JSON API under /api/agent/v1/.
	// Off by default.
	AgentAPI bool
	// AgentRedactor is an optional hook applied to each EventDetail before
	// encoding for the agent API.
	AgentRedactor AgentRedactor
	// AgentExtraRedactedHeaders are additional header names whose values are
	// masked in agent API responses, on top of the built-in defaults.
	AgentExtraRedactedHeaders []string
	// AgentInsecureHeaders disables all built-in header masking in the agent
	// API. Intended only for trusted local development.
	AgentInsecureHeaders bool
	// AgentMaxBodyBytes caps the number of body bytes served by the agent body
	// endpoints (0 = use DefaultAgentMaxBodyBytes).
	AgentMaxBodyBytes uint64
}

// HandlerOption configures a dashboard Handler.
type HandlerOption func(*handlerOptions)

// WithPathPrefix sets the path prefix where the handler is mounted.
// For example, "/_devlog" if mounted at that path.
// This is used for generating correct URLs in the dashboard.
func WithPathPrefix(prefix string) HandlerOption {
	return func(o *handlerOptions) {
		o.PathPrefix = prefix
	}
}

// WithStorageCapacity sets the number of events per user storage.
// Default is 1000 if not specified.
func WithStorageCapacity(capacity uint64) HandlerOption {
	return func(o *handlerOptions) {
		o.StorageCapacity = capacity
	}
}

// WithSessionIdleTimeout sets how long to wait after SSE disconnect before cleanup.
// Default is 30 seconds if not specified.
func WithSessionIdleTimeout(timeout time.Duration) HandlerOption {
	return func(o *handlerOptions) {
		o.SessionIdleTimeout = timeout
	}
}

// WithTruncateAfter limits the number of events shown in the event list.
// Default uses StorageCapacity if not specified.
func WithTruncateAfter(limit uint64) HandlerOption {
	return func(o *handlerOptions) {
		o.TruncateAfter = limit
	}
}

// WithMaxSessions sets the maximum number of concurrent sessions.
// Default is 0 (unlimited).
func WithMaxSessions(limit int) HandlerOption {
	return func(o *handlerOptions) {
		o.MaxSessions = limit
	}
}

// WithAgentAPI enables the read-only agent JSON API under /api/agent/v1/.
//
// The agent API is off by default: unlike the dashboard UI (which shows data to
// a human on screen), the agent API is consumed by tools that may forward data
// to third-party LLM providers, so it must be enabled deliberately. It inherits
// whatever auth middleware the embedder has placed in front of the dashboard.
func WithAgentAPI() HandlerOption {
	return func(o *handlerOptions) {
		o.AgentAPI = true
	}
}

// WithAgentRedactor sets a hook applied to each EventDetail before it is encoded
// for the agent API. The hook may mutate the detail and return it, or return nil
// to suppress the event entirely (omitted from lists, 404 on the detail endpoint).
// It runs after the built-in header masking.
func WithAgentRedactor(redactor AgentRedactor) HandlerOption {
	return func(o *handlerOptions) {
		o.AgentRedactor = redactor
	}
}

// WithAgentRedactedHeaders adds header names whose values are masked in agent API
// responses, on top of the built-in defaults (Authorization, Cookie, Set-Cookie,
// WWW-Authenticate, Proxy-Authenticate, Proxy-Authorization). Matching is
// case-insensitive.
func WithAgentRedactedHeaders(names ...string) HandlerOption {
	return func(o *handlerOptions) {
		o.AgentExtraRedactedHeaders = append(o.AgentExtraRedactedHeaders, names...)
	}
}

// WithAgentInsecureHeaders disables all built-in header masking in the agent API.
// This exposes Authorization, Cookie, and similar headers verbatim and should not
// be used outside trusted local development.
func WithAgentInsecureHeaders() HandlerOption {
	return func(o *handlerOptions) {
		o.AgentInsecureHeaders = true
	}
}

// WithAgentMaxBodyBytes caps the number of body bytes served by the agent body
// endpoints. Bodies larger than the cap are truncated to the cap and the response
// is marked truncated. Default is DefaultAgentMaxBodyBytes (64 KiB).
func WithAgentMaxBodyBytes(n uint64) HandlerOption {
	return func(o *handlerOptions) {
		o.AgentMaxBodyBytes = n
	}
}
