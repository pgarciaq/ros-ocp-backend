package testutil

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

var (
	sharedTestDB     *pgxpool.Pool
	sharedTestDBOnce sync.Once
	sharedTestDBErr  error
	// sharedTestDBMu serializes tests against the shared pool so parallel test runs
	// do not read or write each other's fixture data.
	sharedTestDBMu sync.Mutex
)

// SetupTestDB returns a shared PostgreSQL 16 testcontainers pool with migrations applied.
// The container is started once per test process to avoid Docker exhaustion and t.Parallel
// deadlocks when many integration tests each spawn their own container.
// Skipped automatically in short mode (-short flag).
func SetupTestDB(tb testing.TB) *pgxpool.Pool {
	tb.Helper()
	if testing.Short() {
		tb.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	sharedTestDBOnce.Do(initSharedTestDB)
	if sharedTestDBErr != nil {
		tb.Fatalf("shared test database: %v", sharedTestDBErr)
	}
	sharedTestDBMu.Lock()
	tb.Cleanup(sharedTestDBMu.Unlock)
	truncatePublicTables(tb, sharedTestDB)
	return sharedTestDB
}

func truncatePublicTables(tb testing.TB, pool *pgxpool.Pool) {
	tb.Helper()
	ctx := context.Background()
	var truncateSQL string
	err := pool.QueryRow(ctx, `
		SELECT 'TRUNCATE TABLE ' || string_agg(format('%I', tablename), ', ') || ' CASCADE'
		FROM pg_tables
		WHERE schemaname = 'public'
		  AND tablename NOT IN (
		    'schema_migrations',
		    'notification_code_definitions',
		    'ros_partitioned_parent_registry'
		  )
	`).Scan(&truncateSQL)
	if err != nil {
		tb.Fatalf("build truncate statement: %v", err)
	}
	if truncateSQL == "" {
		return
	}
	if _, err := pool.Exec(ctx, truncateSQL); err != nil {
		tb.Fatalf("truncate public tables: %v", err)
	}
}

func initSharedTestDB() {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("ros_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		sharedTestDBErr = fmt.Errorf("start postgres container: %w", err)
		return
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		sharedTestDBErr = fmt.Errorf("connection string: %w", err)
		return
	}

	migrationsDir := migrationsPath()
	m, err := migrate.New("file://"+migrationsDir, connStr)
	if err != nil {
		sharedTestDBErr = fmt.Errorf("create migrate instance: %w", err)
		return
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		sharedTestDBErr = fmt.Errorf("run migrations: %w", err)
		return
	}
	srcErr, dbErr := m.Close()
	if srcErr != nil {
		sharedTestDBErr = fmt.Errorf("migrate source close: %w", srcErr)
		return
	}
	if dbErr != nil {
		sharedTestDBErr = fmt.Errorf("migrate db close: %w", dbErr)
		return
	}

	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		sharedTestDBErr = fmt.Errorf("parse pool config: %w", err)
		return
	}
	poolCfg.MaxConns = 32

	sharedTestDB, err = pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		sharedTestDBErr = fmt.Errorf("create pgxpool: %w", err)
		return
	}
	if err := sharedTestDB.Ping(ctx); err != nil {
		sharedTestDBErr = fmt.Errorf("ping database: %w", err)
		return
	}
}

// migrationsPath returns the absolute path to the migrations/ directory,
// relative to this source file so it works regardless of the working directory.
func migrationsPath() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("testutil: unable to determine current file path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
}

// TruncateTable removes all rows from the given table. Useful for test isolation.
func TruncateTable(tb testing.TB, pool *pgxpool.Pool, table string) {
	tb.Helper()
	_, err := pool.Exec(context.Background(), fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
	if err != nil {
		tb.Fatalf("failed to truncate %s: %v", table, err)
	}
}
