package engine

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func queryPrimaryKeyColumns(t *testing.T, pool *pgxpool.Pool, table string) []string {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx, `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		JOIN pg_class c ON c.oid = i.indrelid
		WHERE c.relname = $1 AND i.indisprimary
		ORDER BY array_position(i.indkey::int[], a.attnum)
	`, table)
	require.NoError(t, err)
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var col string
		require.NoError(t, rows.Scan(&col))
		cols = append(cols, col)
	}
	require.NoError(t, rows.Err())
	return cols
}

func tableExists(t *testing.T, pool *pgxpool.Pool, table string) bool {
	t.Helper()
	ctx := context.Background()
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = $1
		)`, table).Scan(&exists)
	require.NoError(t, err)
	return exists
}

func columnExists(t *testing.T, pool *pgxpool.Pool, table, column string) bool {
	t.Helper()
	ctx := context.Background()
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
		)`, table, column).Scan(&exists)
	require.NoError(t, err)
	return exists
}

// BH-INT-001
func TestMigration_BusinessHoursSchedulesTable(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	assert.True(t, tableExists(t, pool, "business_hours_schedules"))
	assert.Equal(t, []string{"org_id", "cluster_uuid", "namespace"}, queryPrimaryKeyColumns(t, pool, "business_hours_schedules"))

	required := []string{
		"org_id", "cluster_uuid", "namespace", "timezone", "days",
		"start_time", "end_time", "off_hours_weight", "enabled",
		"reship_pending_since", "updated_at",
	}
	for _, col := range required {
		assert.True(t, columnExists(t, pool, "business_hours_schedules", col), "missing column %s", col)
	}
}

// BH-INT-002
func TestMigration_DigestScheduleTypeEnum_Container(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	var enumExists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_type WHERE typname = 'digest_schedule_type'
		)`).Scan(&enumExists)
	require.NoError(t, err)
	assert.True(t, enumExists)

	assert.True(t, columnExists(t, pool, "daily_container_digests", "schedule_type"))
	pkCols := queryPrimaryKeyColumns(t, pool, "daily_container_digests")
	assert.Contains(t, pkCols, "schedule_type")
}

// BH-INT-003
func TestMigration_DigestScheduleTypeEnum_Namespace(t *testing.T) {
	pool := testutil.SetupTestDB(t)

	assert.True(t, columnExists(t, pool, "daily_namespace_digests", "schedule_type"))
	pkCols := queryPrimaryKeyColumns(t, pool, "daily_namespace_digests")
	assert.Contains(t, pkCols, "schedule_type")
}

// BH-INT-005
func TestMigration_ExistingRowsDefaultAllHours(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	clusterUUID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	var bucketDate string
	err := pool.QueryRow(ctx, `SELECT date_trunc('month', CURRENT_DATE)::date::text`).Scan(&bucketDate)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO daily_container_digests (
			bucket_date, org_id, cluster_uuid, namespace, workload, workload_type, container_name,
			sample_count
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 1)
	`, bucketDate, "org-bh-default", clusterUUID, "ns1", "wl", "deployment", "ctr")
	require.NoError(t, err)

	var scheduleType string
	err = pool.QueryRow(ctx, `
		SELECT schedule_type::text FROM daily_container_digests
		WHERE org_id = $1 AND container_name = $2
	`, "org-bh-default", "ctr").Scan(&scheduleType)
	require.NoError(t, err)
	assert.Equal(t, "all_hours", scheduleType)
}

// BH-INT-015
func TestMigration_BusinessHoursSchedulesIndexes(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	rows, err := pool.Query(ctx, `
		SELECT indexname FROM pg_indexes
		WHERE tablename = 'business_hours_schedules' AND schemaname = 'public'
	`)
	require.NoError(t, err)
	defer rows.Close()

	indexes := make(map[string]bool)
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		indexes[name] = true
	}
	require.NoError(t, rows.Err())

	assert.True(t, indexes["idx_bh_schedules_org"])
	assert.True(t, indexes["idx_bh_schedules_org_cluster"])
}

