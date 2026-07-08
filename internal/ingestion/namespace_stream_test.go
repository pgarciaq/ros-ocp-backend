package ingestion

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func buildMultiNamespaceCSV(namespaceCount int) string {
	header := "interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg"
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	for i := 0; i < namespaceCount; i++ {
		ns := fmt.Sprintf("ns-%d", i)
		b.WriteString(strings.Join([]string{
			"2026-04-01 00:00:00 +0000 UTC",
			"2026-04-01 01:00:00 +0000 UTC",
			ns,
			"0.500",
			"0.250",
			"1073741824",
			"536870912",
		}, ","))
		b.WriteString("\n")
	}
	return b.String()
}

func TestNamespaceIncrementalDigestFlush_TriggersWhenBatchSizeExceeded(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	t.Setenv("ROS_INGEST_FLUSH_BATCH_SIZE", "2")
	config.ResetForTest()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-ns-flush-batch-" + t.Name()
	clusterUUID := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_namespace_digests WHERE org_id = $1`, orgID)
	})

	before := counterValue(t, metrics.IngestFlushTotal)
	csv := buildMultiNamespaceCSV(5)
	_, err := parseAndDigestNamespaceCSVStream(ctx, pool, strings.NewReader(csv), orgID, clusterUUID)
	require.NoError(t, err)

	after := counterValue(t, metrics.IngestFlushTotal)
	assert.Greater(t, after-before, float64(0), "expected at least one incremental digest flush")

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_namespace_digests WHERE org_id = $1`, orgID).Scan(&count))
	assert.Equal(t, 5, count, "all namespace-day digests should be persisted")
}

func TestNamespaceIncrementalDigestFlush_SmallPayloadFlushesAtEOFOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	t.Setenv("ROS_INGEST_FLUSH_BATCH_SIZE", "1000")
	config.ResetForTest()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-ns-flush-small-" + t.Name()
	clusterUUID := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_namespace_digests WHERE org_id = $1`, orgID)
	})

	before := counterValue(t, metrics.IngestFlushTotal)
	csv := buildMultiNamespaceCSV(2)
	_, err := parseAndDigestNamespaceCSVStream(ctx, pool, strings.NewReader(csv), orgID, clusterUUID)
	require.NoError(t, err)

	after := counterValue(t, metrics.IngestFlushTotal)
	assert.Equal(t, before, after, "small payloads below batch threshold should not incremental-flush during streaming")

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_namespace_digests WHERE org_id = $1`, orgID).Scan(&count))
	assert.Equal(t, 2, count)
}

func TestForEachNamespaceCSVRow_DoesNotMaterializeFullSlice(t *testing.T) {
	csv := buildMultiNamespaceCSV(3)
	seen := 0
	count, err := forEachNamespaceCSVRow(strings.NewReader(csv), func(row NamespaceMetricRow) error {
		seen++
		if row.Namespace == "" {
			t.Fatal("expected namespace on streamed row")
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 3, count)
	assert.Equal(t, 3, seen)
}
