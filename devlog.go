package devlog

import (
	"context"
	"log/slog"
	"slices"

	"github.com/samber/lo"
)

type Instance struct {
	logCollector *LogCollector
}

type LogCollector struct {
	// Note: logs are hard-coded to slog.Record for now
	buffer *RingBuffer[slog.Record]
}

func NewLogCollector(capacity uint64) *LogCollector {
	return &LogCollector{
		buffer: NewRingBuffer[slog.Record](capacity),
	}
}

type CollectSlogLogsOptions struct {
	// Level is the minimum level of logs to collect.
	Level slog.Level
}

func (i *Instance) CollectSlogLogs(options CollectSlogLogsOptions) slog.Handler {
	return &slogLogCollectorHandler{
		instance: i,
		options:  options,
	}
}

func (i *Instance) Close() {

}

// Logs returns the most recent n logs.
func (i *Instance) Logs(n int) []slog.Record {
	return i.logCollector.buffer.GetRecords(uint64(n))
}

func New() *Instance {
	return &Instance{
		logCollector: NewLogCollector(1000), // TODO make this configurable via options
	}
}

// ---

type slogLogCollectorHandler struct {
	instance *Instance
	options  CollectSlogLogsOptions

	attrs  []slog.Attr
	groups []string
}

func (h *slogLogCollectorHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.options.Level <= level
}

func (h *slogLogCollectorHandler) Handle(ctx context.Context, record slog.Record) error {
	// Clone the record and add the handlers attributes to the new record.
	// I could not just do `record.AddAttrs(h.attrs...)` because h.Attrs must be added before record.Attrs.
	newRecord := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	newRecord.AddAttrs(h.attrs...)

	attrs := []slog.Attr{}
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})

	for i := range h.groups {
		k := h.groups[len(attrs)-1-i]
		v := attrs
		attrs = []slog.Attr{
			slog.Group(k, lo.ToAnySlice(v)...),
		}
	}
	newRecord.AddAttrs(attrs...)

	h.collectLog(newRecord)

	return nil
}

func (h *slogLogCollectorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &slogLogCollectorHandler{
		instance: h.instance,
		options:  h.options,

		attrs:  appendAttrsToGroup(h.groups, h.attrs, attrs...),
		groups: h.groups,
	}
}

func (h *slogLogCollectorHandler) WithGroup(name string) slog.Handler {
	// https://cs.opensource.google/go/x/exp/+/46b07846:slog/handler.go;l=247
	if name == "" {
		return h
	}

	return &slogLogCollectorHandler{
		instance: h.instance,
		options:  h.options,

		attrs:  h.attrs,
		groups: append(h.groups, name),
	}
}

func (h *slogLogCollectorHandler) collectLog(record slog.Record) {
	h.instance.logCollector.buffer.Add(record)
}

// Copied from github.com/samber/slog-mock
func appendAttrsToGroup(groups []string, actualAttrs []slog.Attr, newAttrs ...slog.Attr) []slog.Attr {
	actualAttrs = slices.Clone(actualAttrs)

	if len(groups) == 0 {
		return append(actualAttrs, newAttrs...)
	}

	for i := range actualAttrs {
		attr := actualAttrs[i]
		if attr.Key == groups[0] && attr.Value.Kind() == slog.KindGroup {
			actualAttrs[i] = slog.Group(groups[0], lo.ToAnySlice(appendAttrsToGroup(groups[1:], attr.Value.Group(), newAttrs...))...)
			return actualAttrs
		}
	}

	return append(
		actualAttrs,
		slog.Group(
			groups[0],
			lo.ToAnySlice(appendAttrsToGroup(groups[1:], []slog.Attr{}, newAttrs...))...,
		),
	)
}
