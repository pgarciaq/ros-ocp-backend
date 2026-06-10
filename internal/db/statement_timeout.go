package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

type statementExecer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// StatementTimeoutSecs returns the session-level statement timeout for API/GORM paths.
func StatementTimeoutSecs() int {
	cfg := config.GetConfig()
	if cfg == nil || cfg.DBStatementTimeoutSecs <= 0 {
		return 25
	}
	return cfg.DBStatementTimeoutSecs
}

// IngestStatementTimeoutSecs returns the per-transaction timeout for ingestion batch writes.
func IngestStatementTimeoutSecs() int {
	cfg := config.GetConfig()
	if cfg == nil || cfg.DBIngestStatementTimeoutSecs <= 0 {
		return 120
	}
	return cfg.DBIngestStatementTimeoutSecs
}

// SetLocalIngestStatementTimeout raises statement_timeout for the current transaction only
// via SET LOCAL. The override resets automatically when the transaction ends.
func SetLocalIngestStatementTimeout(ctx context.Context, conn statementExecer) error {
	secs := IngestStatementTimeoutSecs()
	_, err := conn.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = '%ds'", secs))
	return err
}

// QueryStatementTimeoutMillis returns the active statement_timeout in milliseconds.
func QueryStatementTimeoutMillis(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) int64 {
	var ms int64
	err := q.QueryRow(ctx,
		`SELECT CAST(extract(epoch from current_setting('statement_timeout')::interval) * 1000 AS bigint)`).Scan(&ms)
	if err != nil {
		panic(fmt.Sprintf("query statement_timeout: %v", err))
	}
	return ms
}
