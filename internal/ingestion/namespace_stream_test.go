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

func TestNamespaceStreamFlushKnobIgnored_SingleEOFFlushDespiteGroupCount(t *testing.T) {
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
	err := parseAndDigestNamespaceCSVStream(ctx, pool, strings.NewReader(csv), orgID, clusterUUID)
	require.NoError(t, err)

	after := counterValue(t, metrics.IngestFlushTotal)
	assert.Equal(t, float64(1), after-before,
		"exactly one batch write at EOF — the knob no longer triggers mid-stream flushes (#822)")

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_namespace_digests WHERE org_id = $1`, orgID).Scan(&count))
	assert.Equal(t, 5, count, "all namespace-day digests should be persisted")
}

func TestNamespaceStreamFlushKnobIgnored_SmallPayloadSingleEOFFlush(t *testing.T) {
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
	err := parseAndDigestNamespaceCSVStream(ctx, pool, strings.NewReader(csv), orgID, clusterUUID)
	require.NoError(t, err)

	after := counterValue(t, metrics.IngestFlushTotal)
	assert.Equal(t, float64(1), after-before,
		"exactly one batch write at EOF — same as any payload size (#822)")

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_namespace_digests WHERE org_id = $1`, orgID).Scan(&count))
	assert.Equal(t, 2, count)
}

func TestForEachNamespaceCSVRow_DoesNotMaterializeFullSlice(t *testing.T) {
	csv := buildMultiNamespaceCSV(3)
	seen := 0
	count, err := forEachNamespaceCSVRow(context.Background(), strings.NewReader(csv), func(row NamespaceMetricRow) error {
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

// TestNamespaceStreamFlushKnobIgnored_NamespaceDayNotFragmented is
// #822's namespace twin: with the knob set finite (2), a namespace-day
// spanning the would-be flush boundary must NOT be fragmented — the
// digest must carry all its samples. Owner decision 2026-11-14
// (option a): the stream paths do not honor ROS_INGEST_FLUSH_BATCH_SIZE.
func TestNamespaceStreamFlushKnobIgnored_NamespaceDayNotFragmented(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	t.Setenv("ROS_INGEST_FLUSH_BATCH_SIZE", "2")
	config.ResetForTest()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-822-ns-frag-" + t.Name()
	clusterUUID := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_namespace_digests WHERE org_id = $1`, orgID)
	})

	var b strings.Builder
	b.WriteString("interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg\n")
	row := func(hour, ns, usage string) {
		b.WriteString(strings.Join([]string{
			fmt.Sprintf("2026-04-01 %s:00:00 +0000 UTC", hour),
			fmt.Sprintf("2026-04-01 %s:59:00 +0000 UTC", hour),
			ns, "0.500", usage, "1073741824", "536870912",
		}, ","))
		b.WriteString("\n")
	}
	// ns-frag's first fragment: 3 low-usage hours.
	for h := 0; h < 3; h++ {
		row(fmt.Sprintf("%02d", h), "ns-frag", "0.100")
	}
	// A second group — crosses the threshold (2), firing the mid-stream
	// flush on the current code.
	row("10", "ns-other", "0.100")
	// ns-frag's second fragment: 3 high-usage hours.
	for h := 3; h < 6; h++ {
		row(fmt.Sprintf("%02d", h), "ns-frag", "0.900")
	}

	err := parseAndDigestNamespaceCSVStream(ctx, pool, strings.NewReader(b.String()), orgID, clusterUUID)
	require.NoError(t, err)

	var sampleCount int
	var p50 int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT sample_count, cpu_usage_p50_mc FROM daily_namespace_digests
		 WHERE org_id = $1 AND namespace = 'ns-frag' AND schedule_type = 'all_hours'`,
		orgID).Scan(&sampleCount, &p50))
	assert.Equal(t, 6, sampleCount,
		"the namespace-day's digest must carry ALL its samples — a fragment replacement loses the first half (#822)")
	// digest.PercentileFromSorted: idx = int(0.5 × 5) = 2 → sorted[2]
	// = 100mc; the last-fragment digest (3×900mc) would read 900mc.
	assert.Equal(t, int64(100), p50,
		"p50 over 3×100mc + 3×900mc is sorted[2]=100mc — not the last fragment's 900mc (#822)")
}
