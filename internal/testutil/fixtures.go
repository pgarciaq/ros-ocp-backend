package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func sevenDaysAgo() time.Time {
	return time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -7)
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
// Set to 7 days ago so that a 7-day digest series ending at BaseDate+6
// (i.e. yesterday) is always within the 3-day staleness window, preventing
// integration tests from producing stale recommendations that the GORM
// query filters out.
var BaseDate = sevenDaysAgo()

// RecentStart returns a start date 7 days ago (UTC, day-truncated). Use this
// instead of BaseDate when seeding data for API integration tests that filter
// on stale=false (staleness threshold is 3 days from the latest digest).
func RecentStart() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -7)
}

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
	MemRequestP60KiB int64
	MemRequestP95KiB int64
	MemRequestP98KiB int64
	MemRequestP99KiB int64
	MemUsageP50KiB   int64
	MemUsageP60KiB   int64
	MemUsageP95KiB   int64
	MemUsageP98KiB   int64
	MemUsageP99KiB   int64
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
			memory_request_p50_kib, memory_request_p60_kib, memory_request_p95_kib, memory_request_p98_kib, memory_request_p99_kib,
			memory_usage_p50_kib, memory_usage_p60_kib, memory_usage_p95_kib, memory_usage_p98_kib, memory_usage_p99_kib, memory_usage_max_kib,
			memory_rss_p95_kib, memory_rss_max_kib,
			oom_count_sum, cpu_usage_mean_mc, memory_usage_mean_kib, sample_count
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14,
			$15, $16,
			$17, $18, $19, $20, $21,
			$22, $23, $24, $25, $26, $27,
			$28, $29,
			$30, $31, $32, $33
		)
		ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, bucket_date)
		DO UPDATE SET
			cpu_usage_p50_mc = EXCLUDED.cpu_usage_p50_mc,
			cpu_usage_p95_mc = EXCLUDED.cpu_usage_p95_mc,
			cpu_usage_max_mc = EXCLUDED.cpu_usage_max_mc,
			memory_usage_p50_kib = EXCLUDED.memory_usage_p50_kib,
			memory_usage_p60_kib = EXCLUDED.memory_usage_p60_kib,
			memory_usage_p95_kib = EXCLUDED.memory_usage_p95_kib,
			memory_usage_max_kib = EXCLUDED.memory_usage_max_kib,
			sample_count = EXCLUDED.sample_count`,
		row.BucketDate, row.OrgID, row.ClusterUUID, row.Namespace, row.Workload, row.WorkloadType, row.ContainerName,
		row.CPURequestP50MC, row.CPURequestP95MC,
		row.CPUUsageP50MC, row.CPUUsageP95MC, row.CPUUsageP98MC, row.CPUUsageP99MC, row.CPUUsageMaxMC,
		row.CPUThrottleP95MC, row.CPUThrottleMaxMC,
		row.MemRequestP50KiB, row.MemRequestP60KiB, row.MemRequestP95KiB, row.MemRequestP98KiB, row.MemRequestP99KiB,
		row.MemUsageP50KiB, row.MemUsageP60KiB, row.MemUsageP95KiB, row.MemUsageP98KiB, row.MemUsageP99KiB, row.MemUsageMaxKiB,
		row.MemRSSP95KiB, row.MemRSSMaxKiB,
		row.OOMCountSum, row.CPUUsageMeanMC, row.MemUsageMeanKiB, row.SampleCount,
	)
	if err != nil {
		t.Fatalf("SeedContainerDigest: %v", err)
	}
}

// SeedDigestSeries inserts N daily digest rows starting from BaseDate.
// For API integration tests that filter on stale=false, use SeedDigestSeriesFrom
// with RecentStart() instead.
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
			MemRequestP60KiB: memVal - 512,
			MemRequestP95KiB: memVal + 512,
			MemRequestP98KiB: memVal + 768,
			MemRequestP99KiB: memVal + 900,
			MemUsageP50KiB:   memVal - 512,
			MemUsageP60KiB:   memVal,
			MemUsageP95KiB:   memVal + 512,
			MemUsageP98KiB:   memVal + 768,
			MemUsageP99KiB:   memVal + 900,
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

// SeedNamespaceDigestSeries inserts N daily namespace digest rows starting
// from BaseDate for the given namespace.
func SeedNamespaceDigestSeries(t *testing.T, pool *pgxpool.Pool, namespace string, days int, baseCPU, cpuStep, baseMem, memStep int64) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < days; i++ {
		cpuVal := baseCPU + int64(i)*cpuStep
		memVal := baseMem + int64(i)*memStep
		_, err := pool.Exec(ctx, `
			INSERT INTO daily_namespace_digests (
				bucket_date, org_id, cluster_uuid, namespace,
				cpu_request_p50_mc, cpu_request_p95_mc,
				cpu_usage_p50_mc, cpu_usage_p95_mc, cpu_usage_max_mc,
				memory_request_p50_kib, memory_request_p95_kib,
				memory_usage_p50_kib, memory_usage_p95_kib, memory_usage_max_kib,
				cpu_usage_mean_mc, memory_usage_mean_kib, sample_count
			) VALUES (
				$1, $2, $3, $4,
				$5, $6, $7, $8, $9,
				$10, $11, $12, $13, $14,
				$15, $16, $17
			)
			ON CONFLICT (org_id, cluster_uuid, namespace, bucket_date)
			DO UPDATE SET cpu_usage_p50_mc = EXCLUDED.cpu_usage_p50_mc`,
			BaseDate.AddDate(0, 0, i), TestOrgID, TestClusterUUID, namespace,
			cpuVal-20, cpuVal+10,
			cpuVal-10, cpuVal, cpuVal+15,
			memVal-1024, memVal+512,
			memVal-512, memVal, memVal+1024,
			cpuVal-5, memVal-256, int64(96),
		)
		if err != nil {
			t.Fatalf("SeedNamespaceDigestSeries: %v", err)
		}
	}
}

// SeedNamespaceDigestSeriesFull inserts N daily namespace digest rows including
// all P60/P98/P99 percentile columns. Use for tests that exercise exact
// percentile selection (SelectMemUsagePercentile, SelectCPUUsagePercentile).
func SeedNamespaceDigestSeriesFull(t *testing.T, pool *pgxpool.Pool, namespace string, days int, baseCPU, cpuStep, baseMem, memStep int64) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < days; i++ {
		cpuVal := baseCPU + int64(i)*cpuStep
		memVal := baseMem + int64(i)*memStep
		_, err := pool.Exec(ctx, `
			INSERT INTO daily_namespace_digests (
				bucket_date, org_id, cluster_uuid, namespace,
				cpu_request_p50_mc, cpu_request_p60_mc, cpu_request_p95_mc, cpu_request_p99_mc,
				cpu_usage_p50_mc, cpu_usage_p60_mc, cpu_usage_p95_mc, cpu_usage_p98_mc, cpu_usage_p99_mc, cpu_usage_max_mc,
				memory_request_p50_kib, memory_request_p60_kib, memory_request_p95_kib,
				memory_usage_p50_kib, memory_usage_p60_kib, memory_usage_p95_kib, memory_usage_p98_kib, memory_usage_p99_kib, memory_usage_max_kib,
				cpu_usage_mean_mc, memory_usage_mean_kib, sample_count
			) VALUES (
				$1, $2, $3, $4,
				$5, $6, $7, $8,
				$9, $10, $11, $12, $13, $14,
				$15, $16, $17,
				$18, $19, $20, $21, $22, $23,
				$24, $25, $26
			)
			ON CONFLICT (org_id, cluster_uuid, namespace, bucket_date)
			DO UPDATE SET cpu_usage_p50_mc = EXCLUDED.cpu_usage_p50_mc`,
			BaseDate.AddDate(0, 0, i), TestOrgID, TestClusterUUID, namespace,
			cpuVal-20, cpuVal-10, cpuVal+10, cpuVal+20,
			cpuVal-10, cpuVal, cpuVal+10, cpuVal+15, cpuVal+18, cpuVal+25,
			memVal-1024, memVal-512, memVal+512,
			memVal-512, memVal, memVal+512, memVal+768, memVal+900, memVal+1024,
			cpuVal-5, memVal-256, int64(96),
		)
		if err != nil {
			t.Fatalf("SeedNamespaceDigestSeriesFull: %v", err)
		}
	}
}

// GPUDigestRow holds the fields for a single gpu_container_digests row.
type GPUDigestRow struct {
	IntervalStart       time.Time
	ClusterUUID         string
	Namespace           string
	Workload            string
	WorkloadType        string
	ContainerName       string
	GPUModelName        string
	GPUProfileName      string
	NodeName            string
	FBUsageMinMiB       float64
	FBUsageMaxMiB       float64
	FBUsageAvgMiB       float64
	TensorPipeActiveMin float64
	TensorPipeActiveMax float64
	TensorPipeActiveAvg float64
	DRAMActiveMin       float64
	DRAMActiveMax       float64
	DRAMActiveAvg       float64
	SMActiveMin         float64
	SMActiveMax         float64
	SMActiveAvg         float64
}

// SeedGPUDigest inserts a single row into gpu_container_digests.
func SeedGPUDigest(t *testing.T, pool *pgxpool.Pool, row GPUDigestRow) {
	t.Helper()
	ctx := context.Background()
	monthStart := time.Date(row.IntervalStart.Year(), row.IntervalStart.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := "gpu_container_digests_" + monthStart.Format("200601")
	_, _ = pool.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+partName+
		" PARTITION OF gpu_container_digests FOR VALUES FROM ('"+monthStart.Format("2006-01-02")+
		"') TO ('"+monthEnd.Format("2006-01-02")+"')")

	_, err := pool.Exec(ctx, `
		INSERT INTO gpu_container_digests (
			interval_start, cluster_uuid, namespace, workload, workload_type, container_name,
			gpu_model_name, gpu_profile_name, node_name,
			fb_usage_min_mib, fb_usage_max_mib, fb_usage_avg_mib,
			tensor_pipe_active_min, tensor_pipe_active_max, tensor_pipe_active_avg,
			dram_active_min, dram_active_max, dram_active_avg,
			sm_active_min, sm_active_max, sm_active_avg
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		ON CONFLICT (cluster_uuid, namespace, workload, container_name, gpu_model_name, interval_start)
		DO UPDATE SET node_name = EXCLUDED.node_name`,
		row.IntervalStart, row.ClusterUUID, row.Namespace, row.Workload, row.WorkloadType, row.ContainerName,
		row.GPUModelName, row.GPUProfileName, row.NodeName,
		row.FBUsageMinMiB, row.FBUsageMaxMiB, row.FBUsageAvgMiB,
		row.TensorPipeActiveMin, row.TensorPipeActiveMax, row.TensorPipeActiveAvg,
		row.DRAMActiveMin, row.DRAMActiveMax, row.DRAMActiveAvg,
		row.SMActiveMin, row.SMActiveMax, row.SMActiveAvg,
	)
	if err != nil {
		t.Fatalf("SeedGPUDigest: %v", err)
	}
}

// SeedDigestSeriesFrom is like SeedDigestSeries but starts from the given date
// instead of BaseDate. Use with RecentStart() for API integration tests.
func SeedDigestSeriesFrom(t *testing.T, pool *pgxpool.Pool, start time.Time, days int, baseCPU, cpuStep, baseMem, memStep int64) {
	t.Helper()
	for i := 0; i < days; i++ {
		cpuVal := baseCPU + int64(i)*cpuStep
		memVal := baseMem + int64(i)*memStep
		SeedContainerDigest(t, pool, ContainerDigestRow{
			BucketDate:       start.AddDate(0, 0, i),
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
			MemRequestP60KiB: memVal - 512,
			MemRequestP95KiB: memVal + 512,
			MemRequestP98KiB: memVal + 768,
			MemRequestP99KiB: memVal + 900,
			MemUsageP50KiB:   memVal - 512,
			MemUsageP60KiB:   memVal,
			MemUsageP95KiB:   memVal + 512,
			MemUsageP98KiB:   memVal + 768,
			MemUsageP99KiB:   memVal + 900,
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
