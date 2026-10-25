package model

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func seedHCPCapacityRow(t *testing.T, pool *pgxpool.Pool, orgID, observedAt string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO hcp_capacity_history (
			observed_at, org_id, cluster_uuid, operator_version, ocp_version, mce_version,
			native_model_version, collection_interval_minutes, upload_cycle_minutes,
			inventory_complete, current_hc_count, ready_hcp_count,
			average_ready_hcp_qps, ready_hcp_qps_known, hosted_availability_basis,
			eligible_worker_cpu_allocatable_millicores, eligible_worker_memory_allocatable_bytes,
			eligible_worker_max_pods, worker_placement_known, pressure_state,
			mce_request_max_hcs, mce_low_qps_max_hcs, mce_average_qps_max_hcs,
			mce_medium_qps_max_hcs, mce_high_qps_max_hcs,
			request_max_hcs, request_limiting_resource,
			low_qps_max_hcs, low_qps_limiting_resource,
			average_qps_max_hcs, average_qps_limiting_resource,
			medium_qps_max_hcs, medium_qps_limiting_resource,
			high_qps_max_hcs, high_qps_limiting_resource
		) VALUES ($1, $2, $3, 'deadbeef', '4.22.17', '', 'hcp-published-sizing-v1', 60, 360,
			true, 4, 2, 300, true, 'highly_available',
			64000, 137438953472, 1500, true, 'unknown',
			8, NULL, 8, 4, 2,
			7, 'memory_request', 7, 'memory_request', 7, 'memory_request',
			5, 'cpu_usage', 3, 'cpu_usage')`,
		observedAt, orgID, testutil.TestClusterUUID)
	require.NoError(t, err)
}

// Latest ordering, history ordering, and org-key isolation on the real
// table (#392 plan Task 4 Step 4). RBAC (user-permission scoping) lands
// with the Task 5 handlers; org isolation is the tenant boundary here.
func TestHCPCapacityModel_LatestHistoryOrgIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgA := "org-hcp-cap-model-a"
	orgB := "org-hcp-cap-model-b"
	clusterUUID := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_capacity_history WHERE org_id IN ($1, $2)`, orgA, orgB)
	})

	seedHCPCapacityRow(t, pool, orgA, "2026-10-25T13:59:59Z")
	seedHCPCapacityRow(t, pool, orgA, "2026-10-25T14:59:59Z")
	seedHCPCapacityRow(t, pool, orgB, "2026-10-25T15:59:59Z")

	latest, err := GetLatestHCPCapacity(ctx, pool, orgA, clusterUUID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, "2026-10-25T14:59:59Z", latest.ObservedAt.UTC().Format(time.RFC3339),
		"latest must be the newest observed_at for this org+cluster")

	history, total, err := GetHCPCapacityHistory(ctx, pool, orgA, clusterUUID, 10, 0)
	require.NoError(t, err)
	require.Len(t, history, 2)
	assert.Equal(t, int64(2), total)
	assert.True(t, history[0].ObservedAt.After(history[1].ObservedAt), "history is newest-first")

	// Org isolation: same cluster UUID, different org — a different row set.
	latestB, err := GetLatestHCPCapacity(ctx, pool, orgB, clusterUUID)
	require.NoError(t, err)
	require.NotNil(t, latestB)
	assert.Equal(t, "2026-10-25T15:59:59Z", latestB.ObservedAt.UTC().Format(time.RFC3339))

	// Absent org+cluster reads as no data, never an error and never another org's row.
	none, err := GetLatestHCPCapacity(ctx, pool, "org-hcp-cap-none", clusterUUID)
	require.NoError(t, err)
	assert.Nil(t, none)
}