// Cluster-wide BH digest reads (#514 node, #515 GPU) — migration 000186.
func TestMigration_BHClusterDigestIndexes(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	for _, name := range []string{
		"idx_daily_node_digests_cluster_sched_date",
		"idx_gpu_container_digests_cluster_sched_start",
	} {
		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = $1)
		`, name).Scan(&exists)
		require.NoError(t, err)
		assert.True(t, exists, "expected index %s after migrate up", name)
	}
}

// GPU org_id + covering org index (#512 PR-1) — migration 000187.
func TestMigration_GPUDigestOrgID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	var colExists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'gpu_container_digests' AND column_name = 'org_id'
		)`).Scan(&colExists)
	require.NoError(t, err)
	assert.True(t, colExists, "expected gpu_container_digests.org_id after migrate up")

	var idxExists bool
	err = pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = $1)
	`, "idx_gpu_container_digests_org_cluster_sched_start").Scan(&idxExists)
	require.NoError(t, err)
	assert.True(t, idxExists, "expected org covering GPU index after migrate up")

	var notNull bool
	err = pool.QueryRow(ctx, `
		SELECT is_nullable = 'NO'
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'gpu_container_digests' AND column_name = 'org_id'
	`).Scan(&notNull)
	require.NoError(t, err)
	assert.False(t, notNull, "org_id must stay nullable until #512 PR-2")
}

func TestMigration_GPUDigestOrgIDBackfill(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-gpu-backfill"
	clusterUUID := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	day := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)

	testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
		IntervalStart: day,
		ClusterUUID:   clusterUUID,
		Namespace:     "ml",
		Workload:      "train",
		WorkloadType:  "deployment",
		ContainerName: "gpu",
		GPUModelName:  "A100",
	})
	var stamped *string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT org_id FROM gpu_container_digests
		WHERE cluster_uuid = $1::uuid AND namespace = 'ml'`, clusterUUID).Scan(&stamped))
	assert.Nil(t, stamped, "seed without OrgID must leave org_id NULL")

	var tenantID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO rh_accounts (org_id) VALUES ($1)
		ON CONFLICT (org_id) DO UPDATE SET org_id = EXCLUDED.org_id
		RETURNING id`, orgID).Scan(&tenantID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES ($1, $2::uuid, 'gpu-backfill', 'src-gpu-bf', now()) ON CONFLICT DO NOTHING`,
		tenantID, clusterUUID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		UPDATE gpu_container_digests g
		SET org_id = src.org_id
		FROM (
			SELECT DISTINCT ON (c.cluster_uuid)
				c.cluster_uuid,
				a.org_id
			FROM clusters c
			JOIN rh_accounts a ON a.id = c.tenant_id
			WHERE a.org_id IS NOT NULL AND a.org_id <> ''
			ORDER BY c.cluster_uuid, a.id
		) src
		WHERE g.cluster_uuid = src.cluster_uuid
		  AND g.org_id IS NULL`)
	require.NoError(t, err)

	var got string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT org_id FROM gpu_container_digests
		WHERE cluster_uuid = $1::uuid AND namespace = 'ml'`, clusterUUID).Scan(&got))
	assert.Equal(t, orgID, got)
}

