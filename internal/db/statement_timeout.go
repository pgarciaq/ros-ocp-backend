package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"gorm.io/gorm"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

const pgErrQueryCanceled = "57014"

const heavyAPIStatementTimeoutFallbackMS = 45000

var (
	heavyAPIStatementTimeoutWarnOnce sync.Once
	heavyAPIStatementTimeoutWarnHook   = warnHeavyAPIStatementTimeoutFallback
)

func warnHeavyAPIStatementTimeoutFallback() {
	raw := strings.TrimSpace(os.Getenv("ROS_HEAVY_API_STATEMENT_TIMEOUT_MS"))
	if raw == "" {
		return
	}
	cfg := config.GetConfig()
	if cfg != nil && cfg.DBHeavyAPIStatementTimeoutMS > 0 {
		return
	}
	logging.GetLogger().Warnf(
		"ROS_HEAVY_API_STATEMENT_TIMEOUT_MS=%q is invalid; using default %dms",
		raw,
		heavyAPIStatementTimeoutFallbackMS,
	)
}

func resetHeavyAPIStatementTimeoutWarnForTest() {
	heavyAPIStatementTimeoutWarnOnce = sync.Once{}
	heavyAPIStatementTimeoutWarnHook = warnHeavyAPIStatementTimeoutFallback
}

// ResetHeavyAPIStatementTimeoutWarnForTest resets one-shot warning state between tests.
func ResetHeavyAPIStatementTimeoutWarnForTest() {
	resetHeavyAPIStatementTimeoutWarnForTest()
}

// SetHeavyAPIStatementTimeoutWarnHookForTest overrides the invalid-value warning hook in tests.
func SetHeavyAPIStatementTimeoutWarnHookForTest(hook func()) {
	heavyAPIStatementTimeoutWarnHook = hook
}

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

// HeavyAPIStatementTimeoutMS returns extended statement_timeout for aggregation and fleet-wide list endpoints.
// ROS_HEAVY_API_STATEMENT_TIMEOUT_MS overrides the default (45000ms). SaaS deployments should set ~28000
// to stay within the ~30s ingress/gateway budget.
func HeavyAPIStatementTimeoutMS() int {
	cfg := config.GetConfig()
	if cfg != nil && cfg.DBHeavyAPIStatementTimeoutMS > 0 {
		return cfg.DBHeavyAPIStatementTimeoutMS
	}
	heavyAPIStatementTimeoutWarnOnce.Do(heavyAPIStatementTimeoutWarnHook)
	return heavyAPIStatementTimeoutFallbackMS
}

// QueryRower is satisfied by pgx.Tx and *pgxpool.Pool for read queries.
type QueryRower interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// WithHeavyStatementTimeout runs fn in a transaction with an extended SET LOCAL statement_timeout.
func WithHeavyStatementTimeout(ctx context.Context, pool *pgxpool.Pool, fn func(ctx context.Context, q QueryRower) error) error {
	return WithStatementTimeout(ctx, pool, time.Duration(HeavyAPIStatementTimeoutMS())*time.Millisecond, fn)
}

// WithStatementTimeout runs fn in a transaction with a custom SET LOCAL statement_timeout.
func WithStatementTimeout(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration, fn func(ctx context.Context, q QueryRower) error) error {
	if pool == nil {
		return fmt.Errorf("database pool unavailable")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := SetLocalStatementTimeout(ctx, tx, timeout); err != nil {
		return err
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// WithHeavyGORMStatementTimeout runs fn in a GORM transaction with extended SET LOCAL statement_timeout.
func WithHeavyGORMStatementTimeout(fn func(tx *gorm.DB) error) error {
	db := GetDB()
	if db == nil {
		return fmt.Errorf("database unavailable")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		ms := HeavyAPIStatementTimeoutMS()
		if err := tx.Exec(fmt.Sprintf("SET LOCAL statement_timeout = '%dms'", ms)).Error; err != nil {
			return err
		}
		return fn(tx)
	})
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
