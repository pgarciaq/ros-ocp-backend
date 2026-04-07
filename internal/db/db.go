package db

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB = nil
var Pool *pgxpool.Pool = nil

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
	poolCfg.MaxConns = 10

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

func CreateCACertFile(certString string) string {
	f, err := os.CreateTemp("", "RdsCa.pem")
	if err != nil {
		fmt.Printf("Unable to create RdsCa.pem: %s", err)
		os.Exit(1)
	}
	_, err = f.Write([]byte(certString))
	if err != nil {
		fmt.Printf("Unable to write to RdsCa.pem: %s", err)
		os.Exit(1)
	}
	return f.Name()
}
