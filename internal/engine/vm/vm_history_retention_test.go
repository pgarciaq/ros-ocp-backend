package vm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestVMRecHistory_Retention_PreservesRecentRows(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-vm-retain-" + time.Now().UTC().Format("150405")
	clusterID := testutil.TestClusterUUID

	t.Setenv("ROS_VM_REC_HISTORY_RETENTION_DAYS", "30")
	config.ResetForTest()
	_ = config.GetConfig()

	_, err := pool.Exec(ctx, `
		INSERT INTO vm_recommendation_history (
			org_id, cluster_id, vm_name, namespace, term, engine,
			recommended_vcpu, recommended_memory_gib, created_at
		) VALUES
			($1, $2, 'stale-vm', 'retain-ns', 'short_term', 'cost', 1, 1, NOW() - INTERVAL '60 days'),
			($1, $2, 'fresh-vm', 'retain-ns', 'short_term', 'cost', 2, 4, NOW() - INTERVAL '2 days')`,
		orgID, clusterID,
	)
	require.NoError(t, err)

	require.NoError(t, PruneVMRecommendationHistory(ctx, pool))

	var staleCount, freshCount int64
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM vm_recommendation_history
		WHERE org_id = $1 AND vm_name = 'stale-vm'`, orgID).Scan(&staleCount)
	require.NoError(t, err)
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM vm_recommendation_history
		WHERE org_id = $1 AND vm_name = 'fresh-vm'`, orgID).Scan(&freshCount)
	require.NoError(t, err)

	assert.Equal(t, int64(0), staleCount)
	assert.Equal(t, int64(1), freshCount)
}
