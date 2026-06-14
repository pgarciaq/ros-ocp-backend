package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

const pgErrQueryCanceled = "57014"

var apiStatementTimeoutCancellations = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "ros_api_statement_timeout_cancellations_total",
		Help: "API queries cancelled due to PostgreSQL statement_timeout",
	},
)

type statementExecer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// APIStatementTimeoutMS returns the session-level statement timeout for API/GORM paths in milliseconds.
// ROS_API_STATEMENT_TIMEOUT_MS takes precedence; otherwise ROS_DB_STATEMENT_TIMEOUT (seconds) is used.
func APIStatementTimeoutMS() int {
	cfg := config.GetConfig()
	if cfg == nil {
		return 25000
	}
	if cfg.DBAPIStatementTimeoutMS > 0 {
		return cfg.DBAPIStatementTimeoutMS
	}
	if cfg.DBStatementTimeoutSecs > 0 {
		return cfg.DBStatementTimeoutSecs * 1000
	}
	return 25000
}

// StatementTimeoutSecs returns the session-level statement timeout for API/GORM paths in seconds.
func StatementTimeoutSecs() int {
	ms := APIStatementTimeoutMS()
	if ms <= 0 {
		return 25
	}
	return (ms + 999) / 1000
}

// IngestStatementTimeoutSecs returns the per-transaction timeout for ingestion batch writes.
func IngestStatementTimeoutSecs() int {
	cfg := config.GetConfig()
	if cfg == nil || cfg.DBIngestStatementTimeoutSecs <= 0 {
		return 120
	}
	return cfg.DBIngestStatementTimeoutSecs
}

// SetLocalStatementTimeout overrides statement_timeout for the current transaction only via SET LOCAL.
// The override resets automatically when the transaction ends.
func SetLocalStatementTimeout(ctx context.Context, conn statementExecer, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = time.Duration(APIStatementTimeoutMS()) * time.Millisecond
	}
	ms := int(timeout / time.Millisecond)
	if ms <= 0 {
		ms = 1
	}
	_, err := conn.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = '%dms'", ms))
	return err
}

// SetLocalIngestStatementTimeout raises statement_timeout for the current transaction only
// via SET LOCAL. The override resets automatically when the transaction ends.
func SetLocalIngestStatementTimeout(ctx context.Context, conn statementExecer) error {
	return SetLocalStatementTimeout(ctx, conn, time.Duration(IngestStatementTimeoutSecs())*time.Second)
}

// IsStatementTimeoutCancellation reports whether err is PostgreSQL query_canceled (57014).
func IsStatementTimeoutCancellation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgErrQueryCanceled
}

// RecordStatementTimeoutCancellation increments ros_api_statement_timeout_cancellations_total when
// err is a statement_timeout cancellation.
func RecordStatementTimeoutCancellation(err error) {
	if IsStatementTimeoutCancellation(err) {
		apiStatementTimeoutCancellations.Inc()
	}
}

// QueryStatementTimeoutMillis returns the active statement_timeout in milliseconds.
func QueryStatementTimeoutMillis(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (int64, error) {
	var ms int64
	err := q.QueryRow(ctx,
		`SELECT CAST(extract(epoch from current_setting('statement_timeout')::interval) * 1000 AS bigint)`).Scan(&ms)
	if err != nil {
		return 0, fmt.Errorf("query statement_timeout: %w", err)
	}
	return ms, nil
}
