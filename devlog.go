package devlog

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/networkteam/devlog/collector"
	"github.com/networkteam/devlog/dashboard"
)

type Instance struct {
	logCollector        *collector.LogCollector
	httpClientCollector *collector.HTTPClientCollector
	httpServerCollector *collector.HTTPServerCollector
	dbQueryCollector    *collector.DBQueryCollector
	eventAggregator     *collector.EventAggregator

	dashboardHandler *dashboard.Handler
}

func (i *Instance) Close() {
	i.logCollector.Close()
	i.httpClientCollector.Close()
	i.httpServerCollector.Close()
	i.dbQueryCollector.Close()
	if i.dashboardHandler != nil {
		i.dashboardHandler.Close()
	}
	i.eventAggregator.Close()
}

type Options struct {
	// LogCapacity is the maximum number of log entries to keep.
	// Default: 0
	LogCapacity uint64
	// LogOptions are the options for the log collector.
	// Default: nil, will use collector.DefaultLogOptions()
	LogOptions *collector.LogOptions

	// HTTPClientCapacity is the maximum number of HTTP client requests (outgoing) to keep.
	// Default: 0
	HTTPClientCapacity uint64
	// HTTPClientOptions are the options for the HTTP client collector.
	// Default: nil, will use collector.DefaultHTTPClientOptions()
	HTTPClientOptions *collector.HTTPClientOptions

	// HTTPServerCapacity is the maximum number of HTTP server requests (incoming) to keep.
	// Default: 0
	HTTPServerCapacity uint64
	// HTTPServerOptions are the options for the HTTP server collector.
	// Default: nil, will use collector.DefaultHTTPServerOptions()
	HTTPServerOptions *collector.HTTPServerOptions

	// DBQueryCapacity is the maximum number of database queries to keep.
	// Default: 0
	DBQueryCapacity uint64
	// DBQueryOptions are the options for the database query collector.
	// Default: nil, will use collector.DefaultDBQueryOptions()
	DBQueryOptions *collector.DBQueryOptions
}

// New creates a new devlog dashboard with default options.
func New() *Instance {
	return NewWithOptions(Options{})
}

// NewWithOptions creates a new devlog dashboard with the specified options.
// Default options are the zero value of Options.
//
// By default, no events are collected until a user starts a capture session
// through the dashboard. Events are collected per-user with isolation.
func NewWithOptions(options Options) *Instance {
	// Create the central EventAggregator (no storage by default)
	eventAggregator := collector.NewEventAggregator()

	logOptions := collector.DefaultLogOptions()
	if options.LogOptions != nil {
		logOptions = *options.LogOptions
	}
	logOptions.EventAggregator = eventAggregator

	httpClientOptions := collector.DefaultHTTPClientOptions()
	if options.HTTPClientOptions != nil {
		httpClientOptions = *options.HTTPClientOptions
	}
	httpClientOptions.EventAggregator = eventAggregator

	httpServerOptions := collector.DefaultHTTPServerOptions()
	if options.HTTPServerOptions != nil {
		httpServerOptions = *options.HTTPServerOptions
	}
	httpServerOptions.EventAggregator = eventAggregator

	dbQueryOptions := collector.DefaultDBQueryOptions()
	if options.DBQueryOptions != nil {
		dbQueryOptions = *options.DBQueryOptions
	}
	dbQueryOptions.EventAggregator = eventAggregator

	instance := &Instance{
		logCollector:        collector.NewLogCollectorWithOptions(options.LogCapacity, logOptions),
		httpClientCollector: collector.NewHTTPClientCollectorWithOptions(options.HTTPClientCapacity, httpClientOptions),
		httpServerCollector: collector.NewHTTPServerCollectorWithOptions(options.HTTPServerCapacity, httpServerOptions),
		dbQueryCollector:    collector.NewDBQueryCollectorWithOptions(options.DBQueryCapacity, dbQueryOptions),
		eventAggregator:     eventAggregator,
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

// CollectDBQuery allows to integrate an adapter to collect DB queries
func (i *Instance) CollectDBQuery() func(ctx context.Context, dbQuery collector.DBQuery) {
	return i.dbQueryCollector.Collect
}

func (i *Instance) DashboardHandler(pathPrefix string) http.Handler {
	handler := dashboard.NewHandler(
		dashboard.HandlerOptions{
			EventAggregator: i.eventAggregator,

			PathPrefix: pathPrefix,
		},
	)
	i.dashboardHandler = handler
	return handler
}
