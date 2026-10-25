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

// The 24-column header is the operator-side contract (KMO
// TestHCPCapacityRow_Contract pins the same list); changes coordinate both
// sides plus the shared fixture.
const hcpCapacityTestHeader = "observed_at,mce_sampled_at,operator_version,ocp_version,mce_version," +
	"native_model_version,collection_interval_minutes,upload_cycle_minutes," +
	"inventory_complete,current_hc_count,ready_hcp_count," +
	"average_ready_hcp_qps,ready_hcp_qps_known,hosted_availability_basis," +
	"eligible_worker_cpu_allocatable_millicores,eligible_worker_memory_allocatable_bytes," +
	"eligible_worker_max_pods,worker_placement_known,pressure_state," +
	"mce_request_max_hcs,mce_low_qps_max_hcs,mce_average_qps_max_hcs," +
	"mce_medium_qps_max_hcs,mce_high_qps_max_hcs"

// validHCPCapacityRow mirrors the hand-verified Task 2 fixture: 64 vCPU /
// 128 GiB / 1500 maxPods, 4 HCs, 2 ready, 300 QPS observed mean → stored
// scenarios request/low/average = 7 (memory_request), medium = 5
// (cpu_usage), high = 3 (cpu_usage).
const validHCPCapacityRow = "2026-10-25T14:59:59Z,2026-10-25T14:59:00Z,deadbeef,4.22.17,," +
	"hcp-published-sizing-v1,60,360,true,4,2,300,true,highly_available," +
	"64000,137438953472,1500,true,unknown,8,,8,4,2"

func hcpCapacityCSV(rows ...string) string {
	return hcpCapacityTestHeader + "\n" + strings.Join(rows, "\n") + "\n"
}

func TestParseHCPCapacityRows_Valid(t *testing.T) {
	rows, skipped, err := parseHCPCapacityRows(strings.NewReader(hcpCapacityCSV(validHCPCapacityRow)), time.Now().UTC())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Zero(t, skipped)

	r := rows[0]
	assert.Equal(t, "2026-10-25T14:59:59Z", r.ObservedAt.Format(time.RFC3339))
	require.NotNil(t, r.MCESampledAt)
	assert.Equal(t, "2026-10-25T14:59:00Z", r.MCESampledAt.Format(time.RFC3339))
	assert.Equal(t, "deadbeef", r.OperatorVersion)
	assert.Equal(t, "4.22.17", r.OCPVersion)
	assert.Empty(t, r.MCEVersion)
	assert.True(t, r.InventoryComplete)
	assert.Equal(t, int64(4), r.CurrentHCCount)
	assert.Equal(t, int64(2), r.ReadyHCPCount)
	assert.InDelta(t, 300.0, r.AverageReadyHCPQPS, 1e-9)
	assert.True(t, r.ReadyHCPQPSKnown)
	assert.Equal(t, "highly_available", r.HostedAvailabilityBasis)
	assert.Equal(t, int64(64000), r.EligibleCPUMillicores)
	assert.Equal(t, int64(137438953472), r.EligibleMemoryBytes)
	assert.Equal(t, int64(1500), r.EligibleMaxPods)
	assert.True(t, r.WorkerPlacementKnown)
	require.NotNil(t, r.MCERequestMaxHCs)
	assert.Equal(t, int64(8), *r.MCERequestMaxHCs)
	assert.Nil(t, r.MCELowQPSMaxHCs, "absent MCE reference parses nil, never zero")
}

// Malformed or invalid rows skip with counters — never zero-filled, never
// a file abort: a poisoned capacity snapshot must not gate container
// recommendations via manifest completeness (SLO contract).
func TestParseHCPCapacityRows_SkipMalformed(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(10 * time.Minute).Format(time.RFC3339)
	good := "2026-10-25T14:59:59Z"
	bad := func(mutate func(fields []string) []string) string {
		f := strings.Split(validHCPCapacityRow, ",")
		return strings.Join(mutate(f), ",")
	}
	cases := []struct {
		name string
		row  string
		why  string
	}{
		{"bad timestamp", strings.Replace(validHCPCapacityRow, good, "not-a-time", 1), "unparseable observed_at skips"},
		{"future skew", strings.Replace(validHCPCapacityRow, good, future, 1), "operator-supplied observed_at more than 5 minutes ahead is rejected (cannot keep latest permanently fresh)"},
		{"bad boolean", bad(func(f []string) []string { f[8] = "yes"; return f }), "non-boolean inventory_complete skips"},
		{"bad int", bad(func(f []string) []string { f[9] = "four"; return f }), "unparseable count skips"},
		{"negative qps known", bad(func(f []string) []string { f[11] = "-3"; return f }), "negative QPS is measurement error"},
		{"nan qps known", bad(func(f []string) []string { f[11] = "NaN"; return f }), "non-finite QPS skips"},
		{"invalid basis", bad(func(f []string) []string { f[13] = "HighlyAvailable"; return f }), "raw controlPlaneTopology spellings are rejected — collector must aggregate to the enum"},
		{"wrong model version", bad(func(f []string) []string { f[5] = "hcp-published-sizing-v0"; return f }), "model version must match librobne/hcp — drift is a coordinate-both-sides event"},
		{"bad mce ref", bad(func(f []string) []string { f[19] = "eight"; return f }), "unparseable MCE reference skips"},
		{"bad pressure", bad(func(f []string) []string { f[18] = "clear"; return f }), "v1 never claims clear — only pressured/unknown"},
		{"short row", strings.Join(strings.Split(validHCPCapacityRow, ",")[:20], ","), "wrong field count skips"},
	}
	for _, tc := range cases {
		rows, skipped, err := parseHCPCapacityRows(strings.NewReader(hcpCapacityCSV(tc.row)), now)
		require.NoError(t, err, tc.name)
		assert.Empty(t, rows, "%s: %s", tc.name, tc.why)
		assert.Equal(t, 1, skipped, "%s: %s", tc.name, tc.why)
	}
}

func TestParseHCPCapacityRows_HeaderMismatch(t *testing.T) {
	_, _, err := parseHCPCapacityRows(strings.NewReader("observed_at,operator_version\n2026-10-25T14:59:59Z,deadbeef\n"), time.Now().UTC())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "columns")
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
