package engine

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestMigrationRoundtrip(t *testing.T) {
	// All migrations must survive a full up → down → up cycle without errors.
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("roundtrip_test"),
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

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")

	// Step 1: Run all migrations up (through 033).
	m, err := migrate.New("file://"+migrationsDir, connStr)
	require.NoError(t, err)
	require.NoError(t, m.Up())
	srcErr, dbErr := m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)

	// Step 2: Migrate down to version 25 (reverses 033 through 026).
	m, err = migrate.New("file://"+migrationsDir, connStr)
	require.NoError(t, err)
	require.NoError(t, m.Migrate(25))
	srcErr, dbErr = m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)

	// Step 3: Run all migrations up again.
	m, err = migrate.New("file://"+migrationsDir, connStr)
	require.NoError(t, err)
	require.NoError(t, m.Up())
	srcErr, dbErr = m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)

	// Step 4: Verify the schema is correct by checking for the composite PK columns.
	m, err = migrate.New("file://"+migrationsDir, connStr)
	require.NoError(t, err)
	ver, dirty, err := m.Version()
	require.NoError(t, err)
	assert.False(t, dirty, "migration state should not be dirty after roundtrip")
	assert.Equal(t, uint(34), ver, "should be at latest migration version")
	srcErr, dbErr = m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)
}
