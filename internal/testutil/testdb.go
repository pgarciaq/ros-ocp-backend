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
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/redhatinsights/ros-ocp-backend/internal/asyncjobs"
	"github.com/redhatinsights/ros-ocp-backend/internal/clustercache"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetheatmap"
)

const sharedTestDBMaxConns = 16

var (
	sharedTestDB        *pgxpool.Pool
	sharedTestDBOnce    sync.Once
	sharedTestDBErr     error
	sharedTestGORM      *gorm.DB
	sharedTestGORMAsync sync.Once
	sharedTestGORMErr   error
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
		// Marker for CI summaries (grep SKIP-NO-PG): report loudly, never
		// fail local runs for missing Docker (#536).
		tb.Log("SKIP-NO-PG: skipping integration test (requires testcontainers/Docker)")
		tb.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	sharedTestDBOnce.Do(initSharedTestDB)
	if sharedTestDBErr != nil {
		tb.Fatalf("shared test database: %v", sharedTestDBErr)
	}
	sharedTestDBMu.Lock()
	// Registered first so it runs last: drain async work and restore shared pool
	// after per-test cleanups that assign database.Pool = nil.
	tb.Cleanup(func() {
		waitForAsyncJobsBeforeTruncate(tb)
		database.SetForceTestPool(sharedTestDB)
		database.DB = OpenTestGORM(sharedTestDB)
	})
	tb.Cleanup(sharedTestDBMu.Unlock)
	waitForAsyncJobsBeforeTruncate(tb)
	truncatePublicTables(tb, sharedTestDB)
	clustercache.ResetForTest()
	fleetheatmap.ResetForTest()
	database.SetForceTestPool(sharedTestDB)
	database.DB = OpenTestGORM(sharedTestDB)
	return sharedTestDB
}

func waitForAsyncJobsBeforeTruncate(tb testing.TB) {
	tb.Helper()
	if err := asyncjobs.WaitForTest(0); err != nil {
		tb.Fatalf("wait for async jobs before truncate: %v", err)
	}
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

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("ros_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
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
	poolCfg.MaxConns = sharedTestDBMaxConns

	sharedTestDB, err = pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		sharedTestDBErr = fmt.Errorf("create pgxpool: %w", err)
		return
	}
	if err := sharedTestDB.Ping(ctx); err != nil {
		sharedTestDBErr = fmt.Errorf("ping database: %w", err)
		return
	}
	database.SetForceTestPool(sharedTestDB)
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

// OpenTestGORM returns a shared GORM handle backed by the pgxpool from SetupTestDB.
// Tests must call SetupTestDB first. Reuses the pool instead of opening a separate
// connection pool per test, which exhausts PostgreSQL max_connections in large suites.
func OpenTestGORM(pool *pgxpool.Pool) *gorm.DB {
	sharedTestGORMAsync.Do(func() {
		sqlDB := stdlib.OpenDBFromPool(pool)
		sqlDB.SetMaxOpenConns(sharedTestDBMaxConns)
		sqlDB.SetMaxIdleConns(sharedTestDBMaxConns)

		sharedTestGORM, sharedTestGORMErr = gorm.Open(postgres.New(postgres.Config{
			Conn: sqlDB,
		}), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
	})
	if sharedTestGORMErr != nil {
		panic(fmt.Sprintf("shared test GORM: %v", sharedTestGORMErr))
	}
	return sharedTestGORM
}

// TruncateTable removes all rows from the given table. Useful for test isolation.
func TruncateTable(tb testing.TB, pool *pgxpool.Pool, table string) {
	tb.Helper()
	_, err := pool.Exec(context.Background(), fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
	if err != nil {
		tb.Fatalf("failed to truncate %s: %v", table, err)
	}
}
