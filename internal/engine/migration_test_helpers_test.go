package engine

import (
	"context"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	gormPostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const latestMigrationVersion uint = 216

// openDedicatedTestGORM gives migration tests a GORM handle bound to their
// private container pool. OpenTestGORM is intentionally process-singleton and
// may already point at a different database (or be closed by another test).
func openDedicatedTestGORM(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	sqlDB := stdlib.OpenDBFromPool(pool)
	db, err := gorm.Open(gormPostgres.New(gormPostgres.Config{Conn: sqlDB}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		_ = sqlDB.Close()
		require.NoError(t, err)
		return
	}
	previousDB := database.DB
	database.DB = db
	t.Cleanup(func() {
		database.DB = previousDB
		require.NoError(t, sqlDB.Close())
	})
}

// setupMigratePostgres starts PostgreSQL and returns a connection string. Migrations are not applied.
func setupMigratePostgres(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("migrate_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgContainer.Terminate(ctx) })

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	return connStr
}

func runMigrationsTo(t *testing.T, connStr string, version uint) {
	t.Helper()
	require.NoError(t, migrateTo(t, connStr, version))
}

func migrateTo(t *testing.T, connStr string, version uint) error {
	t.Helper()
	m, err := migrate.New("file://"+testutil.MigrationsPath(), connStr)
	require.NoError(t, err)
	migErr := m.Migrate(version)
	srcErr, dbErr := m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)
	return migErr
}

func runMigrationsUp(t *testing.T, connStr string) {
	t.Helper()
	m, err := migrate.New("file://"+testutil.MigrationsPath(), connStr)
	require.NoError(t, err)
	require.NoError(t, m.Up())
	srcErr, dbErr := m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)
}

func migrationVersion(t *testing.T, connStr string) uint {
	t.Helper()
	m, err := migrate.New("file://"+testutil.MigrationsPath(), connStr)
	require.NoError(t, err)
	ver, dirty, err := m.Version()
	require.NoError(t, err)
	require.False(t, dirty)
	srcErr, dbErr := m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)
	return ver
}
