package model_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

const testOrgContainerKeysOrg = "org-container-keys-test"

func insertRecommendationSetRow(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, namespace, workload, workloadType, container, term, engine string,
	stale bool,
	updatedAt time.Time,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, term, engine, stale, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine)
		DO UPDATE SET stale = EXCLUDED.stale, updated_at = EXCLUDED.updated_at`,
		orgID, clusterUUID, namespace, workload, workloadType, container, term, engine, stale, updatedAt,
	)
	require.NoError(t, err)
}

func TestRefreshOrgContainerKeys_InsertsNewKeys(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testOrgContainerKeysOrg + "-insert"
	clusterUUID := testutil.TestClusterUUID
	updatedAt := time.Now().UTC().Add(-2 * time.Hour)

	insertRecommendationSetRow(t, ctx, pool, orgID, clusterUUID, "ns-a", "deploy-a", "Deployment", "ctr-a", "short", "cost", false, updatedAt)

	require.NoError(t, model.RefreshOrgContainerKeys(ctx, pool, orgID))

	var count int
	err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_container_keys WHERE org_id = $1`, orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	var workloadType string
	err = pool.QueryRow(ctx, `
		SELECT workload_type FROM org_container_keys
		WHERE org_id = $1 AND namespace = $2 AND workload = $3 AND container_name = $4`,
		orgID, "ns-a", "deploy-a", "ctr-a",
	).Scan(&workloadType)
	require.NoError(t, err)
	assert.Equal(t, "Deployment", workloadType)
}

func TestRefreshOrgContainerKeys_RemovesStaledKeys(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testOrgContainerKeysOrg + "-remove"
	clusterUUID := testutil.TestClusterUUID
	now := time.Now().UTC()

	insertRecommendationSetRow(t, ctx, pool, orgID, clusterUUID, "ns-a", "deploy-a", "Deployment", "ctr-a", "short", "cost", false, now)
	require.NoError(t, model.RefreshOrgContainerKeys(ctx, pool, orgID))

	_, err := pool.Exec(ctx, `
		UPDATE recommendation_sets SET stale = true
		WHERE org_id = $1 AND namespace = $2 AND workload = $3 AND container_name = $4`,
		orgID, "ns-a", "deploy-a", "ctr-a",
	)
	require.NoError(t, err)

	require.NoError(t, model.RefreshOrgContainerKeys(ctx, pool, orgID))

	var count int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_container_keys WHERE org_id = $1`, orgID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestRefreshOrgContainerKeys_UpdatesLastReported(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testOrgContainerKeysOrg + "-updated"
	clusterUUID := testutil.TestClusterUUID
	older := time.Now().UTC().Add(-48 * time.Hour)
	newer := time.Now().UTC().Add(-1 * time.Hour)

	insertRecommendationSetRow(t, ctx, pool, orgID, clusterUUID, "ns-a", "deploy-a", "Deployment", "ctr-a", "short", "cost", false, older)
	require.NoError(t, model.RefreshOrgContainerKeys(ctx, pool, orgID))

	insertRecommendationSetRow(t, ctx, pool, orgID, clusterUUID, "ns-a", "deploy-a", "Deployment", "ctr-a", "medium", "cost", false, newer)
	require.NoError(t, model.RefreshOrgContainerKeys(ctx, pool, orgID))

	var lastReported time.Time
	err := pool.QueryRow(ctx, `
		SELECT last_reported FROM org_container_keys
		WHERE org_id = $1 AND namespace = $2 AND workload = $3 AND container_name = $4`,
		orgID, "ns-a", "deploy-a", "ctr-a",
	).Scan(&lastReported)
	require.NoError(t, err)
	assert.WithinDuration(t, newer, lastReported, time.Second)
}

func TestRefreshOrgContainerKeys_PreservesResolvedTags(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testOrgContainerKeysOrg + "-tags"
	clusterUUID := testutil.TestClusterUUID
	now := time.Now().UTC()

	insertRecommendationSetRow(t, ctx, pool, orgID, clusterUUID, "ns-a", "deploy-a", "Deployment", "ctr-a", "short", "cost", false, now)
	require.NoError(t, model.RefreshOrgContainerKeys(ctx, pool, orgID))

	_, err := pool.Exec(ctx, `
		UPDATE org_container_keys
		SET resolved_tags = '{"app":"billing"}'::jsonb
		WHERE org_id = $1 AND namespace = $2 AND workload = $3 AND container_name = $4`,
		orgID, "ns-a", "deploy-a", "ctr-a",
	)
	require.NoError(t, err)

	insertRecommendationSetRow(t, ctx, pool, orgID, clusterUUID, "ns-a", "deploy-a", "Deployment", "ctr-a", "short", "cost", false, now.Add(time.Minute))
	require.NoError(t, model.RefreshOrgContainerKeys(ctx, pool, orgID))

	var tags string
	err = pool.QueryRow(ctx, `
		SELECT resolved_tags::text FROM org_container_keys
		WHERE org_id = $1 AND namespace = $2 AND workload = $3 AND container_name = $4`,
		orgID, "ns-a", "deploy-a", "ctr-a",
	).Scan(&tags)
	require.NoError(t, err)
	assert.Equal(t, `{"app": "billing"}`, tags)
}
