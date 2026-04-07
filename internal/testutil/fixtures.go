package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func firstOfCurrentMonth() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// Deterministic test constants used across all test suites.
const (
	TestOrgID        = "org7654321"
	TestClusterUUID  = "11111111-1111-1111-1111-111111111111"
	TestNamespace    = "test-ns"
	TestWorkload     = "test-deploy"
	TestWorkloadType = "deployment"
	TestContainer    = "main"
)

// BaseDate is a fixed reference point for reproducible test data.
// Uses first of current month so partition auto-creation in migrations covers it.
var BaseDate = firstOfCurrentMonth()

// ContainerDigestRow holds the fields for a single daily_container_digests row.
type ContainerDigestRow struct {
	BucketDate       time.Time
	OrgID            string
	ClusterUUID      string
	Namespace        string
	Workload         string
	WorkloadType     string
	ContainerName    string
	CPURequestP50MC  int64
	CPURequestP95MC  int64
	CPUUsageP50MC    int64
	CPUUsageP95MC    int64
	CPUUsageP98MC    int64
	CPUUsageP99MC    int64
	CPUUsageMaxMC    int64
	CPUThrottleP95MC int64
	CPUThrottleMaxMC int64
	MemRequestP50KiB int64
	MemRequestP95KiB int64
	MemUsageP50KiB   int64
	MemUsageP95KiB   int64
	MemUsageMaxKiB   int64
	MemRSSP95KiB     int64
	MemRSSMaxKiB     int64
	OOMCountSum      int64
	CPUUsageMeanMC   int64
	MemUsageMeanKiB  int64
	SampleCount      int64
}

// SeedContainerDigest inserts a single row into daily_container_digests.
func SeedContainerDigest(t *testing.T, pool *pgxpool.Pool, row ContainerDigestRow) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO daily_container_digests (
			bucket_date, org_id, cluster_uuid, namespace, workload, workload_type, container_name,
			cpu_request_p50_mc, cpu_request_p95_mc,
			cpu_usage_p50_mc, cpu_usage_p95_mc, cpu_usage_p98_mc, cpu_usage_p99_mc, cpu_usage_max_mc,
			cpu_throttle_p95_mc, cpu_throttle_max_mc,
			memory_request_p50_kib, memory_request_p95_kib,
			memory_usage_p50_kib, memory_usage_p95_kib, memory_usage_max_kib,
			memory_rss_p95_kib, memory_rss_max_kib,
			oom_count_sum, cpu_usage_mean_mc, memory_usage_mean_kib, sample_count
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14,
			$15, $16, $17, $18, $19, $20, $21,
			$22, $23, $24, $25, $26, $27
		)
		ON CONFLICT (org_id, cluster_uuid, namespace, workload, container_name, bucket_date)
		DO UPDATE SET
			cpu_usage_p50_mc = EXCLUDED.cpu_usage_p50_mc,
			cpu_usage_p95_mc = EXCLUDED.cpu_usage_p95_mc,
			cpu_usage_max_mc = EXCLUDED.cpu_usage_max_mc,
			memory_usage_p50_kib = EXCLUDED.memory_usage_p50_kib,
			memory_usage_p95_kib = EXCLUDED.memory_usage_p95_kib,
			memory_usage_max_kib = EXCLUDED.memory_usage_max_kib,
			sample_count = EXCLUDED.sample_count`,
		row.BucketDate, row.OrgID, row.ClusterUUID, row.Namespace, row.Workload, row.WorkloadType, row.ContainerName,
		row.CPURequestP50MC, row.CPURequestP95MC,
		row.CPUUsageP50MC, row.CPUUsageP95MC, row.CPUUsageP98MC, row.CPUUsageP99MC, row.CPUUsageMaxMC,
		row.CPUThrottleP95MC, row.CPUThrottleMaxMC,
		row.MemRequestP50KiB, row.MemRequestP95KiB,
		row.MemUsageP50KiB, row.MemUsageP95KiB, row.MemUsageMaxKiB,
		row.MemRSSP95KiB, row.MemRSSMaxKiB,
		row.OOMCountSum, row.CPUUsageMeanMC, row.MemUsageMeanKiB, row.SampleCount,
	)
	if err != nil {
		t.Fatalf("SeedContainerDigest: %v", err)
	}
}

// SeedDigestSeries inserts N daily digest rows with linearly increasing values,
// starting from BaseDate, using the test constants for identity columns.
// The CPU usage P95 starts at baseCPU and increments by cpuStep per day.
// The memory usage P95 starts at baseMem and increments by memStep per day.
func SeedDigestSeries(t *testing.T, pool *pgxpool.Pool, days int, baseCPU, cpuStep, baseMem, memStep int64) {
	t.Helper()
	for i := 0; i < days; i++ {
		cpuVal := baseCPU + int64(i)*cpuStep
		memVal := baseMem + int64(i)*memStep
		SeedContainerDigest(t, pool, ContainerDigestRow{
			BucketDate:       BaseDate.AddDate(0, 0, i),
			OrgID:            TestOrgID,
			ClusterUUID:      TestClusterUUID,
			Namespace:        TestNamespace,
			Workload:         TestWorkload,
			WorkloadType:     TestWorkloadType,
			ContainerName:    TestContainer,
			CPURequestP50MC:  cpuVal - 20,
			CPURequestP95MC:  cpuVal + 10,
			CPUUsageP50MC:    cpuVal - 10,
			CPUUsageP95MC:    cpuVal,
			CPUUsageP98MC:    cpuVal + 5,
			CPUUsageP99MC:    cpuVal + 8,
			CPUUsageMaxMC:    cpuVal + 15,
			CPUThrottleP95MC: 5,
			CPUThrottleMaxMC: 10,
			MemRequestP50KiB: memVal - 1024,
			MemRequestP95KiB: memVal + 512,
			MemUsageP50KiB:   memVal - 512,
			MemUsageP95KiB:   memVal,
			MemUsageMaxKiB:   memVal + 1024,
			MemRSSP95KiB:     memVal - 256,
			MemRSSMaxKiB:     memVal + 512,
			OOMCountSum:      0,
			CPUUsageMeanMC:   cpuVal - 5,
			MemUsageMeanKiB:  memVal - 256,
			SampleCount:      96,
		})
	}
}
