package engine

import (
	"context"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestMigrationRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
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

	migrationsDir := testutil.MigrationsPath()

	// Step 1: Run all migrations up.
	m, err := migrate.New("file://"+migrationsDir, connStr)
	require.NoError(t, err)
	require.NoError(t, m.Up())
	srcErr, dbErr := m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)

	// Step 2: Migrate down to version 25 (reverses 044 through 026).
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
	assert.Equal(t, latestMigrationVersion, ver, "should be at latest migration version")
	srcErr, dbErr = m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)
}

// BH-INT-004: New business-hours migrations must survive up → down 067 → up without error.
func TestMigrationRoundtrip_BusinessHours(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}

	connStr := setupMigratePostgres(t)
	runMigrationsUp(t, connStr)
	require.Equal(t, latestMigrationVersion, migrationVersion(t, connStr))

	poolCfg, err := pgxpool.ParseConfig(connStr)
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	ctx := context.Background()
	clusterUUID := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	var bucketDate string
	require.NoError(t, pool.QueryRow(ctx, `SELECT date_trunc('month', CURRENT_DATE)::date::text`).Scan(&bucketDate))
	_, err = pool.Exec(ctx, `
		INSERT INTO daily_container_digests (
			bucket_date, org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, schedule_type, sample_count
		) VALUES
			($2::date, 'org-roundtrip', $1, 'ns', 'wl', 'deployment', 'ctr', 'all_hours', 1),
			($2::date, 'org-roundtrip', $1, 'ns', 'wl', 'deployment', 'ctr', 'business_hours', 1)
	`, clusterUUID, bucketDate)
	require.NoError(t, err)

	runMigrationsTo(t, connStr, 66)

	var countAfterDown int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM daily_container_digests WHERE org_id = 'org-roundtrip'
	`).Scan(&countAfterDown)
	require.NoError(t, err)
	assert.Equal(t, 1, countAfterDown, "067 down must delete business_hours rows before dropping column")
	assert.False(t, columnExists(t, pool, "daily_container_digests", "schedule_type"))

	runMigrationsUp(t, connStr)
	require.Equal(t, latestMigrationVersion, migrationVersion(t, connStr))

	pkCols := queryPrimaryKeyColumns(t, pool, "daily_container_digests")
	assert.Contains(t, pkCols, "schedule_type")
}
