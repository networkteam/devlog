package collector

import (
	"context"
	"database/sql/driver"
	"time"
)

// DBQuery represents a database query execution record
type DBQuery struct {
	// Query is the SQL query or statement
	Query string
	// Args of the query or statement
	Args []driver.NamedValue
	// Duration of executing the query or statement
	Duration time.Duration
	// Timestamp when the query or statement was started
	Timestamp time.Time
	// SQL dialect / language for highlighting and formatting
	Language string
	// Error if any error occurred
	Error error
}

type DBQueryCollector struct {
	buffer          *RingBuffer[DBQuery]
	notifier        *Notifier[DBQuery]
	eventAggregator *EventAggregator
}

func (c *DBQueryCollector) Collect(ctx context.Context, query DBQuery) {
	if c.buffer != nil {
		c.buffer.Add(query)
	}
	c.notifier.Notify(query)
	if c.eventAggregator != nil {
		c.eventAggregator.CollectEvent(ctx, query)
	}
}

func (c *DBQueryCollector) Tail(n int) []DBQuery {
	if c.buffer == nil {
		return nil
	}
	return c.buffer.GetRecords(uint64(n))
}

// Subscribe returns a channel that receives notifications of new query records
func (c *DBQueryCollector) Subscribe(ctx context.Context) <-chan DBQuery {
	return c.notifier.Subscribe(ctx)
}

type DBQueryOptions struct {
	// NotifierOptions are options for notification about new queries
	NotifierOptions *NotifierOptions

	// EventAggregator is the aggregator for collecting queries as grouped events
	EventAggregator *EventAggregator
}

func DefaultDBQueryOptions() DBQueryOptions {
	return DBQueryOptions{}
}

func NewDBQueryCollector(capacity uint64) *DBQueryCollector {
	return NewDBQueryCollectorWithOptions(capacity, DefaultDBQueryOptions())
}

func NewDBQueryCollectorWithOptions(capacity uint64, options DBQueryOptions) *DBQueryCollector {
	notifierOptions := DefaultNotifierOptions()
	if options.NotifierOptions != nil {
		notifierOptions = *options.NotifierOptions
	}

	collector := &DBQueryCollector{
		notifier:        NewNotifierWithOptions[DBQuery](notifierOptions),
		eventAggregator: options.EventAggregator,
	}
	if capacity > 0 {
		collector.buffer = NewRingBuffer[DBQuery](capacity)
	}

	return collector
}

// Close releases resources used by the collector
func (c *DBQueryCollector) Close() {
	c.notifier.Close()
	c.buffer = nil
}
