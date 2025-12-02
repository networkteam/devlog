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

// Size returns the estimated memory size of this query in bytes
func (q DBQuery) Size() uint64 {
	size := uint64(100) // base struct overhead
	size += uint64(len(q.Query))
	size += uint64(len(q.Language))
	// Estimate 50 bytes per arg (name + value)
	size += uint64(len(q.Args) * 50)
	return size
}

type DBQueryCollector struct {
	notifier        *Notifier[DBQuery]
	eventAggregator *EventAggregator
}

func (c *DBQueryCollector) Collect(ctx context.Context, query DBQuery) {
	c.notifier.Notify(query)
	if c.eventAggregator != nil {
		c.eventAggregator.CollectEvent(ctx, query)
	}
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

func NewDBQueryCollector() *DBQueryCollector {
	return NewDBQueryCollectorWithOptions(DefaultDBQueryOptions())
}

func NewDBQueryCollectorWithOptions(options DBQueryOptions) *DBQueryCollector {
	notifierOptions := DefaultNotifierOptions()
	if options.NotifierOptions != nil {
		notifierOptions = *options.NotifierOptions
	}

	return &DBQueryCollector{
		notifier:        NewNotifierWithOptions[DBQuery](notifierOptions),
		eventAggregator: options.EventAggregator,
	}
}

// Close releases resources used by the collector
func (c *DBQueryCollector) Close() {
	c.notifier.Close()
}
