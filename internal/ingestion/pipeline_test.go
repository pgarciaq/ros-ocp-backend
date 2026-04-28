package ingestion

import (
	"context"
	"strings"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessCSVToDigests_ValidCSV(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	csv := csvHeader + "\n" +
		csvRow("2026-04-01 00:00:00 +0000 UTC", "2026-04-01 00:15:00 +0000 UTC", "test-ns", "pod-1", "test-deploy", "deployment", "main", "0.1", "0.15", "0.08", "0.001", "134217728", "134217728", "104857600", "100000000", "0") + "\n" +
		csvRow("2026-04-01 00:15:00 +0000 UTC", "2026-04-01 00:30:00 +0000 UTC", "test-ns", "pod-1", "test-deploy", "deployment", "main", "0.1", "0.15", "0.09", "0.001", "134217728", "134217728", "110000000", "105000000", "0")

	reader := strings.NewReader(csv)
	err := ProcessCSVToDigests(ctx, pool, reader, "org-test-pipeline", "11111111-1111-1111-1111-111111111111")
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1`,
		"org-test-pipeline").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestProcessCSVToDigests_Empty(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	reader := strings.NewReader("")
	err := ProcessCSVToDigests(ctx, pool, reader, "org-empty", "11111111-1111-1111-1111-111111111111")
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1`,
		"org-empty").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestProcessCSVToDigests_MultipleContainers(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	csv := csvHeader + "\n" +
		csvRow("2026-04-01 00:00:00 +0000 UTC", "2026-04-01 00:15:00 +0000 UTC", "ns1", "pod-a", "deploy-a", "deployment", "main", "0.1", "0.15", "0.08", "0.001", "134217728", "134217728", "104857600", "100000000", "0") + "\n" +
		csvRow("2026-04-01 00:00:00 +0000 UTC", "2026-04-01 00:15:00 +0000 UTC", "ns2", "pod-b", "deploy-b", "deployment", "sidecar", "0.05", "0.06", "0.03", "0.001", "67108864", "67108864", "50000000", "48000000", "0")

	reader := strings.NewReader(csv)
	err := ProcessCSVToDigests(ctx, pool, reader, "org-multi", "11111111-1111-1111-1111-111111111111")
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1`,
		"org-multi").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestProcessCSVToDigests_AutoCreatesPartition(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Use a date 3 months in the past — the migration only creates partitions
	// for the current + next 2 months, so this partition won't exist yet.
	pastDate := "2025-01-15 00:00:00 +0000 UTC"
	pastDateEnd := "2025-01-15 00:15:00 +0000 UTC"

	csv := csvHeader + "\n" +
		csvRow(pastDate, pastDateEnd, "test-ns", "pod-part", "test-deploy", "deployment", "main",
			"0.1", "0.15", "0.08", "0.001", "134217728", "134217728", "104857600", "100000000", "0")

	reader := strings.NewReader(csv)
	err := ProcessCSVToDigests(ctx, pool, reader, "org-partition-test", "22222222-2222-2222-2222-222222222222")
	require.NoError(t, err, "ProcessCSVToDigests should auto-create missing partition")

	var count int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1`,
		"org-partition-test").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestProcessCSVToDigests_WritesUsageSamples(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	csv := csvHeader + "\n" +
		csvRow("2026-04-01 00:00:00 +0000 UTC", "2026-04-01 00:15:00 +0000 UTC", "test-ns", "pod-sp1", "test-deploy", "deployment", "main", "0.1", "0.15", "0.08", "0.001", "134217728", "134217728", "104857600", "100000000", "0") + "\n" +
		csvRow("2026-04-01 00:15:00 +0000 UTC", "2026-04-01 00:30:00 +0000 UTC", "test-ns", "pod-sp1", "test-deploy", "deployment", "main", "0.1", "0.15", "0.09", "0.001", "134217728", "134217728", "110000000", "105000000", "0")

	reader := strings.NewReader(csv)
	err := ProcessCSVToDigests(ctx, pool, reader, "org-samples", "11111111-1111-1111-1111-111111111111")
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM container_usage_samples WHERE org_id = $1`,
		"org-samples").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "should have one sample per CSV row")
}

func TestProcessCSVToDigests_SamplesIdempotent(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	csv := csvHeader + "\n" +
		csvRow("2026-04-01 00:00:00 +0000 UTC", "2026-04-01 00:15:00 +0000 UTC", "test-ns", "pod-id", "test-deploy", "deployment", "main", "0.1", "0.15", "0.08", "0.001", "134217728", "134217728", "104857600", "100000000", "0")

	// Ingest same data twice
	reader := strings.NewReader(csv)
	require.NoError(t, ProcessCSVToDigests(ctx, pool, reader, "org-idem", "11111111-1111-1111-1111-111111111111"))
	reader = strings.NewReader(csv)
	require.NoError(t, ProcessCSVToDigests(ctx, pool, reader, "org-idem", "11111111-1111-1111-1111-111111111111"))

	var count int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM container_usage_samples WHERE org_id = $1`,
		"org-idem").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "upsert should not duplicate rows")
}

const csvHeader = "interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count"

func csvRow(start, end, ns, pod, wl, wlType, cn, cpuReq, cpuLimit, cpuUsage, cpuThrottle, memReq, memLimit, memUsage, memRSS, oom string) string {
	return start + "," + end + "," + ns + "," + pod + "," + wl + "," + wlType + "," + cn + "," +
		cpuReq + "," + cpuLimit + "," + cpuUsage + "," + cpuThrottle + "," +
		memReq + "," + memLimit + "," + memUsage + "," + memRSS + "," + oom
}
