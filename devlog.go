package devlog

import (
	"log/slog"
	"net/http"

	"github.com/networkteam/devlog/collector"
	"github.com/networkteam/devlog/dashboard"
)

type Instance struct {
	logCollector        *collector.LogCollector
	httpClientCollector *collector.HTTPClientCollector
	httpServerCollector *collector.HTTPServerCollector
}

func (i *Instance) Close() {
	// Currently no-op
}

// Logs returns the most recent n logs.
func (i *Instance) Logs(n int) []slog.Record {
	return i.logCollector.Tail(n)
}

// CollectSlogLogs returns a slog.Handler that collects logs into devlog.
// You can use this handler with slog.New(slogmulti.Fanout(...)) to collect logs into devlog in addition to another slog handler.
func (i *Instance) CollectSlogLogs(options collector.CollectSlogLogsOptions) slog.Handler {
	return collector.NewSlogLogCollectorHandler(i.logCollector, options)
}

type Options struct {
	// LogCapacity is the maximum number of log entries to keep.
	// Default: 1000
	LogCapacity uint64

	// HTTPClientCapacity is the maximum number of HTTP client requests (outgoing) to keep.
	// Default: 1000
	HTTPClientCapacity uint64
	// HTTPClientOptions are the options for the HTTP client collector.
	// Default: nil, will use collector.DefaultHTTPClientOptions()
	HTTPClientOptions *collector.HTTPClientOptions

	// HTTPServerCapacity is the maximum number of HTTP server requests (incoming) to keep.
	// Default: 1000
	HTTPServerCapacity uint64
	// HTTPServerOptions are the options for the HTTP server collector.
	// Default: nil, will use collector.DefaultHTTPServerOptions()
	HTTPServerOptions *collector.HTTPServerOptions
}

// New creates a new devlog dashboard with default options.
func New() *Instance {
	return NewWithOptions(Options{})
}

// NewWithOptions creates a new devlog dashboard with the specified options.
func NewWithOptions(options Options) *Instance {
	if options.LogCapacity == 0 {
		options.LogCapacity = 1000
	}
	if options.HTTPClientCapacity == 0 {
		options.HTTPClientCapacity = 1000
	}
	if options.HTTPServerCapacity == 0 {
		options.HTTPServerCapacity = 1000
	}

	httpClientOptions := collector.DefaultHTTPClientOptions()
	if options.HTTPClientOptions != nil {
		httpClientOptions = *options.HTTPClientOptions
	}

	httpServerOptions := collector.DefaultHTTPServerOptions()
	if options.HTTPServerOptions != nil {
		httpServerOptions = *options.HTTPServerOptions
	}

	instance := &Instance{
		logCollector:        collector.NewLogCollector(options.LogCapacity),
		httpClientCollector: collector.NewHTTPClientCollectorWithOptions(options.HTTPClientCapacity, httpClientOptions),
		httpServerCollector: collector.NewHTTPServerCollectorWithOptions(options.HTTPServerCapacity, httpServerOptions),
	}
	return instance
}

func (i *Instance) DashboardHandler() http.Handler {
	return dashboard.NewHandler(
		dashboard.HandlerOptions{
			LogCollector:        i.logCollector,
			HTTPClientCollector: i.httpClientCollector,
			HTTPServerCollector: i.httpServerCollector,
		},
	)
}

// CollectHTTPClient wraps an http.RoundTripper to collect outgoing HTTP requests.
func (i *Instance) CollectHTTPClient(transport http.RoundTripper) http.RoundTripper {
	return i.httpClientCollector.Transport(transport)
}

// CollectHTTPServer wraps an http.Handler to collect incoming HTTP requests.
func (i *Instance) CollectHTTPServer(handler http.Handler) http.Handler {
	return i.httpServerCollector.Middleware(handler)
}
