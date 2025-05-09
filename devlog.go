package devlog

import (
	"log/slog"

	"github.com/networkteam/devlog/collector"
)

type Instance struct {
	logCollector *collector.LogCollector
}

func (i *Instance) Close() {

}

// Logs returns the most recent n logs.
func (i *Instance) Logs(n int) []slog.Record {
	return i.logCollector.Tail(n)
}

func (i *Instance) CollectSlogLogs(options collector.CollectSlogLogsOptions) slog.Handler {
	return collector.NewSlogLogCollectorHandler(i.logCollector, options)
}

func New() *Instance {
	return &Instance{
		logCollector: collector.NewLogCollector(1000), // TODO make this configurable via options
	}
}

// ---
