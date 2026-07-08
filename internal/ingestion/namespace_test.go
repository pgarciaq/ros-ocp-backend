package ingestion

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestParseNamespaceCSVRows_ValidRows(t *testing.T) {
	csv := strings.Join([]string{
		"interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_limit_namespace_sum,cpu_usage_namespace_avg,cpu_usage_namespace_max,cpu_usage_namespace_min,cpu_throttle_namespace_avg,cpu_throttle_namespace_max,memory_request_namespace_sum,memory_limit_namespace_sum,memory_usage_namespace_avg,memory_usage_namespace_max,memory_usage_namespace_min,memory_rss_usage_namespace_avg,memory_rss_usage_namespace_max",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,kube-system,0.500,1.000,0.250,0.400,0.100,0.010,0.020,1073741824,2147483648,536870912,805306368,268435456,268435456,536870912",
		"2026-03-20 01:00:00 +0000 UTC,2026-03-20 02:00:00 +0000 UTC,kube-system,0.600,1.200,0.300,0.500,0.150,0.020,0.040,1073741824,2147483648,536870912,805306368,268435456,268435456,536870912",
	}, "\n")

	rows, err := ParseNamespaceCSVRows(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	r := rows[0]
	if r.Namespace != "kube-system" {
		t.Errorf("expected namespace kube-system, got %s", r.Namespace)
	}
	if r.CPURequestSumMC != 500 {
		t.Errorf("expected CPURequestSumMC=500, got %d", r.CPURequestSumMC)
	}
	if r.CPULimitSumMC != 1000 {
		t.Errorf("expected CPULimitSumMC=1000, got %d", r.CPULimitSumMC)
	}
	if r.CPUUsageAvgMC != 250 {
		t.Errorf("expected CPUUsageAvgMC=250, got %d", r.CPUUsageAvgMC)
	}
	if r.CPUUsageMaxMC != 400 {
		t.Errorf("expected CPUUsageMaxMC=400, got %d", r.CPUUsageMaxMC)
	}
	if r.CPUUsageMinMC != 100 {
		t.Errorf("expected CPUUsageMinMC=100, got %d", r.CPUUsageMinMC)
	}
	// 1073741824 bytes = 1048576 KiB
	if r.MemRequestSumKiB != 1048576 {
		t.Errorf("expected MemRequestSumKiB=1048576, got %d", r.MemRequestSumKiB)
	}
	// 536870912 bytes = 524288 KiB
	if r.MemUsageAvgKiB != 524288 {
		t.Errorf("expected MemUsageAvgKiB=524288, got %d", r.MemUsageAvgKiB)
	}
}

func TestParseNamespaceCSVRows_MissingRequiredColumn(t *testing.T) {
	csv := "interval_start,interval_end,namespace,cpu_request_namespace_sum\n"
	_, err := ParseNamespaceCSVRows(strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error for missing required columns, got nil")
	}
	if !strings.Contains(err.Error(), "missing required column") {
		t.Errorf("expected 'missing required column' in error, got: %v", err)
	}
}

func TestParseNamespaceCSVRows_EmptyCSV(t *testing.T) {
	rows, err := ParseNamespaceCSVRows(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows != nil {
		t.Errorf("expected nil rows for empty CSV, got %d rows", len(rows))
	}
}

func TestParseNamespaceCSVRows_MalformedRowsSkipped(t *testing.T) {
	csv := strings.Join([]string{
		"interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg",
		"bad-date,2026-03-20 01:00:00 +0000 UTC,ns1,0.500,0.250,1073741824,536870912",
		"2026-03-20 01:00:00 +0000 UTC,2026-03-20 02:00:00 +0000 UTC,ns1,0.600,0.300,1073741824,536870912",
	}, "\n")

	rows, err := ParseNamespaceCSVRows(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row (malformed skipped), got %d", len(rows))
	}
	if rows[0].CPURequestSumMC != 600 {
		t.Errorf("expected CPURequestSumMC=600, got %d", rows[0].CPURequestSumMC)
	}
}

func TestParseNamespaceCSVRows_OptionalColumnsAbsent(t *testing.T) {
	csv := strings.Join([]string{
		"interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,ns-minimal,0.500,0.250,1073741824,536870912",
	}, "\n")

	rows, err := ParseNamespaceCSVRows(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.CPULimitSumMC != 0 {
		t.Errorf("expected CPULimitSumMC=0 (absent), got %d", r.CPULimitSumMC)
	}
	if r.CPUUsageMaxMC != 0 {
		t.Errorf("expected CPUUsageMaxMC=0 (absent), got %d", r.CPUUsageMaxMC)
	}
	if r.MemRSSAvgKiB != 0 {
		t.Errorf("expected MemRSSAvgKiB=0 (absent), got %d", r.MemRSSAvgKiB)
	}
	if r.CPURequestUsedMC != 0 || r.CPULimitUsedMC != 0 || r.MemoryRequestUsedBytes != 0 || r.MemoryLimitUsedBytes != 0 {
		t.Errorf("expected quota used fields 0 when columns absent, got used=%d/%d mem=%d/%d",
			r.CPURequestUsedMC, r.CPULimitUsedMC, r.MemoryRequestUsedBytes, r.MemoryLimitUsedBytes)
	}
}

func TestParseNamespaceCSVRows_QuotaUsedColumns(t *testing.T) {
	csv := strings.Join([]string{
		"interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_request_namespace_used,cpu_limit_namespace_sum,cpu_limit_namespace_used,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_request_namespace_used,memory_limit_namespace_sum,memory_limit_namespace_used,memory_usage_namespace_avg",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,app,2.000,1.500,4.000,3.000,0.500,2147483648,1073741824,4294967296,2147483648,536870912",
	}, "\n")

	rows, err := ParseNamespaceCSVRows(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.CPURequestUsedMC != 1500 {
		t.Errorf("CPURequestUsedMC: want 1500, got %d", r.CPURequestUsedMC)
	}
	if r.CPULimitUsedMC != 3000 {
		t.Errorf("CPULimitUsedMC: want 3000, got %d", r.CPULimitUsedMC)
	}
	if r.MemoryRequestUsedBytes != 1073741824 {
		t.Errorf("MemoryRequestUsedBytes: want 1073741824, got %d", r.MemoryRequestUsedBytes)
	}
	if r.MemoryLimitUsedBytes != 2147483648 {
		t.Errorf("MemoryLimitUsedBytes: want 2147483648, got %d", r.MemoryLimitUsedBytes)
	}
}

func TestComputeNamespaceDigest_QuotaUsedMaxPerDay(t *testing.T) {
	key := NamespaceDigestKey{
		OrgID: "org1", ClusterUUID: "cluster-1", Namespace: "app",
		BucketDate: time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
	}
	rows := []NamespaceMetricRow{
		{CPURequestHardMC: 1000, CPURequestUsedMC: 400, MemoryRequestHardBytes: 2048, MemoryRequestUsedBytes: 512},
		{CPURequestHardMC: 2000, CPURequestUsedMC: 900, MemoryRequestHardBytes: 4096, MemoryRequestUsedBytes: 1024},
	}
	d := ComputeNamespaceDigest(key, rows)
	if d.CPURequestHardMC != 2000 {
		t.Errorf("CPURequestHardMC max: want 2000, got %d", d.CPURequestHardMC)
	}
	if d.CPURequestUsedMC != 900 {
		t.Errorf("CPURequestUsedMC max: want 900, got %d", d.CPURequestUsedMC)
	}
	if d.MemoryRequestUsedBytes != 1024 {
		t.Errorf("MemoryRequestUsedBytes max: want 1024, got %d", d.MemoryRequestUsedBytes)
	}
}

func TestGroupNamespaceCSVRows(t *testing.T) {
	day1 := time.Date(2026, 3, 20, 1, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 3, 21, 2, 0, 0, 0, time.UTC)

	rows := []NamespaceMetricRow{
		{IntervalStart: day1, Namespace: "ns-a"},
		{IntervalStart: day1, Namespace: "ns-a"},
		{IntervalStart: day1, Namespace: "ns-b"},
		{IntervalStart: day2, Namespace: "ns-a"},
	}

	groups := GroupNamespaceCSVRows(rows, "org1", "cluster-1")
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}

	keyA1 := NamespaceDigestKey{
		OrgID:        "org1",
		ClusterUUID:  "cluster-1",
		Namespace:    "ns-a",
		BucketDate:   time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
		ScheduleType: ScheduleTypeAllHours,
	}
	if len(groups[keyA1]) != 2 {
		t.Errorf("expected 2 rows for ns-a day 2026-03-20, got %d", len(groups[keyA1]))
	}

	keyB1 := NamespaceDigestKey{
		OrgID:        "org1",
		ClusterUUID:  "cluster-1",
		Namespace:    "ns-b",
		BucketDate:   time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
		ScheduleType: ScheduleTypeAllHours,
	}
	if len(groups[keyB1]) != 1 {
		t.Errorf("expected 1 row for ns-b day 2026-03-20, got %d", len(groups[keyB1]))
	}

	keyA2 := NamespaceDigestKey{
		OrgID:        "org1",
		ClusterUUID:  "cluster-1",
		Namespace:    "ns-a",
		BucketDate:   time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC),
		ScheduleType: ScheduleTypeAllHours,
	}
	if len(groups[keyA2]) != 1 {
		t.Errorf("expected 1 row for ns-a day 2026-03-21, got %d", len(groups[keyA2]))
	}
}

func TestComputeNamespaceDigest(t *testing.T) {
	key := NamespaceDigestKey{
		OrgID:       "org1",
		ClusterUUID: "cluster-1",
		Namespace:   "default",
		BucketDate:  time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
	}

	rows := []NamespaceMetricRow{
		{CPURequestSumMC: 100, CPUUsageAvgMC: 50, CPUUsageMaxMC: 70, MemRequestSumKiB: 2048, MemUsageAvgKiB: 1024, MemUsageMaxKiB: 1500},
		{CPURequestSumMC: 200, CPUUsageAvgMC: 80, CPUUsageMaxMC: 90, MemRequestSumKiB: 3072, MemUsageAvgKiB: 2048, MemUsageMaxKiB: 2500},
		{CPURequestSumMC: 300, CPUUsageAvgMC: 60, CPUUsageMaxMC: 95, MemRequestSumKiB: 4096, MemUsageAvgKiB: 1536, MemUsageMaxKiB: 3000},
	}

	d := ComputeNamespaceDigest(key, rows)

	if d.SampleCount != 3 {
		t.Errorf("expected SampleCount=3, got %d", d.SampleCount)
	}

	// CPU usage mean: (50+80+60)/3 = 63
	if d.CPUUsageMeanMC != 63 {
		t.Errorf("expected CPUUsageMeanMC=63, got %d", d.CPUUsageMeanMC)
	}

	// Mem usage mean: (1024+2048+1536)/3 = 1536
	if d.MemUsageMeanKiB != 1536 {
		t.Errorf("expected MemUsageMeanKiB=1536, got %d", d.MemUsageMeanKiB)
	}

	// CPU usage max should come from the CPUUsageMaxMC column (95),
	// since the max of the per-interval max column (95) > max of avg column (80).
	if d.CPUUsageMaxMC != 95 {
		t.Errorf("expected CPUUsageMaxMC=95, got %d", d.CPUUsageMaxMC)
	}

	// Mem usage max should come from MemUsageMaxKiB column (3000).
	if d.MemUsageMaxKiB != 3000 {
		t.Errorf("expected MemUsageMaxKiB=3000, got %d", d.MemUsageMaxKiB)
	}

	if d.Key != key {
		t.Error("digest key should match input key")
	}
}

func TestComputeNamespaceDigest_SingleRow(t *testing.T) {
	key := NamespaceDigestKey{
		OrgID:       "org1",
		ClusterUUID: "cluster-1",
		Namespace:   "single",
		BucketDate:  time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
	}

	rows := []NamespaceMetricRow{
		{CPURequestSumMC: 500, CPUUsageAvgMC: 250, MemRequestSumKiB: 4096, MemUsageAvgKiB: 2048},
	}

	d := ComputeNamespaceDigest(key, rows)

	if d.SampleCount != 1 {
		t.Errorf("expected SampleCount=1, got %d", d.SampleCount)
	}
	// With a single value, all percentiles should equal that value.
	if d.CPURequestP50MC != 500 || d.CPURequestP60MC != 500 || d.CPURequestP95MC != 500 || d.CPURequestP98MC != 500 || d.CPURequestP99MC != 500 {
		t.Errorf("single-row CPU request percentiles should all be 500, got P50=%d P60=%d P95=%d P98=%d P99=%d",
			d.CPURequestP50MC, d.CPURequestP60MC, d.CPURequestP95MC, d.CPURequestP98MC, d.CPURequestP99MC)
	}
	if d.CPUUsageP50MC != 250 || d.CPUUsageP60MC != 250 || d.CPUUsageP95MC != 250 || d.CPUUsageP98MC != 250 || d.CPUUsageP99MC != 250 {
		t.Errorf("single-row CPU usage percentiles should all be 250, got P50=%d P60=%d P95=%d P98=%d P99=%d",
			d.CPUUsageP50MC, d.CPUUsageP60MC, d.CPUUsageP95MC, d.CPUUsageP98MC, d.CPUUsageP99MC)
	}
	if d.MemRequestP50KiB != 4096 || d.MemRequestP60KiB != 4096 || d.MemRequestP95KiB != 4096 || d.MemRequestP98KiB != 4096 || d.MemRequestP99KiB != 4096 {
		t.Errorf("single-row memory request percentiles should all be 4096, got P50=%d P60=%d P95=%d P98=%d P99=%d",
			d.MemRequestP50KiB, d.MemRequestP60KiB, d.MemRequestP95KiB, d.MemRequestP98KiB, d.MemRequestP99KiB)
	}
	if d.MemUsageP50KiB != 2048 || d.MemUsageP60KiB != 2048 || d.MemUsageP95KiB != 2048 || d.MemUsageP98KiB != 2048 || d.MemUsageP99KiB != 2048 {
		t.Errorf("single-row memory usage percentiles should all be 2048, got P50=%d P60=%d P95=%d P98=%d P99=%d",
			d.MemUsageP50KiB, d.MemUsageP60KiB, d.MemUsageP95KiB, d.MemUsageP98KiB, d.MemUsageP99KiB)
	}
}

func TestComputeNamespaceDigest_MaxFallbackToAvgColumn(t *testing.T) {
	key := NamespaceDigestKey{
		OrgID:       "org1",
		ClusterUUID: "cluster-1",
		Namespace:   "no-max",
		BucketDate:  time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
	}

	// CPUUsageMaxMC and MemUsageMaxKiB are zero (column absent in CSV).
	rows := []NamespaceMetricRow{
		{CPURequestSumMC: 100, CPUUsageAvgMC: 50, CPUUsageMaxMC: 0, MemRequestSumKiB: 2048, MemUsageAvgKiB: 1024, MemUsageMaxKiB: 0},
		{CPURequestSumMC: 200, CPUUsageAvgMC: 80, CPUUsageMaxMC: 0, MemRequestSumKiB: 3072, MemUsageAvgKiB: 2048, MemUsageMaxKiB: 0},
	}

	d := ComputeNamespaceDigest(key, rows)

	// Falls back to max of avg column: max(50, 80) = 80
	if d.CPUUsageMaxMC != 80 {
		t.Errorf("expected CPUUsageMaxMC=80 (fallback to avg max), got %d", d.CPUUsageMaxMC)
	}
	// Falls back to max of avg column: max(1024, 2048) = 2048
	if d.MemUsageMaxKiB != 2048 {
		t.Errorf("expected MemUsageMaxKiB=2048 (fallback to avg max), got %d", d.MemUsageMaxKiB)
	}
}

func TestBuildNSColumnIndex_ValidHeader(t *testing.T) {
	header := []string{
		"interval_start", "interval_end", "namespace",
		"cpu_request_namespace_sum", "cpu_usage_namespace_avg",
		"memory_request_namespace_sum", "memory_usage_namespace_avg",
		"cpu_limit_namespace_sum", "cpu_usage_namespace_max",
	}

	idx, err := buildNSColumnIndex(header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx.intervalStart != 0 {
		t.Errorf("expected intervalStart=0, got %d", idx.intervalStart)
	}
	if idx.namespace != 2 {
		t.Errorf("expected namespace=2, got %d", idx.namespace)
	}
	if idx.cpuLimitSum != 7 {
		t.Errorf("expected cpuLimitSum=7, got %d", idx.cpuLimitSum)
	}
	// memUsageMax should remain -1 (not in header)
	if idx.memUsageMax != -1 {
		t.Errorf("expected memUsageMax=-1 (absent), got %d", idx.memUsageMax)
	}
}

func TestBuildNSColumnIndex_MissingRequired(t *testing.T) {
	header := []string{"interval_start", "namespace", "cpu_request_namespace_sum"}
	_, err := buildNSColumnIndex(header)
	if err == nil {
		t.Fatal("expected error for missing required columns")
	}
}

func TestProcessNamespaceCSVToDigests_NoLongerWritesSamples(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	csvData := strings.Join([]string{
		"interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,default,0.500,0.250,1073741824,536870912",
		"2026-03-20 01:00:00 +0000 UTC,2026-03-20 02:00:00 +0000 UTC,default,0.600,0.300,1073741824,536870912",
		"2026-03-20 02:00:00 +0000 UTC,2026-03-20 03:00:00 +0000 UTC,kube-system,0.100,0.050,536870912,268435456",
	}, "\n")

	orgID := testutil.TestOrgID
	clusterUUID := testutil.TestClusterUUID

	err := ProcessNamespaceCSVToDigests(ctx, pool, strings.NewReader(csvData), orgID, clusterUUID)
	if err != nil {
		t.Fatalf("ProcessNamespaceCSVToDigests failed: %v", err)
	}

	var count int64
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM namespace_usage_samples WHERE org_id = $1`, orgID).Scan(&count)
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 namespace_usage_samples (writes disabled), got %d", count)
	}

	var digestCount int64
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM daily_namespace_digests WHERE org_id = $1`, orgID).Scan(&digestCount)
	if err != nil {
		t.Fatalf("digest count query failed: %v", err)
	}
	if digestCount != 2 {
		t.Errorf("expected 2 namespace digests (2 namespaces, 1 day each), got %d", digestCount)
	}
}
