package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/pvc"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestWritePVCRecommendations_BatchUpsert(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	rec := pvc.PVCRec{
		OrgID:              testutil.TestOrgID,
		ClusterUUID:        testutil.TestClusterUUID,
		Namespace:          "pvc-batch",
		PVC:                "data-vol",
		StorageClass:       "gp3",
		CapacityBytes:      100 << 30,
		UsageBytesMax:      10 << 30,
		UsageRatio:         0.10,
		RecommendationType: pvc.PVCRecTypeOversized,
		DataDays:           7,
		Term:               "medium",
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM pvc_recommendation_sets WHERE org_id = $1 AND namespace = $2`, rec.OrgID, rec.Namespace)
	})

	require.NoError(t, pvc.WritePVCRecommendations(ctx, pool, []pvc.PVCRec{rec}, []string{"medium"}))

	var usageRatio float64
	err := pool.QueryRow(ctx, `
		SELECT usage_ratio FROM pvc_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3 AND persistentvolumeclaim = $4 AND term = $5`,
		rec.OrgID, rec.ClusterUUID, rec.Namespace, rec.PVC, rec.Term,
	).Scan(&usageRatio)
	require.NoError(t, err)
	assert.InDelta(t, 0.10, usageRatio, 0.001)

	rec.UsageRatio = 0.15
	rec.UsageBytesMax = 15 << 30
	require.NoError(t, pvc.WritePVCRecommendations(ctx, pool, []pvc.PVCRec{rec}, []string{"medium"}))

	var count int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM pvc_recommendation_sets
		WHERE org_id = $1 AND namespace = $2 AND persistentvolumeclaim = $3`,
		rec.OrgID, rec.Namespace, rec.PVC,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "batch upsert should update, not duplicate")

	err = pool.QueryRow(ctx, `
		SELECT usage_ratio FROM pvc_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3 AND persistentvolumeclaim = $4 AND term = $5`,
		rec.OrgID, rec.ClusterUUID, rec.Namespace, rec.PVC, rec.Term,
	).Scan(&usageRatio)
	require.NoError(t, err)
	assert.InDelta(t, 0.15, usageRatio, 0.001)
}

func TestWritePVCRecommendations_CleanupStaleTerms(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	orgID := testutil.TestOrgID
	clusterUUID := testutil.TestClusterUUID
	namespace := "pvc-stale-terms"
	pvcName := "data-vol"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM pvc_recommendation_sets WHERE org_id = $1 AND namespace = $2`, orgID, namespace)
	})

	base := pvc.PVCRec{
		OrgID:              orgID,
		ClusterUUID:        clusterUUID,
		Namespace:          namespace,
		PVC:                pvcName,
		RecommendationType: pvc.PVCRecTypeHealthy,
		DataDays:           7,
	}
	short := base
	short.Term = "short"
	long := base
	long.Term = "long"

	require.NoError(t, pvc.WritePVCRecommendations(ctx, pool, []pvc.PVCRec{short, long}, []string{"short", "long"}))

	var termCount int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM pvc_recommendation_sets
		WHERE org_id = $1 AND namespace = $2 AND persistentvolumeclaim = $3`,
		orgID, namespace, pvcName,
	).Scan(&termCount)
	require.NoError(t, err)
	assert.Equal(t, 2, termCount)

	require.NoError(t, pvc.WritePVCRecommendations(ctx, pool, []pvc.PVCRec{short}, []string{"short"}))

	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM pvc_recommendation_sets
		WHERE org_id = $1 AND namespace = $2 AND persistentvolumeclaim = $3`,
		orgID, namespace, pvcName,
	).Scan(&termCount)
	require.NoError(t, err)
	assert.Equal(t, 1, termCount, "stale term rows should be deleted after batch write")

	var remainingTerm string
	err = pool.QueryRow(ctx, `
		SELECT term FROM pvc_recommendation_sets
		WHERE org_id = $1 AND namespace = $2 AND persistentvolumeclaim = $3`,
		orgID, namespace, pvcName,
	).Scan(&remainingTerm)
	require.NoError(t, err)
	assert.Equal(t, "short", remainingTerm)
}
