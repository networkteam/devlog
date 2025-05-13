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
	eventCollector      *collector.EventCollector
}

func (i *Instance) Close() {
	i.logCollector.Close()
	i.httpClientCollector.Close()
	i.httpServerCollector.Close()
	i.eventCollector.Close()
}

type Options struct {
	// LogCapacity is the maximum number of log entries to keep.
	// Default: 1000
	LogCapacity uint64
	// LogOptions are the options for the log collector.
	// Default: nil, will use collector.DefaultLogOptions()
	LogOptions *collector.LogOptions

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

	// EventCapacity is the maximum number of events to keep.
	// Default: 1000
	EventCapacity uint64
	// EventOptions are the options for the event collector.
	// Default: nil, will use collector.DefaultEventOptions()
	EventOptions *collector.EventOptions
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
	if options.EventCapacity == 0 {
		options.EventCapacity = 1000
	}

	eventOptions := collector.DefaultEventOptions()
	if options.EventOptions != nil {
		eventOptions = *options.EventOptions
	}

	eventCollector := collector.NewEventCollectorWithOptions(options.EventCapacity, eventOptions)

	logOptions := collector.DefaultLogOptions()
	if options.LogOptions != nil {
		logOptions = *options.LogOptions
	}
	logOptions.EventCollector = eventCollector

	httpClientOptions := collector.DefaultHTTPClientOptions()
	if options.HTTPClientOptions != nil {
		httpClientOptions = *options.HTTPClientOptions
	}
	httpClientOptions.EventCollector = eventCollector

	httpServerOptions := collector.DefaultHTTPServerOptions()
	if options.HTTPServerOptions != nil {
		httpServerOptions = *options.HTTPServerOptions
	}
	httpServerOptions.EventCollector = eventCollector

	instance := &Instance{
		logCollector:        collector.NewLogCollectorWithOptions(options.LogCapacity, logOptions),
		httpClientCollector: collector.NewHTTPClientCollectorWithOptions(options.HTTPClientCapacity, httpClientOptions),
		httpServerCollector: collector.NewHTTPServerCollectorWithOptions(options.HTTPServerCapacity, httpServerOptions),
		eventCollector:      eventCollector,
	}
	return instance
}

// CollectSlogLogs returns a slog.Handler that collects logs into devlog.
//
// You can use this handler with slog.New(slogmulti.Fanout(...)) to collect logs into devlog in addition to another slog handler.
func (i *Instance) CollectSlogLogs(options collector.CollectSlogLogsOptions) slog.Handler {
	return collector.NewSlogLogCollectorHandler(i.logCollector, options)
}

// CollectHTTPClient wraps an http.RoundTripper to collect outgoing HTTP requests.
func (i *Instance) CollectHTTPClient(transport http.RoundTripper) http.RoundTripper {
	return i.httpClientCollector.Transport(transport)
}

// CollectHTTPServer wraps an http.Handler to collect incoming HTTP requests.
func (i *Instance) CollectHTTPServer(handler http.Handler) http.Handler {
	return i.httpServerCollector.Middleware(handler)
}

func (i *Instance) DashboardHandler(pathPrefix string) http.Handler {
	return dashboard.NewHandler(
		dashboard.HandlerOptions{
			LogCollector:        i.logCollector,
			HTTPClientCollector: i.httpClientCollector,
			HTTPServerCollector: i.httpServerCollector,
			EventCollector:      i.eventCollector,

			PathPrefix: pathPrefix,
		},
	)
}
