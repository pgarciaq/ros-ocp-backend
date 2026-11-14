package ingestion

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, c.Write(&m))
	return m.GetCounter().GetValue()
}

func buildMultiContainerCSV(containerCount int) string {
	var b strings.Builder
	b.WriteString(csvHeader)
	b.WriteString("\n")
	for i := 0; i < containerCount; i++ {
		cn := fmt.Sprintf("container-%d", i)
		b.WriteString(csvRow(
			"2026-04-01 00:00:00 +0000 UTC", "2026-04-01 00:15:00 +0000 UTC",
			"flush-ns", fmt.Sprintf("pod-%d", i), "deploy-a", cn,
			"0.1", "0.15", "0.08", "134217728", "134217728", "104857600", "100000000",
		))
		b.WriteString("\n")
	}
	return b.String()
}

// #822 (owner decision 2026-11-14, option a): the stream paths ignore
// ROS_INGEST_FLUSH_BATCH_SIZE — one batch write per CSV at EOF,
// regardless of the group count. The ingest-flush metric counts that
// write (exactly one), never a mid-stream fragmenting flush.
func TestStreamFlushKnobIgnored_SingleEOFFlushDespiteGroupCount(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	t.Setenv("ROS_INGEST_FLUSH_BATCH_SIZE", "2")
	config.ResetForTest()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-flush-batch-" + t.Name()
	clusterUUID := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
	})

	before := counterValue(t, metrics.IngestFlushTotal)
	csv := buildMultiContainerCSV(5)
	err := parseAndDigestCSVStream(ctx, pool, strings.NewReader(csv), orgID, clusterUUID, ParseDigestOptions{})
	require.NoError(t, err)

	after := counterValue(t, metrics.IngestFlushTotal)
	assert.Equal(t, float64(1), after-before,
		"exactly one batch write (the EOF flush) — the knob no longer triggers mid-stream flushes (#822)")

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1`, orgID).Scan(&count))
	assert.Equal(t, 5, count, "all container-day digests should be persisted")
}

func TestIncrementalDigestFlush_SmallPayloadFlushesAtEOFOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	t.Setenv("ROS_INGEST_FLUSH_BATCH_SIZE", "1000")
	config.ResetForTest()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-flush-small-" + t.Name()
	clusterUUID := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
	})

	before := counterValue(t, metrics.IngestFlushTotal)
	csv := buildMultiContainerCSV(2)
	err := parseAndDigestCSVStream(ctx, pool, strings.NewReader(csv), orgID, clusterUUID, ParseDigestOptions{})
	require.NoError(t, err)

	after := counterValue(t, metrics.IngestFlushTotal)
	assert.Equal(t, float64(1), after-before,
		"exactly one batch write at EOF — same as any payload size (#822)")

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1`, orgID).Scan(&count))
	assert.Equal(t, 2, count)
}

func TestIngestTransactionUsesExtendedStatementTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	t.Setenv("ROS_DB_INGEST_STATEMENT_TIMEOUT", "120")
	config.ResetForTest()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	require.NoError(t, db.SetLocalIngestStatementTimeout(ctx, tx))

	ms, err := db.QueryStatementTimeoutMillis(ctx, tx)
	require.NoError(t, err)
	assert.Equal(t, int64(120000), ms)
}

// TestStreamFlushKnobIgnored_ContainerDayNotFragmented is #822's RED:
// with the knob set finite (2), a container-day spanning the would-be
// flush boundary must NOT be fragmented — the digest must reflect ALL
// its samples. Baseline: the mid-stream flush fires when the row
// creating the 2nd group arrives, so container-a's low-usage fragment
// is written first and its high-usage fragment REPLACES it at EOF —
// the digest ends with the last 6 samples only (p50 900mc, count 6)
// instead of the full 12 (p50 100mc, the engine's nearest-rank
// convention).
//
// Owner decision 2026-11-14 (option a): the stream paths do not honor
// ROS_INGEST_FLUSH_BATCH_SIZE — it is staged-path-only (where the
// flush is safe). This test pins that: the knob is set, and the
// stream path still computes the digest from the complete sample set.
func TestStreamFlushKnobIgnored_ContainerDayNotFragmented(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	t.Setenv("ROS_INGEST_FLUSH_BATCH_SIZE", "2")
	config.ResetForTest()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-822-frag-" + t.Name()
	clusterUUID := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
	})

	var b strings.Builder
	b.WriteString(csvHeader)
	b.WriteString("\n")
	row := func(hour, cn, usage string) {
		b.WriteString(csvRow(
			"2026-04-01 "+hour+":00:00 +0000 UTC",
			"2026-04-01 "+hour+":15:00 +0000 UTC",
			"frag-ns", "pod-a", "deploy-a", cn,
			"0.2", "0.3", usage, "134217728", "134217728", "104857600", "100000000",
		))
		b.WriteString("\n")
	}
	// Container-a's first fragment: 6 low-usage samples.
	for h := 0; h < 6; h++ {
		row(fmt.Sprintf("%02d", h), "container-a", "0.1")
	}
	// A second group — the row that crosses the threshold (2) and
	// triggers the mid-stream flush on the current code.
	row("20", "container-b", "0.1")
	// Container-a's second fragment: 6 high-usage samples.
	for h := 6; h < 12; h++ {
		row(fmt.Sprintf("%02d", h), "container-a", "0.9")
	}

	err := parseAndDigestCSVStream(ctx, pool, strings.NewReader(b.String()), orgID, clusterUUID, ParseDigestOptions{})
	require.NoError(t, err)

	var sampleCount int
	var p50 int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT sample_count, cpu_usage_p50_mc FROM daily_container_digests
		 WHERE org_id = $1 AND container_name = 'container-a' AND schedule_type = 'all_hours'`,
		orgID).Scan(&sampleCount, &p50))
	assert.Equal(t, 12, sampleCount,
		"the container-day's digest must carry ALL its samples — a fragment replacement loses the first half (#822)")
	// The engine's percentile convention (digest.PercentileFromSorted):
	// idx = int(0.5 × (n−1)) = 5 → sorted[5] = 100mc. The last-fragment
	// digest (6×900mc) would read 900mc — the discriminator.
	assert.Equal(t, int64(100), p50,
		"p50 over 6×100mc + 6×900mc is sorted[5]=100mc — not the last fragment's 900mc (#822)")
}
