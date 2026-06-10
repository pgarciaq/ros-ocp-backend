package ingestion

import (
	"context"
	"fmt"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/client_golang/prometheus"
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
			"flush-ns", fmt.Sprintf("pod-%d", i), "deploy-a", "deployment", cn,
			"0.1", "0.15", "0.08", "0.001", "134217728", "134217728", "104857600", "100000000", "0",
		))
		b.WriteString("\n")
	}
	return b.String()
}

func TestIncrementalDigestFlush_TriggersWhenBatchSizeExceeded(t *testing.T) {
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
		_, _ = pool.Exec(ctx, `DELETE FROM container_usage_samples WHERE org_id = $1`, orgID)
	})

	before := counterValue(t, metrics.IngestFlushTotal)
	csv := buildMultiContainerCSV(5)
	_, err := parseAndDigestCSVStream(ctx, pool, strings.NewReader(csv), orgID, clusterUUID, ParseDigestOptions{})
	require.NoError(t, err)

	after := counterValue(t, metrics.IngestFlushTotal)
	assert.Greater(t, after-before, float64(0), "expected at least one incremental digest flush")

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
		_, _ = pool.Exec(ctx, `DELETE FROM container_usage_samples WHERE org_id = $1`, orgID)
	})

	before := counterValue(t, metrics.IngestFlushTotal)
	csv := buildMultiContainerCSV(2)
	_, err := parseAndDigestCSVStream(ctx, pool, strings.NewReader(csv), orgID, clusterUUID, ParseDigestOptions{})
	require.NoError(t, err)

	after := counterValue(t, metrics.IngestFlushTotal)
	assert.Equal(t, before, after, "small payloads below batch threshold should not incremental-flush during streaming")

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
	defer tx.Rollback(ctx)

	require.NoError(t, db.SetLocalIngestStatementTimeout(ctx, tx))

	assert.Equal(t, int64(120000), db.QueryStatementTimeoutMillis(ctx, tx))
}