// BH-INT-016
func TestMigration_BusinessHoursSchedulesDefaults(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		INSERT INTO business_hours_schedules (org_id, timezone, days, start_time, end_time)
		VALUES ($1, $2, $3, $4, $5)
	`, "org-bh-defaults", "UTC", []string{"monday"}, "08:00", "17:00")
	require.NoError(t, err)

	var offHoursWeight float32
	var enabled bool
	err = pool.QueryRow(ctx, `
		SELECT off_hours_weight, enabled FROM business_hours_schedules WHERE org_id = $1
	`, "org-bh-defaults").Scan(&offHoursWeight, &enabled)
	require.NoError(t, err)
	assert.Equal(t, float32(0.0), offHoursWeight)
	assert.True(t, enabled)
}

// BH-INT-017
func TestMigration_ReshipPendingSinceColumn(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	assert.True(t, columnExists(t, pool, "business_hours_schedules", "reship_pending_since"))

	var isNullable string
	err := pool.QueryRow(ctx, `
		SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'business_hours_schedules'
		  AND column_name = 'reship_pending_since'
	`).Scan(&isNullable)
	require.NoError(t, err)
	assert.Equal(t, "YES", isNullable)
}

// BH-INT-033
func TestMigration_InvalidScheduleTypeRejected(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	clusterUUID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	var bucketDate string
	require.NoError(t, pool.QueryRow(ctx, `SELECT date_trunc('month', CURRENT_DATE)::date::text`).Scan(&bucketDate))
	_, err := pool.Exec(ctx, `
		INSERT INTO daily_container_digests (
			bucket_date, org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, schedule_type, sample_count
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::digest_schedule_type, 1)
	`, bucketDate, "org-invalid", clusterUUID, "ns", "wl", "deployment", "ctr", "invalid")
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "invalid")
}

// BH-INT-038
func TestMigration_FilesExistAndOrdered(t *testing.T) {
	dir := testutil.MigrationsPath()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var versions []int
	names := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		names[name] = true
		if len(name) >= 6 && name[:6] >= "000066" && name[:6] <= "000067" {
			var ver int
			_, scanErr := fmt.Sscanf(name[:6], "%d", &ver)
			if scanErr == nil {
				versions = append(versions, ver)
			}
		}
	}

	assert.True(t, names["000066_create_business_hours_schedules.up.sql"])
	assert.True(t, names["000066_create_business_hours_schedules.down.sql"])
	assert.True(t, names["000067_add_schedule_type_to_digests.up.sql"])
	assert.True(t, names["000067_add_schedule_type_to_digests.down.sql"])
	assert.True(t, names["000110_namespace_recommendation_schedule_type.up.sql"])
	assert.True(t, names["000110_namespace_recommendation_schedule_type.down.sql"])

	sort.Ints(versions)
	require.GreaterOrEqual(t, len(versions), 2)
	assert.Equal(t, 66, versions[0])
	assert.Equal(t, 67, versions[len(versions)-1])

	assert.True(t, names["000065_org_recommendation_terms_add_type.up.sql"])
	assert.Greater(t, int(latestMigrationVersion), 67)
}

// BH-INT-040
func TestMigration_067Down_DeletesBusinessHoursRowsBeforeDropColumn(t *testing.T) {
	connStr := setupMigratePostgres(t)
	runMigrationsUp(t, connStr)
	require.Equal(t, latestMigrationVersion, migrationVersion(t, connStr))

	poolCfg, err := pgxpool.ParseConfig(connStr)
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	ctx := context.Background()
	clusterUUID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	var bucketDate string
	require.NoError(t, pool.QueryRow(ctx, `SELECT date_trunc('month', CURRENT_DATE)::date::text`).Scan(&bucketDate))

	for _, st := range []string{"all_hours", "business_hours"} {
		_, err = pool.Exec(ctx, `
			INSERT INTO daily_container_digests (
				bucket_date, org_id, cluster_uuid, namespace, workload, workload_type,
				container_name, schedule_type, sample_count
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::digest_schedule_type, 1)
		`, bucketDate, "org-067-down", clusterUUID, "ns", "wl", "deployment", "ctr", st)
		require.NoError(t, err)
	}

	runMigrationsTo(t, connStr, 66)

	var count int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM daily_container_digests WHERE org_id = $1
	`, "org-067-down").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "067 down must delete business_hours rows; only all_hours row remains")
	assert.False(t, columnExists(t, pool, "daily_container_digests", "schedule_type"))
	pkCols := queryPrimaryKeyColumns(t, pool, "daily_container_digests")
	assert.NotContains(t, pkCols, "schedule_type")
}
