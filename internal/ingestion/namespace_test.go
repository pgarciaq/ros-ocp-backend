package ingestion

import (
	"strings"
	"testing"
	"time"
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
	if r.CPURequestMC != 500 {
		t.Errorf("expected CPURequestMC=500, got %d", r.CPURequestMC)
	}
	if r.CPULimitMC != 1000 {
		t.Errorf("expected CPULimitMC=1000, got %d", r.CPULimitMC)
	}
	if r.CPUUsageMC != 250 {
		t.Errorf("expected CPUUsageMC=250, got %d", r.CPUUsageMC)
	}
	if r.CPUUsageMaxMC != 400 {
		t.Errorf("expected CPUUsageMaxMC=400, got %d", r.CPUUsageMaxMC)
	}
	if r.CPUUsageMinMC != 100 {
		t.Errorf("expected CPUUsageMinMC=100, got %d", r.CPUUsageMinMC)
	}
	if r.CPUThrottleAvgMC != 10 {
		t.Errorf("expected CPUThrottleAvgMC=10, got %d", r.CPUThrottleAvgMC)
	}
	if r.CPUThrottleMaxMC != 20 {
		t.Errorf("expected CPUThrottleMaxMC=20, got %d", r.CPUThrottleMaxMC)
	}
	if r.MemUsageMinKiB != 262144 {
		t.Errorf("expected MemUsageMinKiB=262144, got %d", r.MemUsageMinKiB)
	}
	if r.MemRSSKiB != 262144 {
		t.Errorf("expected MemRSSKiB=262144, got %d", r.MemRSSKiB)
	}
	if r.MemRSSMaxKiB != 524288 {
		t.Errorf("expected MemRSSMaxKiB=524288, got %d", r.MemRSSMaxKiB)
	}
	if r.CPURequestHardMC != 500 {
		t.Errorf("expected CPURequestHardMC=500 (from sum), got %d", r.CPURequestHardMC)
	}
	if r.MemoryRequestHardBytes != 1073741824 {
		t.Errorf("expected MemoryRequestHardBytes=1073741824, got %d", r.MemoryRequestHardBytes)
	}
	// 1073741824 bytes = 1048576 KiB
	if r.MemRequestKiB != 1048576 {
		t.Errorf("expected MemRequestKiB=1048576, got %d", r.MemRequestKiB)
	}
	// 536870912 bytes = 524288 KiB
	if r.MemUsageKiB != 524288 {
		t.Errorf("expected MemUsageKiB=524288, got %d", r.MemUsageKiB)
	}
}

func TestParseNamespaceCSVRows_MissingRequiredColumn(t *testing.T) {
	csv := "interval_start,interval_end,namespace,cpu_request_namespace_sum\n"
	_, err := ParseNamespaceCSVRows(strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error for missing required columns, got nil")
	}
	if !strings.Contains(err.Error(), "missing columns") {
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
	before := csvRowsSkippedTotal("namespace")
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
	if rows[0].CPURequestMC != 600 {
		t.Errorf("expected CPURequestMC=600, got %d", rows[0].CPURequestMC)
	}
	if got := csvRowsSkippedTotal("namespace"); got != before+1 {
		t.Errorf("expected skipped counter +1, before=%v got=%v", before, got)
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
	if r.CPULimitMC != 0 {
		t.Errorf("expected CPULimitMC=0 (absent), got %d", r.CPULimitMC)
	}
	if r.CPUUsageMaxMC != 0 {
		t.Errorf("expected CPUUsageMaxMC=0 (absent), got %d", r.CPUUsageMaxMC)
	}
	if r.MemRSSKiB != 0 {
		t.Errorf("expected MemRSSKiB=0 (absent), got %d", r.MemRSSKiB)
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
		{CPURequestMC: 100, CPUUsageMC: 50, CPUUsageMaxMC: 70, MemRequestKiB: 2048, MemUsageKiB: 1024, MemUsageMaxKiB: 1500},
		{CPURequestMC: 200, CPUUsageMC: 80, CPUUsageMaxMC: 90, MemRequestKiB: 3072, MemUsageKiB: 2048, MemUsageMaxKiB: 2500},
		{CPURequestMC: 300, CPUUsageMC: 60, CPUUsageMaxMC: 95, MemRequestKiB: 4096, MemUsageKiB: 1536, MemUsageMaxKiB: 3000},
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
		{CPURequestMC: 500, CPUUsageMC: 250, MemRequestKiB: 4096, MemUsageKiB: 2048},
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
		{CPURequestMC: 100, CPUUsageMC: 50, CPUUsageMaxMC: 0, MemRequestKiB: 2048, MemUsageKiB: 1024, MemUsageMaxKiB: 0},
		{CPURequestMC: 200, CPUUsageMC: 80, CPUUsageMaxMC: 0, MemRequestKiB: 3072, MemUsageKiB: 2048, MemUsageMaxKiB: 0},
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

func TestParseNamespaceCSVRows_OptionalLimitAndMaxPresent(t *testing.T) {
	csv := strings.Join([]string{
		"interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg,cpu_limit_namespace_sum,cpu_usage_namespace_max",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,ns1,0.500,0.250,1073741824,536870912,1.000,0.400",
	}, "\n")
	rows, err := ParseNamespaceCSVRows(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.CPULimitMC != 1000 {
		t.Errorf("expected CPULimitMC=1000, got %d", r.CPULimitMC)
	}
	if r.CPUUsageMaxMC != 400 {
		t.Errorf("expected CPUUsageMaxMC=400, got %d", r.CPUUsageMaxMC)
	}
	if r.MemUsageMaxKiB != 0 {
		t.Errorf("expected MemUsageMaxKiB=0 (column absent), got %d", r.MemUsageMaxKiB)
	}
}

func TestValidateNamespaceMetricRow_NegativeRejected(t *testing.T) {
	err := ValidateNamespaceMetricRow(NamespaceMetricRow{CPURequestMC: -1})
	if err == nil {
		t.Fatal("expected error for negative CPURequestMC")
	}
	if !strings.Contains(err.Error(), "CPURequestMC") {
		t.Errorf("expected CPURequestMC in error, got: %v", err)
	}
}
