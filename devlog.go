package devlog

import (
	"log/slog"

	"github.com/networkteam/devlog/collector"
)

type Instance struct {
	logCollector *collector.LogCollector
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

	instance := &Instance{
		logCollector: collector.NewLogCollector(options.LogCapacity),
	}
	return instance
}
