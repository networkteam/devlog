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
	// Error if any error occured
	Error error
}

type DBQueryCollector struct {
	buffer         *RingBuffer[DBQuery]
	notifier       *Notifier[DBQuery]
	eventCollector *EventCollector
}

func (c *DBQueryCollector) Collect(ctx context.Context, query DBQuery) {
	c.buffer.Add(query)
	c.notifier.Notify(query)
	if c.eventCollector != nil {
		c.eventCollector.CollectEvent(ctx, query)
	}
}

func (c *DBQueryCollector) Tail(n int) []DBQuery {
	return c.buffer.GetRecords(uint64(n))
}

// Subscribe returns a channel that receives notifications of new query records
func (c *DBQueryCollector) Subscribe(ctx context.Context) <-chan DBQuery {
	return c.notifier.Subscribe(ctx)
}

type DBQueryOptions struct {
	// NotifierOptions are options for notification about new queries
	NotifierOptions *NotifierOptions

	// EventCollector is an optional event collector for collecting logs as grouped events
	EventCollector *EventCollector
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

	return &DBQueryCollector{
		buffer:         NewRingBuffer[DBQuery](capacity),
		notifier:       NewNotifierWithOptions[DBQuery](notifierOptions),
		eventCollector: options.EventCollector,
	}
}

// Close releases resources used by the collector
func (c *DBQueryCollector) Close() {
	c.notifier.Close()
}
