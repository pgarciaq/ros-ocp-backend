package gpu

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestGpuMIGListSortColumn(t *testing.T) {
	tests := []struct {
		orderBy string
		want    string
	}{
		{"cluster_uuid", "m.cluster_uuid::text"},
		{"namespace", "m.namespace"},
		{"workload", "m.workload"},
		{"container", "m.container_name"},
		{"gpu_model", "m.gpu_model_name"},
		{"term", "m.term"},
		{"confidence", "m.confidence"},
		{"gpu_idle_state", "m.gpu_idle_state"},
		{"unknown", "m.cluster_uuid::text"},
	}
	for _, tt := range tests {
		t.Run(tt.orderBy, func(t *testing.T) {
			got := gpuMIGListSortColumn(tt.orderBy)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAppendGPUMIGFilters(t *testing.T) {
	t.Run("no filters", func(t *testing.T) {
		q, args, idx := appendGPUMIGFilters("SELECT 1", []any{"org1"}, 2, GPUMIGListFilters{})
		assert.Equal(t, "SELECT 1", q)
		assert.Len(t, args, 1)
		assert.Equal(t, 2, idx)
	})

	t.Run("all filters", func(t *testing.T) {
		f := GPUMIGListFilters{
			ClusterUUIDs:  []string{"c1", "c2"},
			Namespaces:    []string{"ns1"},
			Workloads:     []string{"wl1"},
			Term:          "short",
			GPUIdleStates: []string{"idle", "zombie"},
		}
		q, args, idx := appendGPUMIGFilters("SELECT 1", []any{"org1"}, 2, f)
		assert.Contains(t, q, "cluster_uuid")
		assert.Contains(t, q, "namespace")
		assert.Contains(t, q, "workload")
		assert.Contains(t, q, "term")
		assert.Contains(t, q, "gpu_idle_state")
		assert.Len(t, args, 6)
		assert.Equal(t, 7, idx)
	})
}

// Same cluster UUID registered under two tenants (e.g. a cloned cluster
// re-registered under a different account). #525: the clusters alias join
// must predicate c.org_id so orgA never sees orgB's alias. Pre-fix the join
// fans out to both tenants' rows (duplicating list rows) and the alias is
// whichever row Postgres happens to return.
func TestListGPUMIGRecommendationSets_CollidingClusterUUIDUsesOwnOrgAlias(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	const (
		otherOrgID = "org-mig-collide-b"
		sharedUUID = "33333333-3333-3333-3333-333333333333"
	)

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (11, $1), (12, $2) ON CONFLICT DO NOTHING`,
		testutil.TestOrgID, otherOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, org_id, last_reported_at)
		VALUES (11, $1::uuid, 'alias-tenant-a', 'src-mig-a', $2, now()),
		       (12, $1::uuid, 'alias-tenant-b', 'src-mig-b', $3, now())
		ON CONFLICT DO NOTHING`, sharedUUID, testutil.TestOrgID, otherOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO gpu_mig_recommendation_sets
		(org_id, cluster_uuid, namespace, workload, workload_type, container_name, node_name, gpu_model_name, term, recommended_gpu_profile)
		VALUES ($1, $2::uuid, 'ns-a', 'wl-a', 'deployment', 'ctr-a', 'node-a', 'A100', 'short', '1g.5gb')`,
		testutil.TestOrgID, sharedUUID)
	require.NoError(t, err)

	rows, err := ListGPUMIGRecommendationSets(ctx, pool, testutil.TestOrgID, GPUMIGListFilters{}, "cluster_uuid", false, 10, 0, nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, sharedUUID, rows[0].ClusterUUID)
	assert.Equal(t, "alias-tenant-a", rows[0].ClusterAlias)
}
