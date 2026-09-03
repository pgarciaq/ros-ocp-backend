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

// GPU org_id (#512 PR-1 + PR-2) — migrations 000187 / 000188.
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
	assert.True(t, notNull, "org_id must be NOT NULL after #512 PR-2")
}

func TestGPUDigestOrgID_RejectsNullInsert(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	day := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	ensureGPUDigestPartition(t, pool, day)

	_, err := pool.Exec(ctx, `
		INSERT INTO gpu_container_digests (
			interval_start, cluster_uuid, namespace, workload, workload_type,
			container_name, gpu_model_name
		) VALUES ($1, $2::uuid, 'ml', 'train', 'deployment', 'gpu', 'A100')`,
		day, "cccccccc-cccc-cccc-cccc-cccccccccccc")
	require.Error(t, err, "NULL org_id insert must fail after 000188")
	assert.Contains(t, strings.ToLower(err.Error()), "null")
}

func TestMigration_GPUDigestOrgIDBackfillThenNotNull(t *testing.T) {
	connStr := setupMigratePostgres(t)
	runMigrationsTo(t, connStr, 187)

	poolCfg, err := pgxpool.ParseConfig(connStr)
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	ctx := context.Background()
	orgID := "org-gpu-188-backfill"
	clusterUUID := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	day := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	insertGPUDigestWithOrg(t, pool, nil, clusterUUID, "ml", day)

	var tenantID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO rh_accounts (org_id) VALUES ($1)
		ON CONFLICT (org_id) DO UPDATE SET org_id = EXCLUDED.org_id
		RETURNING id`, orgID).Scan(&tenantID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES ($1, $2::uuid, 'gpu-188-bf', 'src-gpu-188', now()) ON CONFLICT DO NOTHING`,
		tenantID, clusterUUID)
	require.NoError(t, err)

	runMigrationsTo(t, connStr, 188)

	var got string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT org_id FROM gpu_container_digests
		WHERE cluster_uuid = $1::uuid AND namespace = 'ml'`, clusterUUID).Scan(&got))
	assert.Equal(t, orgID, got)

	var notNull bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT is_nullable = 'NO'
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'gpu_container_digests' AND column_name = 'org_id'
	`).Scan(&notNull))
	assert.True(t, notNull)
}

func TestMigration_GPUDigestOrgIDSetNotNullFailsOnOrphans(t *testing.T) {
	connStr := setupMigratePostgres(t)
	runMigrationsTo(t, connStr, 187)

	poolCfg, err := pgxpool.ParseConfig(connStr)
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	day := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	insertGPUDigestWithOrg(t, pool, nil, "dddddddd-dddd-dddd-dddd-dddddddddddd", "orphan", day)

	err = migrateTo(t, connStr, 188)
	require.Error(t, err, "000188 must fail when org_id IS NULL remains")
	assert.True(t,
		strings.Contains(strings.ToLower(err.Error()), "null") ||
			strings.Contains(strings.ToLower(err.Error()), "violat"),
		"expected null/constraint violation, got: %v", err)
}

// GPU unique includes org_id (#512 PR-3) — migration 000189.
func TestMigration_GPUDigestUniqueIncludesOrgID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	var indexdef string
	err := pool.QueryRow(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE indexname = 'gpu_container_digests_natural_key'`).Scan(&indexdef)
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(indexdef), "org_id",
		"natural key must include org_id after #512 PR-3, got: %s", indexdef)
}

func TestGPUDigestUnique_AllowsSameClusterUUIDAcrossOrgs(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	day := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	clusterUUID := "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"

	insertGPUDigestWithOrg(t, pool, strPtr("org-a"), clusterUUID, "ml", day)
	insertGPUDigestWithOrg(t, pool, strPtr("org-b"), clusterUUID, "ml", day)

	testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
		OrgID: "org-a", ClusterUUID: clusterUUID, Namespace: "ml", Workload: "train",
		WorkloadType: "deployment", ContainerName: "gpu", GPUModelName: "A100",
		NodeName: "stolen-check", IntervalStart: day,
	})

	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT count(*) FROM gpu_container_digests
		WHERE cluster_uuid = $1::uuid AND namespace = 'ml'`, clusterUUID).Scan(&n))
	assert.Equal(t, 2, n, "two orgs with the same cluster UUID must both persist GPU days")

	var orgB int
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT count(*) FROM gpu_container_digests
		WHERE org_id = 'org-b' AND cluster_uuid = $1::uuid AND namespace = 'ml'`, clusterUUID).Scan(&orgB))
	assert.Equal(t, 1, orgB, "upsert for org-a must not steal org-b's row")
}

func TestGPUDigestUnique_SameOrgConflictKeepsOneRow(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	day := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	clusterUUID := "ffffffff-ffff-ffff-ffff-ffffffffffff"
	orgID := "org-conflict"

	testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
		OrgID: orgID, ClusterUUID: clusterUUID, Namespace: "ml", Workload: "train",
		WorkloadType: "deployment", ContainerName: "gpu", GPUModelName: "A100",
		NodeName: "node-1", IntervalStart: day,
	})
	testutil.SeedGPUDigest(t, pool, testutil.GPUDigestRow{
		OrgID: orgID, ClusterUUID: clusterUUID, Namespace: "ml", Workload: "train",
		WorkloadType: "deployment", ContainerName: "gpu", GPUModelName: "A100",
		NodeName: "node-2", IntervalStart: day,
	})

	var n int
	var node string
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT count(*), max(node_name) FROM gpu_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = 'ml'`,
		orgID, clusterUUID).Scan(&n, &node))
	assert.Equal(t, 1, n)
	assert.Equal(t, "node-2", node)
}

func strPtr(s string) *string { return &s }

func ensureGPUDigestPartition(t *testing.T, pool *pgxpool.Pool, day time.Time) {
	t.Helper()
	monthStart := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := "gpu_container_digests_" + monthStart.Format("200601")
	_, err := pool.Exec(context.Background(), "CREATE TABLE IF NOT EXISTS "+partName+
		" PARTITION OF gpu_container_digests FOR VALUES FROM ('"+monthStart.Format("2006-01-02")+
		"') TO ('"+monthEnd.Format("2006-01-02")+"')")
	require.NoError(t, err)
}

func insertGPUDigestWithOrg(t *testing.T, pool *pgxpool.Pool, orgID *string, clusterUUID, ns string, day time.Time) {
	t.Helper()
	ensureGPUDigestPartition(t, pool, day)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO gpu_container_digests (
			interval_start, org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, gpu_model_name
		) VALUES ($1, $2, $3::uuid, $4, 'train', 'deployment', 'gpu', 'A100')`,
		day, orgID, clusterUUID, ns)
	require.NoError(t, err)
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
