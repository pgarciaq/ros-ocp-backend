package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB = nil
var Pool *pgxpool.Pool = nil

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
	cfg := config.GetConfig()
	log := logging.GetLogger()
	var (
		user     = cfg.DBUser
		password = cfg.DBPassword
		dbname   = cfg.DBName
		host     = cfg.DBHost
		port     = cfg.DBPort
		dbssl    = cfg.DBssl
	)

	dsn := fmt.Sprintf("user=%s password=%s dbname=%s host=%s port=%s sslmode=%s", user, password, dbname, host, port, dbssl)

	if dbssl != "disable" {
		rdsCA := CreateCACertFile(cfg.DBCACert)
		sslCertParam := fmt.Sprintf(" sslrootcert=%s", rdsCA)
		dsn = fmt.Sprintf("%s %s", dsn, sslCertParam)

	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		log.Fatal(err)
	}

	DB = db

	log.Info("DB initialization complete")
}

func GetDB() *gorm.DB {
	if DB == nil {
		initDB()
	}
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
		maxConns = 10
	}
	poolCfg.MaxConns = maxConns
	minConns := int32(cfg.DBMinConns)
	if minConns > 0 {
		poolCfg.MinConns = minConns
	}
	poolCfg.MaxConnLifetime = time.Duration(cfg.DBMaxConnLifetimeMins) * time.Minute
	poolCfg.MaxConnIdleTime = time.Duration(cfg.DBMaxConnIdleTimeMins) * time.Minute
	poolCfg.ConnConfig.DefaultQueryExecMode = parseQueryExecMode(cfg.DBStatementCacheMode)

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
	log.Info("pgxpool initialization complete")
}

// GetPool returns the pgxpool.Pool singleton, initializing it if needed.
func GetPool() *pgxpool.Pool {
	if Pool == nil {
		initPool()
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
