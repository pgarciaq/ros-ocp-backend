package ingestion

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// W4 fleet-capacity ingestion tests (#392, #700). The parse/validation
// suite lives in librobne/csv (hcp_capacity_test.go — the shared parser
// with the robne CLI); this file pins the DB round-trip.

// hcpCapacityTestHeader mirrors the shared librobne/csv contract fixture.
const hcpCapacityTestHeader = "observed_at,mce_sampled_at,operator_version,ocp_version,mce_version," +
	"native_model_version,collection_interval_minutes,upload_cycle_minutes," +
	"inventory_complete,current_hc_count,ready_hcp_count," +
	"average_ready_hcp_qps,ready_hcp_qps_known,hosted_availability_basis," +
	"eligible_worker_cpu_allocatable_millicores,eligible_worker_memory_allocatable_bytes," +
	"eligible_worker_max_pods,worker_placement_known,pressure_state," +
	"mce_request_max_hcs,mce_low_qps_max_hcs,mce_average_qps_max_hcs," +
	"mce_medium_qps_max_hcs,mce_high_qps_max_hcs,shared_infrastructure"

// validHCPCapacityRow mirrors the hand-verified Task 2 fixture: 64 vCPU /
// 128 GiB / 1500 maxPods, 4 HCs, 2 ready, 300 QPS observed mean → stored
// scenarios request/low/average = 7 (memory_request), medium = 5
// (cpu_usage), high = 3 (cpu_usage).
const validHCPCapacityRow = "2026-10-25T14:59:59Z,2026-10-25T14:59:00Z,deadbeef,4.22.17,," +
	"hcp-published-sizing-v1,60,360,true,4,2,300,true,highly_available," +
	"64000,137438953472,1500,true,unknown,8,,8,4,2,false"

func hcpCapacityCSV(rows ...string) string {
	return hcpCapacityTestHeader + "\n" + strings.Join(rows, "\n") + "\n"
}

func TestProcessHCPCapacityCSV_DBStoreRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-hcp-capacity-" + t.Name()
	clusterUUID := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_capacity_history WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_capacity_history WHERE org_id = $1`, orgID+"-b")
	})

	require.NoError(t, EnsureHCPCapacityPartitions(ctx, pool, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)))
	require.NoError(t, ProcessHCPCapacityCSV(ctx, pool, strings.NewReader(hcpCapacityCSV(validHCPCapacityRow)), orgID, clusterUUID))
	// Replay idempotency: the monthly file accumulates hourly rows, so the
	// same window re-uploads on every packaging cycle.
	require.NoError(t, ProcessHCPCapacityCSV(ctx, pool, strings.NewReader(hcpCapacityCSV(validHCPCapacityRow)), orgID, clusterUUID))

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM hcp_capacity_history WHERE org_id = $1`, orgID).Scan(&count))
	assert.Equal(t, 1, count, "same-window re-ingest must upsert, not duplicate")

	// Scenarios computed at ingest via librobne/hcp (hand-verified Task 2
	// fixture): request/low/average = 7 memory_request, medium = 5
	// cpu_usage, high = 3 cpu_usage; signed headroom for high = -1.
	var reqMax, medMax, highMax int64
	var reqLimit, medLimit, highLimit string
	var highHeadroom *int64
	var mceReq, mceLow, mceMed *int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT request_max_hcs, request_limiting_resource,
		       medium_qps_max_hcs, medium_qps_limiting_resource,
		       high_qps_max_hcs, high_qps_limiting_resource,
		       mce_request_max_hcs, mce_low_qps_max_hcs, mce_medium_qps_max_hcs,
		       high_qps_max_hcs - current_hc_count
		FROM hcp_capacity_history WHERE org_id = $1`, orgID).
		Scan(&reqMax, &reqLimit, &medMax, &medLimit, &highMax, &highLimit,
			&mceReq, &mceLow, &mceMed, &highHeadroom))
	assert.Equal(t, int64(7), reqMax)
	assert.Equal(t, "memory_request", reqLimit)
	assert.Equal(t, int64(5), medMax)
	assert.Equal(t, "cpu_usage", medLimit)
	assert.Equal(t, int64(3), highMax)
	assert.Equal(t, "cpu_usage", highLimit)
	require.NotNil(t, highHeadroom)
	assert.Equal(t, int64(-1), *highHeadroom, "over-capacity headroom stays signed in SQL")
	require.NotNil(t, mceReq)
	assert.Equal(t, int64(8), *mceReq)
	assert.Nil(t, mceLow, "absent MCE reference stores NULL, never zero")
	require.NotNil(t, mceMed)
	assert.Equal(t, int64(4), *mceMed)

	// Tenant isolation: the same cluster UUID in another org is a
	// different row set — org_id is part of the key.
	require.NoError(t, ProcessHCPCapacityCSV(ctx, pool, strings.NewReader(hcpCapacityCSV(validHCPCapacityRow)), orgID+"-b", clusterUUID))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM hcp_capacity_history WHERE org_id = $1`, orgID+"-b").Scan(&count))
	assert.Equal(t, 1, count)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM hcp_capacity_history WHERE org_id = $1`, orgID).Scan(&count))
	assert.Equal(t, 1, count, "cross-org ingest must not touch the first org's rows")
}
