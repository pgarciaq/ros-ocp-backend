package main

import (
	"context"
	"net"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	modeltypes "github.com/redhatinsights/ros-ocp-backend/internal/model/types"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

func TestEmbeddedHeadPositive(t *testing.T) {
	t.Parallel()
	head, err := embeddedHead()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, head, uint(181))
}

func TestNativeContainerIDMatchesModel(t *testing.T) {
	t.Parallel()
	got := pgrec.NativeContainerID(testutil.TestClusterUUID, "ns", "wl", "deployment", "main")
	want := modeltypes.NativeContainerID(testutil.TestClusterUUID, "ns", "wl", "deployment", "main")
	assert.Equal(t, want, got)
}

func TestPersist_RefuseForeignSourceID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (org_id) VALUES ($1) ON CONFLICT (org_id) DO NOTHING`, "1234567")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, source_id, cluster_uuid, cluster_alias, last_reported_at)
		SELECT id, 'sources-uuid', $1, 'helm', now() FROM rh_accounts WHERE org_id = $2`,
		testutil.TestClusterUUID, "1234567")
	require.NoError(t, err)

	err = persistRecommendations(ctx, commonFlags{
		output:      poolDSN(t, pool, ""),
		applySchema: false,
	}, sampleResult())
	require.Error(t, err)
	assert.ErrorIs(t, err, pgrec.ErrForeignSourceID)
}

func TestPersist_UpsertContainerRec(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	result := sampleResult()
	err := persistRecommendations(ctx, commonFlags{
		output:      poolDSN(t, pool, ""),
		applySchema: false,
	}, result)
	require.NoError(t, err)

	var n int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3`,
		result.OrgID, result.ClusterID, "app").Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	var sourceID string
	err = pool.QueryRow(ctx, `
		SELECT c.source_id FROM clusters c
		JOIN rh_accounts ra ON ra.id = c.tenant_id
		WHERE ra.org_id = $1 AND c.cluster_uuid = $2`,
		result.OrgID, result.ClusterID).Scan(&sourceID)
	require.NoError(t, err)
	assert.Equal(t, pgrec.SourceID, sourceID)

	result.Recs[0].RecCPURequestMC = 99
	require.NoError(t, persistRecommendations(ctx, commonFlags{
		output:      poolDSN(t, pool, ""),
		applySchema: false,
	}, result))
	var cpu int64
	err = pool.QueryRow(ctx, `
		SELECT rec_cpu_request_millicores FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3`,
		result.OrgID, result.ClusterID, "app").Scan(&cpu)
	require.NoError(t, err)
	assert.Equal(t, int64(99), cpu)
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3`,
		result.OrgID, result.ClusterID, "app").Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestOpenPostgres_EmptyRequiresApplySchema(t *testing.T) {
	admin := testutil.SetupTestDB(t)
	dsn := createEmptyDatabase(t, admin)
	_, err := openPostgres(context.Background(), dsn, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--apply-schema")
	assert.Contains(t, err.Error(), "empty")
}

func TestOpenPostgres_EmptyWithApplySchema(t *testing.T) {
	admin := testutil.SetupTestDB(t)
	dsn := createEmptyDatabase(t, admin)
	pool, err := openPostgres(context.Background(), dsn, true)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	var n int
	err = pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM recommendation_sets`).Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestOpenPostgres_BehindRequiresApplySchema(t *testing.T) {
	admin := testutil.SetupTestDB(t)
	dsn := createEmptyDatabase(t, admin)
	m, err := newEmbeddedMigrate(dsn)
	require.NoError(t, err)
	require.NoError(t, m.Migrate(1))
	srcErr, dbErr := m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)

	_, err = openPostgres(context.Background(), dsn, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--apply-schema")
	assert.Contains(t, err.Error(), "1")

	upgraded, err := openPostgres(context.Background(), dsn, true)
	require.NoError(t, err)
	t.Cleanup(upgraded.Close)
	var n int
	err = upgraded.QueryRow(context.Background(), `SELECT COUNT(*) FROM recommendation_sets`).Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestOpenPostgres_Dirty(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	_, err := pool.Exec(context.Background(), `UPDATE schema_migrations SET dirty = true`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE schema_migrations SET dirty = false`)
	})
	_, err = openPostgres(context.Background(), poolDSN(t, pool, ""), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dirty")
}

func TestOpenPostgres_NewerThanBinary(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	_, err := pool.Exec(context.Background(), `UPDATE schema_migrations SET version = 999999`)
	require.NoError(t, err)
	t.Cleanup(func() {
		head, herr := embeddedHead()
		require.NoError(t, herr)
		_, _ = pool.Exec(context.Background(), `UPDATE schema_migrations SET version = $1`, head)
	})
	_, err = openPostgres(context.Background(), poolDSN(t, pool, ""), false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer")
}

func TestPersist_NonUUIDCluster(t *testing.T) {
	t.Parallel()
	err := persistRecommendations(context.Background(), commonFlags{output: "postgres://localhost/robne"}, recommendResult{
		OrgID:     "1234567",
		ClusterID: "local-cluster",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UUID")
}

func sampleResult() recommendResult {
	return recommendResult{
		OrgID:     "1234567",
		ClusterID: testutil.TestClusterUUID,
		Now:       time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC),
		Recs: []types.ContainerRec{{
			OrgID:            "1234567",
			ClusterUUID:      testutil.TestClusterUUID,
			Namespace:        "app",
			Workload:         "api",
			WorkloadType:     "deployment",
			ContainerName:    "api",
			Term:             "short",
			Engine:           "cost",
			RecCPURequestMC:  58,
			RecCPULimitMC:    61,
			RecMemRequestKiB: 58880,
			RecMemLimitKiB:   61824,
		}},
	}
}

func poolDSN(t *testing.T, pool *pgxpool.Pool, dbname string) string {
	t.Helper()
	c := pool.Config().ConnConfig
	if dbname == "" {
		dbname = c.Database
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.User, c.Password),
		Host:     net.JoinHostPort(c.Host, strconv.Itoa(int(c.Port))),
		Path:     "/" + dbname,
		RawQuery: "sslmode=disable",
	}
	return u.String()
}

func createEmptyDatabase(t *testing.T, admin *pgxpool.Pool) string {
	t.Helper()
	name := "robne_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := admin.Exec(context.Background(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	return poolDSN(t, admin, name)
}
