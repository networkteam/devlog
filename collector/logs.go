package collector

import (
	"context"
	"log/slog"
	"slices"

	"github.com/samber/lo"
)

type LogCollector struct {
	buffer         *RingBuffer[slog.Record]
	notifier       *Notifier[slog.Record]
	eventCollector *EventCollector
}

func (c *LogCollector) Collect(ctx context.Context, record slog.Record) {
	c.buffer.Add(record)
	c.notifier.Notify(record)
	if c.eventCollector != nil {
		c.eventCollector.CollectEvent(ctx, record)
	}
}

func (c *LogCollector) Tail(n int) []slog.Record {
	return c.buffer.GetRecords(uint64(n))
}

// Subscribe returns a channel that receives notifications of new log records
func (c *LogCollector) Subscribe(ctx context.Context) <-chan slog.Record {
	return c.notifier.Subscribe(ctx)
}

func NewLogCollector(capacity uint64) *LogCollector {
	return NewLogCollectorWithOptions(capacity, DefaultLogOptions())
}

func DefaultLogOptions() LogOptions {
	return LogOptions{}
}

type LogOptions struct {
	// NotifierOptions are options for notification about new logs
	NotifierOptions *NotifierOptions

	// EventCollector is an optional event collector for collecting logs as grouped events
	EventCollector *EventCollector
}

func NewLogCollectorWithOptions(capacity uint64, options LogOptions) *LogCollector {
	notifierOptions := DefaultNotifierOptions()
	if options.NotifierOptions != nil {
		notifierOptions = *options.NotifierOptions
	}

	return &LogCollector{
		buffer:         NewRingBuffer[slog.Record](capacity),
		notifier:       NewNotifierWithOptions[slog.Record](notifierOptions),
		eventCollector: options.EventCollector,
	}
}

// Close releases resources used by the collector
func (c *LogCollector) Close() {
	c.notifier.Close()
}

type CollectSlogLogsOptions struct {
	// Level is the minimum level of logs to collect.
	Level slog.Level
}

type SlogLogCollectorHandler struct {
	collector *LogCollector
	options   CollectSlogLogsOptions

	attrs  []slog.Attr
	groups []string
}

func NewSlogLogCollectorHandler(collector *LogCollector, options CollectSlogLogsOptions) *SlogLogCollectorHandler {
	return &SlogLogCollectorHandler{
		collector: collector,
		options:   options,

		attrs:  []slog.Attr{},
		groups: []string{},
	}
}

func (h *SlogLogCollectorHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.options.Level <= level
}

func (h *SlogLogCollectorHandler) Handle(ctx context.Context, record slog.Record) error {
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

	h.collector.Collect(ctx, newRecord)

	return nil
}

func (h *SlogLogCollectorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SlogLogCollectorHandler{
		collector: h.collector,
		options:   h.options,

		attrs:  appendAttrsToGroup(h.groups, h.attrs, attrs...),
		groups: h.groups,
	}
}

func (h *SlogLogCollectorHandler) WithGroup(name string) slog.Handler {
	// https://cs.opensource.google/go/x/exp/+/46b07846:slog/handler.go;l=247
	if name == "" {
		return h
	}

	return &SlogLogCollectorHandler{
		collector: h.collector,
		options:   h.options,

		attrs:  h.attrs,
		groups: append(h.groups, name),
	}
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
