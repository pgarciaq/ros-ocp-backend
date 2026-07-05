package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB = nil
var Pool *pgxpool.Pool = nil

var poolOnce sync.Once
var dbOnce sync.Once

// testPoolOverride is the atomic test-pool pointer. Tests store a *pgxpool.Pool
// here to override GetPool without resetting sync.Once (which was a data race).
// Production code never touches this; its zero value (nil) means "no override."
var testPoolOverride atomic.Pointer[pgxpool.Pool]

// testInitSuppressed prevents poolOnce.Do(initPool) from running when tests have
// explicitly set Pool=nil to exercise error paths. Atomic for safe concurrent reads.
var testInitSuppressed atomic.Bool

// testMu serializes test-only state transitions (SetForceTestPool, SuspendForceTestPool).
// Production code never acquires this lock.
var testMu sync.Mutex

// SetForceTestPool directs GetPool and GetDB to use the integration-test pgxpool.
// Called by internal/testutil when the shared testcontainers Postgres starts.
// Thread-safe: uses atomic store for the override pointer.
func SetForceTestPool(p *pgxpool.Pool) {
	testMu.Lock()
	defer testMu.Unlock()
	testPoolOverride.Store(p)
	testInitSuppressed.Store(false)
	Pool = p
	DB = nil
}

// SuspendForceTestPool clears the integration-test pool override so unit tests can
// exercise nil-pool error paths. Call the returned restore func in cleanup.
// Thread-safe: uses atomic store for the override pointer. Suppresses auto-init
// so that GetPool() returns nil instead of attempting a real database connection.
func SuspendForceTestPool() (restore func()) {
	testMu.Lock()
	defer testMu.Unlock()
	prev := testPoolOverride.Load()
	prevDB := DB
	testPoolOverride.Store(nil)
	testInitSuppressed.Store(true)
	Pool = nil
	DB = nil
	return func() {
		testMu.Lock()
		defer testMu.Unlock()
		testPoolOverride.Store(prev)
		testInitSuppressed.Store(false)
		Pool = prev
		DB = prevDB
	}
}

func setStatementTimeout(ctx context.Context, conn *pgconn.PgConn) error {
	ms := APIStatementTimeoutMS()
	_, err := conn.Exec(ctx, fmt.Sprintf("SET statement_timeout = '%dms'", ms)).ReadAll()
	return err
}

// poolAcquireTimeout bounds how long Pool.Acquire may wait when using contexts
// produced by ContextWithAcquireTimeout. Zero disables the helper timeout.
var poolAcquireTimeout time.Duration

// ContextWithAcquireTimeout returns a child context with a deadline for pool
// acquisition when the parent has no deadline. The caller must invoke cancel
// when finished with the returned context. pgxpool respects ctx when acquiring
// connections from the pool.
func ContextWithAcquireTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if poolAcquireTimeout <= 0 || ctx == nil {
		return ctx, func() {}
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, poolAcquireTimeout)
}

// ReadyzDB is the subset of *pgxpool.Pool used by GET /readyz.
type ReadyzDB interface {
	Ping(ctx context.Context) error
}

// ReadyzPoolProvider supplies the database handle for GET /readyz. When nil, GetPool() is used.
// Tests may assign a mock implementation and reset to nil in cleanup.
var ReadyzPoolProvider func() ReadyzDB

func initDB() {
	// ADR-0128: Unified pgxpool via stdlib.OpenDBFromPool (GORM shares pool with pgx paths).
	pool := GetPool()
	log := logging.GetLogger()

	sqlDB := stdlib.OpenDBFromPool(pool)
	sqlDB.SetMaxIdleConns(0)
	sqlDB.SetMaxOpenConns(0)

	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn: sqlDB,
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		log.Fatal(err)
	}

	DB = db

	log.Info("DB initialization complete (GORM shares pgxpool)")
}

func GetDB() *gorm.DB {
	dbOnce.Do(func() {
		if DB == nil {
			initDB()
		}
	})
	return DB
}

func initPool() {
	cfg := config.GetConfig()
	log := logging.GetLogger()

	dsn := fmt.Sprintf("user=%s password=%s dbname=%s host=%s port=%s sslmode=%s",
		cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBHost, cfg.DBPort, cfg.DBssl)

	if cfg.DBssl != "disable" {
		rdsCA := CreateCACertFile(cfg.DBCACert)
		dsn = fmt.Sprintf("%s sslrootcert=%s", dsn, rdsCA)
	}

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Fatalf("failed to parse pgxpool config: %v", err)
	}

	maxConns := int32(cfg.DBMaxConns)
	if maxConns <= 0 {
		maxConns = 5
	}
	poolCfg.MaxConns = maxConns
	minConns := int32(cfg.DBMinConns)
	if minConns > 0 {
		poolCfg.MinConns = minConns
	}
	poolCfg.MaxConnLifetime = time.Duration(cfg.DBMaxConnLifetimeMins) * time.Minute
	poolCfg.MaxConnIdleTime = time.Duration(cfg.DBMaxConnIdleTimeMins) * time.Minute
	poolCfg.ConnConfig.DefaultQueryExecMode = parseQueryExecMode(cfg.DBStatementCacheMode)
	poolCfg.ConnConfig.AfterConnect = setStatementTimeout

	timeoutSecs := cfg.DBAcquireTimeoutSecs
	if timeoutSecs < 0 {
		timeoutSecs = 0
	}
	poolAcquireTimeout = time.Duration(timeoutSecs) * time.Second

	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		log.Fatalf("failed to create pgxpool: %v", err)
	}

	Pool = pool
	metrics.RegisterPoolCollector(func() *pgxpool.Pool { return Pool })
	log.Info("pgxpool initialization complete")
}

// GetPool returns the pgxpool.Pool singleton, initializing it if needed.
// Test overrides (via SetForceTestPool) take priority and are read atomically.
// When testInitSuppressed is set (by SuspendForceTestPool), initialization is
// skipped so tests can exercise nil-pool error handling without triggering real
// database connections.
func GetPool() *pgxpool.Pool {
	if p := testPoolOverride.Load(); p != nil {
		return p
	}
	if !testInitSuppressed.Load() {
		poolOnce.Do(func() {
			if Pool == nil {
				initPool()
			}
		})
	}
	return Pool
}

// parseQueryExecMode maps ROS_DB_STATEMENT_CACHE_MODE to pgx DefaultQueryExecMode.
// Accepts pgx names and shorthand "describe" (cache_describe).
func parseQueryExecMode(mode string) pgx.QueryExecMode {
	switch mode {
	case "describe", "cache_describe":
		return pgx.QueryExecModeCacheDescribe
	case "cache_statement", "statement":
		return pgx.QueryExecModeCacheStatement
	case "describe_exec":
		return pgx.QueryExecModeDescribeExec
	case "exec":
		return pgx.QueryExecModeExec
	case "simple_protocol":
		return pgx.QueryExecModeSimpleProtocol
	default:
		return pgx.QueryExecModeCacheDescribe
	}
}

func CreateCACertFile(certString string) string {
	f, err := os.CreateTemp("", "RdsCa.pem")
	if err != nil {
		log.Fatalf("db: unable to create RdsCa.pem: %v", err)
	}
	_, err = f.Write([]byte(certString))
	if err != nil {
		log.Fatalf("db: unable to write to RdsCa.pem: %v", err)
	}
	return f.Name()
}
