package engine

import (
	"context"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

const latestMigrationVersion uint = 103

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
	m, err := migrate.New("file://"+testutil.MigrationsPath(), connStr)
	require.NoError(t, err)
	require.NoError(t, m.Migrate(version))
	srcErr, dbErr := m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)
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
