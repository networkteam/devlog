package sqlloggeradapter

import (
	"context"
	"database/sql/driver"
	"time"

	"github.com/networkteam/go-sqllogger"

	"github.com/networkteam/devlog/collector"
)

func New(collect func(ctx context.Context, dbQuery collector.DBQuery)) sqllogger.SQLLogger {
	return &adapter{collect: collect}
}

type adapter struct {
	collect func(ctx context.Context, dbQuery collector.DBQuery)
}

// ConnBegin implements sqllogger.SQLLogger.
func (a *adapter) ConnBegin(ctx context.Context, connID int64, txID int64, opts driver.TxOptions) {
}

// ConnClose implements sqllogger.SQLLogger.
func (a *adapter) ConnClose(ctx context.Context, connID int64) {
}

// ConnExec implements sqllogger.SQLLogger.
func (a *adapter) ConnExec(ctx context.Context, connID int64, query string, args []driver.Value) {
	timestamp, duration := timingFromContext(ctx)

	a.collect(ctx, collector.DBQuery{
		Query:     query,
		Args:      toNamedValues(args),
		Timestamp: timestamp,
		Duration:  duration,
	})
}

// ConnExecContext implements sqllogger.SQLLogger.
func (a *adapter) ConnExecContext(ctx context.Context, connID int64, query string, args []driver.NamedValue) {
	timestamp, duration := timingFromContext(ctx)

	a.collect(ctx, collector.DBQuery{
		Query:     query,
		Args:      args,
		Timestamp: timestamp,
		Duration:  duration,
	})
}

// ConnPrepare implements sqllogger.SQLLogger.
func (a *adapter) ConnPrepare(ctx context.Context, connID int64, stmtID int64, query string) {
}

// ConnPrepareContext implements sqllogger.SQLLogger.
func (a *adapter) ConnPrepareContext(ctx context.Context, connID int64, stmtID int64, query string) {

}

// ConnQuery implements sqllogger.SQLLogger.
func (a *adapter) ConnQuery(ctx context.Context, connID int64, rowsID int64, query string, args []driver.Value) {
	timestamp, duration := timingFromContext(ctx)

	a.collect(ctx, collector.DBQuery{
		Query:     query,
		Args:      toNamedValues(args),
		Timestamp: timestamp,
		Duration:  duration,
	})
}

// ConnQueryContext implements sqllogger.SQLLogger.
func (a *adapter) ConnQueryContext(ctx context.Context, connID int64, rowsID int64, query string, args []driver.NamedValue) {
	timestamp, duration := timingFromContext(ctx)

	a.collect(ctx, collector.DBQuery{
		Query:     query,
		Args:      args,
		Timestamp: timestamp,
		Duration:  duration,
	})
}

// Connect implements sqllogger.SQLLogger.
func (a *adapter) Connect(ctx context.Context, connID int64) {
}

// RowsClose implements sqllogger.SQLLogger.
func (a *adapter) RowsClose(ctx context.Context, rowsID int64) {
}

// StmtClose implements sqllogger.SQLLogger.
func (a *adapter) StmtClose(ctx context.Context, stmtID int64) {
}

// StmtExec implements sqllogger.SQLLogger.
func (a *adapter) StmtExec(ctx context.Context, stmtID int64, query string, args []driver.Value) {
	a.collect(ctx, collector.DBQuery{
		Query:     query,
		Args:      toNamedValues(args),
		Timestamp: time.Now(),
	})

}

// StmtExecContext implements sqllogger.SQLLogger.
func (a *adapter) StmtExecContext(ctx context.Context, stmtID int64, query string, args []driver.NamedValue) {
	a.collect(ctx, collector.DBQuery{
		Query:     query,
		Args:      args,
		Timestamp: time.Now(),
	})
}

// StmtQuery implements sqllogger.SQLLogger.
func (a *adapter) StmtQuery(ctx context.Context, stmtID int64, rowsID int64, query string, args []driver.Value) {
	timestamp, duration := timingFromContext(ctx)

	a.collect(ctx, collector.DBQuery{
		Query:     query,
		Args:      toNamedValues(args),
		Timestamp: timestamp,
		Duration:  duration,
	})
}

// StmtQueryContext implements sqllogger.SQLLogger.
func (a *adapter) StmtQueryContext(ctx context.Context, stmtID int64, rowsID int64, query string, args []driver.NamedValue) {
	timestamp, duration := timingFromContext(ctx)

	a.collect(ctx, collector.DBQuery{
		Query:     query,
		Args:      args,
		Timestamp: timestamp,
		Duration:  duration,
	})
}

// TxCommit implements sqllogger.SQLLogger.
func (a *adapter) TxCommit(ctx context.Context, txID int64) {
}

// TxRollback implements sqllogger.SQLLogger.
func (a *adapter) TxRollback(ctx context.Context, txID int64) {
}

var _ sqllogger.SQLLogger = &adapter{}

func toNamedValues(args []driver.Value) []driver.NamedValue {
	var collectedArgs []driver.NamedValue
	for i, arg := range args {
		collectedArgs = append(collectedArgs, driver.NamedValue{
			Ordinal: i + 1,
			Value:   arg,
		})
	}
	return collectedArgs
}

func timingFromContext(ctx context.Context) (time.Time, time.Duration) {
	timing, ok := sqllogger.GetTiming(ctx)
	if !ok {
		return time.Now(), 0
	}

	return timing.Start, timing.End.Sub(timing.Start)
}
