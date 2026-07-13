package vm

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestVMRecHistory_Appended(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-hist-" + uuid.New().String()[:8]
	clusterID := uuid.MustParse(testutil.TestClusterUUID)

	rec := model.VMRecommendation{
		OrgID:                orgID,
		ClusterUUID:          clusterID,
		VMName:               "hist-vm",
		Namespace:            "hist-ns",
		Term:                 "short_term",
		Engine:               "cost",
		RecommendedVCPU:      4,
		RecommendedMemoryGiB: 16,
		Confidence:           "high",
		LastRecommendedAt:    time.Now().UTC(),
	}
	inst := "u1.xlarge"
	rec.RecommendedInstanceType = &inst

	require.NoError(t, PersistVMRecommendations(ctx, pool, []model.VMRecommendation{rec}, nil))

	var count int64
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM vm_recommendation_history
		WHERE org_id = $1 AND cluster_id = $2 AND vm_name = $3 AND namespace = $4`,
		orgID, clusterID.String(), rec.VMName, rec.Namespace,
	).Scan(&count)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, int64(1))
}

func TestVMRecHistory_Retention(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID
	clusterID := testutil.TestClusterUUID

	_, err := pool.Exec(ctx, `
		INSERT INTO vm_recommendation_history (
			org_id, cluster_id, vm_name, namespace, term, engine,
			recommended_vcpu, recommended_memory_gib, created_at
		) VALUES ($1, $2, 'old-vm', 'old-ns', 'short_term', 'cost', 1, 1, NOW() - INTERVAL '120 days')`,
		orgID, clusterID,
	)
	require.NoError(t, err)
	require.NoError(t, PruneVMRecommendationHistory(ctx, pool))

	var count int64
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM vm_recommendation_history
		WHERE vm_name = 'old-vm' AND namespace = 'old-ns'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestVMRecHistory_Pagination(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-page-" + uuid.New().String()[:8]
	clusterID := testutil.TestClusterUUID
	vmName := "page-vm"
	ns := "page-ns"

	for i := 0; i < 3; i++ {
		_, err := pool.Exec(ctx, `
			INSERT INTO vm_recommendation_history (
				org_id, cluster_id, vm_name, namespace, term, engine,
				recommended_vcpu, recommended_memory_gib
			) VALUES ($1, $2, $3, $4, 'short_term', 'cost', $5, 8)`,
			orgID, clusterID, vmName, ns, i+1,
		)
		require.NoError(t, err)
	}

	rows, total, err := ListVMRecommendationHistory(ctx, pool, orgID, clusterID, vmName, ns, "short_term", "cost", 2, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, rows, 2)
}
