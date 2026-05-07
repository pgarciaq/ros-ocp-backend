package testutil

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
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

// SetupTestDB spins up a PostgreSQL 16 container via testcontainers,
// runs all golang-migrate migrations, and returns a *pgxpool.Pool.
// The container and pool are cleaned up automatically via t.Cleanup().
// Skipped automatically in short mode (-short flag).
func SetupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
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
		t.Fatalf("failed to start postgres container: %v", err)
	}

	t.Cleanup(func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("warning: failed to terminate postgres container: %v", err)
		}
	})

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	migrationsDir := migrationsPath()
	m, err := migrate.New("file://"+migrationsDir, connStr)
	if err != nil {
		t.Fatalf("failed to create migrate instance: %v", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("failed to run migrations: %v", err)
	}
	srcErr, dbErr := m.Close()
	if srcErr != nil {
		t.Fatalf("migrate source close error: %v", srcErr)
	}
	if dbErr != nil {
		t.Fatalf("migrate db close error: %v", dbErr)
	}

	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("failed to parse pool config: %v", err)
	}
	poolCfg.MaxConns = 5

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatalf("failed to create pgxpool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("failed to ping test database: %v", err)
	}

	return pool
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
func TruncateTable(t *testing.T, pool *pgxpool.Pool, table string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
	if err != nil {
		t.Fatalf("failed to truncate %s: %v", table, err)
	}
}
