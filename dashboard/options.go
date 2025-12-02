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
