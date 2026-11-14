package ingestion

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #758 (V15-08) regression suite. The baseline ingested each container file
// through its own stream, computing and REPLACING the digest per key at EOF
// (pgdigest ON CONFLICT DO UPDATE) — so a manifest whose files carry disjoint
// portions of one container/day kept only the last file's sample (audit:
// 08:00=2000m then 09:00=100m → sample_count=1, p95=100, max=100; reversed
// order keeps the 2000 but still count=1).
//
// RED evidence (baseline, witnessed 2026-11-04): the accumulation assertions
// below failed on the reviewed baseline through this exact per-file API with
// the audit's numbers — three tests, count=1 in every order. The approved
// fix (option B hybrid staging, owner-locked) scopes accumulation to the
// MANIFEST BATCH; the standalone per-file path deliberately KEEPS
// last-writer-wins replacement — it is the cross-payload replacement
// semantic ("a later independent fuller payload replaces rather than
// accumulates") at the file level, relied on by CLI/tools and the fallback
// path. The first three tests pin that contract; the ContainerDigestBatch
// tests pin the accumulation contract.

const containerTestDay = "2026-10-25"

// containerHourCSV builds one container CSV row for the fixture container at
// the given hour with the given cpu usage in cores.
func containerHourCSV(hour string, cpuUsageCores string) string {
	return csvHeader + "\n" +
		containerHourRow(hour, cpuUsageCores)
}

func containerHourRow(hour string, cpuUsageCores string) string {
	return "2026-10-25T" + hour + ":00:00Z,2026-10-25T" + hour + ":59:59Z,ns-1,pod-1,wl-1,Deployment,ctr-1,1.0,2.0," + cpuUsageCores + ",0.0,1048576.0,2097152.0,1048576.0,1048576.0,0\n"
}

// containerDigestRow reads the fixture container's all-hours digest row.
func containerDigestRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (count int, cpuMax int64) {
	t.Helper()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID
	err := pool.QueryRow(ctx, `
		SELECT sample_count, cpu_usage_max_mc FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = 'ns-1' AND workload = 'wl-1'
			AND container_name = 'ctr-1' AND bucket_date = $3 AND schedule_type = 'all_hours'`,
		orgID, cluster, containerTestDay).Scan(&count, &cpuMax)
	require.NoError(t, err, "the fixture container's digest row must exist")
	return count, cpuMax
}

func ingestContainerFile(t *testing.T, ctx context.Context, pool *pgxpool.Pool, csv, orgID string) {
	t.Helper()
	cluster := testutil.TestClusterUUID
	_, err := ParseAndDigestCSV(ctx, pool, strings.NewReader(csv), orgID, cluster)
	require.NoError(t, err)
}

// Standalone-path guard: per-file ingest REPLACES the same key — the last
// writer wins. Deliberate semantics (#758 scope decision): the standalone
// path serves CLI/tools and the plugin-off fallback; cross-payload
// replacement is its contract. (These assertions failed on the baseline in
// the accumulation direction — the RED that motivated the batch.)
func TestContainerPerFileIngest_ReplacesSameKey(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"

	ingestContainerFile(t, ctx, pool, containerHourCSV("08", "2.0"), orgID)
	ingestContainerFile(t, ctx, pool, containerHourCSV("09", "0.1"), orgID)

	count, cpuMax := containerDigestRow(t, ctx, pool)
	assert.Equal(t, 1, count, "standalone per-file ingest replaces the same key (#758 scope decision)")
	assert.Equal(t, int64(100), cpuMax, "the last writer's value wins in the standalone path")
}

// Standalone-path guard, reversed order: the survivor flips — still one
// sample, the replacement is order-dependent outside the batch.
func TestContainerPerFileIngest_ReplacementIsLastWriter(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"

	ingestContainerFile(t, ctx, pool, containerHourCSV("09", "0.1"), orgID)
	ingestContainerFile(t, ctx, pool, containerHourCSV("08", "2.0"), orgID)

	count, cpuMax := containerDigestRow(t, ctx, pool)
	assert.Equal(t, 1, count, "standalone per-file ingest replaces regardless of order")
	assert.Equal(t, int64(2000), cpuMax, "the last writer's value wins in the standalone path")
}

// Batch (the #758 fix): two files of one manifest carrying disjoint portions
// of the same container/day produce the COMPLETE daily distribution — the
// audit's proof, order-independent.
func TestContainerBatch_SameDaySamplesAccumulate(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID

	batch, err := NewContainerDigestBatch(ctx, pool, orgID, cluster, "manifest-batch-1", ParseDigestOptions{})
	require.NoError(t, err)
	_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("08", "2.0")))
	require.NoError(t, err)
	_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("09", "0.1")))
	require.NoError(t, err)
	require.NoError(t, batch.Flush(ctx))

	count, cpuMax := containerDigestRow(t, ctx, pool)
	assert.Equal(t, 2, count, "both same-day observations must survive the manifest batch (#758)")
	assert.Equal(t, int64(2000), cpuMax, "the high point must survive the manifest batch (#758)")
}

// Batch, reversed order: accumulation is order-independent.
func TestContainerBatch_ReversedOrderAccumulates(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID

	batch, err := NewContainerDigestBatch(ctx, pool, orgID, cluster, "manifest-batch-2", ParseDigestOptions{})
	require.NoError(t, err)
	_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("09", "0.1")))
	require.NoError(t, err)
	_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("08", "2.0")))
	require.NoError(t, err)
	require.NoError(t, batch.Flush(ctx))

	count, _ := containerDigestRow(t, ctx, pool)
	assert.Equal(t, 2, count, "reversed input order must not change the batch's sample count (#758)")
}

// Batch, same-valued control: identical values cannot hide a lost sample
// behind a surviving-value assertion — only the count catches it.
func TestContainerBatch_SameValuedSamplesCounted(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID

	batch, err := NewContainerDigestBatch(ctx, pool, orgID, cluster, "manifest-batch-3", ParseDigestOptions{})
	require.NoError(t, err)
	_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("08", "2.0")))
	require.NoError(t, err)
	_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("09", "2.0")))
	require.NoError(t, err)
	require.NoError(t, batch.Flush(ctx))

	count, _ := containerDigestRow(t, ctx, pool)
	assert.Equal(t, 2, count, "same-valued observations must both be counted (#758)")
}

// Batch equivalence property: batching files A+B must equal ingesting one
// file carrying A+B's rows — the complete-distribution criterion, pinned
// against a golden single-file reference (full percentiles, not a count
// bump or GREATEST of per-file percentiles).
func TestContainerBatch_EqualsSingleFileUnion(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	batchOrg, refOrg := "1234567", "7654321"
	cluster := testutil.TestClusterUUID

	batch, err := NewContainerDigestBatch(ctx, pool, batchOrg, cluster, "manifest-batch-eq", ParseDigestOptions{})
	require.NoError(t, err)
	_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("08", "2.0")))
	require.NoError(t, err)
	_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("09", "0.1")))
	require.NoError(t, err)
	_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("10", "1.5")))
	require.NoError(t, err)
	require.NoError(t, batch.Flush(ctx))

	singleFile := csvHeader + "\n" +
		containerHourRow("08", "2.0") + containerHourRow("09", "0.1") + containerHourRow("10", "1.5")
	ingestContainerFile(t, ctx, pool, singleFile, refOrg)

	var batchCount, refCount int
	var batchP95, refP95, batchMax, refMax, batchMean, refMean int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT sample_count, cpu_usage_p95_mc, cpu_usage_max_mc, cpu_usage_mean_mc
		FROM daily_container_digests WHERE org_id = $1 AND cluster_uuid = $2
			AND namespace = 'ns-1' AND container_name = 'ctr-1' AND bucket_date = $3 AND schedule_type = 'all_hours'`,
		batchOrg, cluster, containerTestDay).Scan(&batchCount, &batchP95, &batchMax, &batchMean))
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT sample_count, cpu_usage_p95_mc, cpu_usage_max_mc, cpu_usage_mean_mc
		FROM daily_container_digests WHERE org_id = $1 AND cluster_uuid = $2
			AND namespace = 'ns-1' AND container_name = 'ctr-1' AND bucket_date = $3 AND schedule_type = 'all_hours'`,
		refOrg, cluster, containerTestDay).Scan(&refCount, &refP95, &refMax, &refMean))
	assert.Equal(t, refCount, batchCount, "batch count equals the single-file union")
	assert.Equal(t, refP95, batchP95, "batch p95 equals the single-file union's complete percentile (#758)")
	assert.Equal(t, refMax, batchMax, "batch max equals the single-file union's")
	assert.Equal(t, refMean, batchMean, "batch mean equals the single-file union's")
}

// Batch replay idempotency: a second batch over the same files reproduces
// the same digest (replacement, not accumulation, across batches).
func TestContainerBatch_ReplayIsIdempotent(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID

	for i := 0; i < 2; i++ {
		batch, err := NewContainerDigestBatch(ctx, pool, orgID, cluster, "manifest-batch-replay", ParseDigestOptions{})
		require.NoError(t, err)
		_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("08", "2.0")))
		require.NoError(t, err)
		_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("09", "0.1")))
		require.NoError(t, err)
		require.NoError(t, batch.Flush(ctx))
	}

	count, cpuMax := containerDigestRow(t, ctx, pool)
	assert.Equal(t, 2, count, "replay reproduces the same distribution (no duplication)")
	assert.Equal(t, int64(2000), cpuMax, "replay reproduces the same maximum")
}

// Batch, later fuller payload replaces: a second batch with different data
// for the same key replaces — the cross-payload semantics preserved.
func TestContainerBatch_LaterPayloadReplaces(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID

	b1, err := NewContainerDigestBatch(ctx, pool, orgID, cluster, "manifest-batch-p1", ParseDigestOptions{})
	require.NoError(t, err)
	_, err = b1.AddFile(ctx, strings.NewReader(containerHourCSV("08", "2.0")))
	require.NoError(t, err)
	_, err = b1.AddFile(ctx, strings.NewReader(containerHourCSV("09", "0.1")))
	require.NoError(t, err)
	require.NoError(t, b1.Flush(ctx))

	// The later payload re-sends only one hour with different usage.
	b2, err := NewContainerDigestBatch(ctx, pool, orgID, cluster, "manifest-batch-p2", ParseDigestOptions{})
	require.NoError(t, err)
	_, err = b2.AddFile(ctx, strings.NewReader(containerHourCSV("09", "3.0")))
	require.NoError(t, err)
	require.NoError(t, b2.Flush(ctx))

	count, cpuMax := containerDigestRow(t, ctx, pool)
	assert.Equal(t, 1, count, "a later payload replaces rather than accumulates (#758 criterion)")
	assert.Equal(t, int64(3000), cpuMax, "the later payload's value wins")
}

// F2 (review finding): the config layer defaults an unset flush knob to
// math.MaxInt32 — that sentinel must not defeat the batch's staging bound.
func TestContainerBatch_StageThresholdDefaultsWhenKnobUnset(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	config.ResetForTest()
	_ = config.GetConfig()

	batch, err := NewContainerDigestBatch(ctx, pool, "1234567", testutil.TestClusterUUID, "m-threshold", ParseDigestOptions{})
	require.NoError(t, err)
	assert.Equal(t, defaultContainerStageKeyThreshold, batch.stageKeyThreshold(),
		"the MaxInt32 config default must not disable staging (#758 review finding)")

	t.Setenv("ROS_INGEST_FLUSH_BATCH_SIZE", "2")
	config.ResetForTest()
	_ = config.GetConfig()
	assert.Equal(t, 2, batch.stageKeyThreshold(), "an explicitly set knob keeps its contract")
}

// F5 (review finding): an UNLOGGED staging table truncates on a Postgres
// restart while the worker may survive — finalizing against silently
// missing staged rows must be a hard error, never a done-with-partial-data.
func TestContainerBatch_StagedCountMismatchFailsFlush(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"

	t.Setenv("ROS_INGEST_FLUSH_BATCH_SIZE", "2")
	config.ResetForTest()
	_ = config.GetConfig()
	batch, err := NewContainerDigestBatch(ctx, pool, orgID, testutil.TestClusterUUID, "m-mismatch", ParseDigestOptions{})
	require.NoError(t, err)

	twoContainerFile := csvHeader + "\n" +
		containerHourRow("08", "2.0") +
		"2026-10-25T08:00:00Z,2026-10-25T08:59:59Z,ns-1,pod-2,wl-1,Deployment,ctr-2,1.0,2.0,1.0,0.0,1048576.0,2097152.0,1048576.0,1048576.0,0\n"
	_, err = batch.AddFile(ctx, strings.NewReader(twoContainerFile))
	require.NoError(t, err)
	require.Equal(t, 1, batch.digestBatchesStaged, "the two-group threshold must have staged")

	// Simulate the Postgres restart: the staging table truncates behind the
	// batch's back while the worker survives.
	_, err = pool.Exec(ctx, `DELETE FROM container_metric_samples_staging WHERE manifest_id = $1`, "m-mismatch")
	require.NoError(t, err)

	flushErr := batch.Flush(ctx)
	require.Error(t, flushErr, "a staged-count mismatch must fail the flush (#758 review finding)")
	assert.Contains(t, flushErr.Error(), "staged sample count mismatch",
		"the failure must name the staging-loss mechanism")
}

// F6 (review finding): Postgres normalizes UUID columns to lowercase — an
// uppercase cluster UUID must not break the staged/memory key merge.
func TestContainerBatch_UpperCaseClusterUUIDMerges(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	upperCluster := "ABCDEF01-1111-2222-3333-444444444444"

	t.Setenv("ROS_INGEST_FLUSH_BATCH_SIZE", "2")
	config.ResetForTest()
	_ = config.GetConfig()
	batch, err := NewContainerDigestBatch(ctx, pool, orgID, upperCluster, "m-uuidcase", ParseDigestOptions{})
	require.NoError(t, err)

	twoContainerFile := csvHeader + "\n" +
		containerHourRow("08", "2.0") +
		"2026-10-25T08:00:00Z,2026-10-25T08:59:59Z,ns-1,pod-2,wl-1,Deployment,ctr-2,1.0,2.0,1.0,0.0,1048576.0,2097152.0,1048576.0,1048576.0,0\n"
	_, err = batch.AddFile(ctx, strings.NewReader(twoContainerFile))
	require.NoError(t, err)
	require.Equal(t, 1, batch.digestBatchesStaged)
	_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("09", "0.1")))
	require.NoError(t, err)
	require.NoError(t, batch.Flush(ctx))

	var count int
	var cpuMax int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT sample_count, cpu_usage_max_mc FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = 'ns-1' AND container_name = 'ctr-1'
			AND bucket_date = $3 AND schedule_type = 'all_hours'`,
		orgID, "abcdef01-1111-2222-3333-444444444444", containerTestDay).Scan(&count, &cpuMax))
	assert.Equal(t, 2, count, "an uppercase cluster UUID must not break the staged/memory merge (#758 review finding)")
	assert.Equal(t, int64(2000), cpuMax, "the staged high point must survive the merge")
}

// F1 (review finding): the finalize's chunk boundary — a key with multiple
// samples at the 1024th position must never split across two replacement
// upserts. 1024 single-sample keys, then a lexically-last key with two
// samples: the chunk fills at the boundary key's first row, and its second
// row must land in the SAME chunk.
func TestContainerBatch_ChunkBoundaryNeverSplitsKey(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"

	t.Setenv("ROS_INGEST_FLUSH_BATCH_SIZE", "2")
	config.ResetForTest()
	_ = config.GetConfig()
	batch, err := NewContainerDigestBatch(ctx, pool, orgID, testutil.TestClusterUUID, "m-boundary", ParseDigestOptions{})
	require.NoError(t, err)

	// 1023 single-sample keys, then the key at sorted position 1024
	// (ctr-1023) with TWO ADJACENT rows (both stage together when the next
	// key pushes the group count over the threshold), then more keys so
	// everything stages. In the finalize's sorted stream, ctr-1023's first
	// row fills the chunk to the threshold — the per-row flush bug would
	// split its second sample into a new replacement chunk.
	var b strings.Builder
	b.WriteString(csvHeader + "\n")
	for i := 0; i < stagedChunkKeys-1; i++ {
		fmt.Fprintf(&b, "2026-10-25T08:00:00Z,2026-10-25T08:59:59Z,ns-1,pod-%d,wl-1,Deployment,ctr-%04d,1.0,2.0,1.0,0.0,1048576.0,2097152.0,1048576.0,1048576.0,0\n", i, i)
	}
	fmt.Fprintf(&b, "2026-10-25T08:00:00Z,2026-10-25T08:59:59Z,ns-1,pod-x,wl-1,Deployment,ctr-1023,1.0,2.0,2.0,0.0,1048576.0,2097152.0,1048576.0,1048576.0,0\n")
	fmt.Fprintf(&b, "2026-10-25T09:00:00Z,2026-10-25T09:59:59Z,ns-1,pod-x,wl-1,Deployment,ctr-1023,1.0,2.0,3.0,0.0,1048576.0,2097152.0,1048576.0,1048576.0,0\n")
	for i := stagedChunkKeys; i < stagedChunkKeys+4; i++ {
		fmt.Fprintf(&b, "2026-10-25T08:00:00Z,2026-10-25T08:59:59Z,ns-1,pod-%d,wl-1,Deployment,ctr-%04d,1.0,2.0,1.0,0.0,1048576.0,2097152.0,1048576.0,1048576.0,0\n", i, i)
	}

	_, err = batch.AddFile(ctx, strings.NewReader(b.String()))
	require.NoError(t, err)
	require.NoError(t, batch.Flush(ctx))

	var count int
	var cpuMax int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT sample_count, cpu_usage_max_mc FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = 'ns-1' AND container_name = 'ctr-1023'
			AND bucket_date = $3 AND schedule_type = 'all_hours'`,
		orgID, testutil.TestClusterUUID, containerTestDay).Scan(&count, &cpuMax))
	assert.Equal(t, 2, count, "a key at the chunk boundary must not split across upserts (#758 review finding)")
	assert.Equal(t, int64(3000), cpuMax, "the boundary key's second sample must survive")
}

// Batch staging path: with the staging threshold at 2 groups, two
// containers stage mid-file; a later row for an ALREADY-STAGED key stays in
// memory, and the finalize must merge staged + in-memory samples for that
// key losslessly — the exact path a count-split manifest exercises.
func TestContainerBatch_StagingThresholdStillLossless(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"

	t.Setenv("ROS_INGEST_FLUSH_BATCH_SIZE", "2")
	config.ResetForTest()
	_ = config.GetConfig()
	batch, err := NewContainerDigestBatch(ctx, pool, orgID, testutil.TestClusterUUID, "manifest-batch-stage", ParseDigestOptions{})
	require.NoError(t, err)

	// File 1: two distinct containers — the second row pushes the group
	// count to the threshold, staging BOTH groups and clearing memory.
	twoContainerFile := csvHeader + "\n" +
		containerHourRow("08", "2.0") +
		"2026-10-25T08:00:00Z,2026-10-25T08:59:59Z,ns-1,pod-2,wl-1,Deployment,ctr-2,1.0,2.0,1.0,0.0,1048576.0,2097152.0,1048576.0,1048576.0,0\n"
	_, err = batch.AddFile(ctx, strings.NewReader(twoContainerFile))
	require.NoError(t, err)
	require.Equal(t, 1, batch.digestBatchesStaged, "the two-group threshold must have staged once")

	// File 2: another hour for ctr-1 — an ALREADY-STAGED key — landing in
	// memory (group count 1 < threshold 2): the finalize's merge path.
	_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("09", "0.1")))
	require.NoError(t, err)
	require.NoError(t, batch.Flush(ctx))

	count, cpuMax := containerDigestRow(t, ctx, pool)
	assert.Equal(t, 2, count, "staged + in-memory samples for the same key must merge losslessly (#758)")
	assert.Equal(t, int64(2000), cpuMax, "the staged high point must survive the merge (#758)")

	// The staging table must be clean after a successful finalize.
	var staged int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM container_metric_samples_staging WHERE manifest_id = $1`,
		"manifest-batch-stage").Scan(&staged))
	assert.Equal(t, 0, staged, "successful finalize deletes the manifest's staged rows")
}

// TestContainerBatch_BHStagedAccountingSucceeds is #858's RED: with a
// business-hours schedule active, rows inside the BH window stage
// TWICE (an all-hours sample and a business-hours sample), but the
// staging bookkeeping counted INPUT rows — the finalize integrity
// check read back more than was accounted and hard-failed, after
// digest chunks had already committed. Deterministic on every retry:
// the manifest could never complete. Baseline: Flush returns
// "staged sample count mismatch" for a perfectly intact staging table.
func TestContainerBatch_BHStagedAccountingSucceeds(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-858-bh-" + t.Name()
	clusterUUID := testutil.TestClusterUUID

	t.Setenv("ROS_INGEST_FLUSH_BATCH_SIZE", "2")
	config.ResetForTest()
	_ = config.GetConfig()

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	// A cluster-scope Mon–Fri 08:00–17:00 New York schedule. 2026-10-26
	// is a Monday; 14:00 UTC = 10:00 EDT — inside the window, so each
	// row stages twice (all_hours + business_hours).
	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: clusterUUID, Namespace: "",
		Timezone: "America/New_York", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "08:00", EndTime: "17:00", OffHoursWeight: 0.0, Enabled: true,
	}))

	batch, err := NewContainerDigestBatch(ctx, pool, orgID, clusterUUID, "m-858-bh", ParseDigestOptions{})
	require.NoError(t, err)

	twoContainerBHFile := csvHeader + "\n" +
		"2026-10-26T14:00:00Z,2026-10-26T14:59:59Z,ns-1,pod-1,wl-1,Deployment,ctr-1,1.0,2.0,1.0,0.0,1048576.0,2097152.0,1048576.0,1048576.0,0\n" +
		"2026-10-26T14:00:00Z,2026-10-26T14:59:59Z,ns-1,pod-2,wl-1,Deployment,ctr-2,1.0,2.0,1.0,0.0,1048576.0,2097152.0,1048576.0,1048576.0,0\n"
	_, err = batch.AddFile(ctx, strings.NewReader(twoContainerBHFile))
	require.NoError(t, err)
	require.Positive(t, batch.digestBatchesStaged, "the two-group threshold must have staged")

	flushErr := batch.Flush(ctx)
	require.NoError(t, flushErr,
		"an intact staging table must finalize — the BH double-stage was miscounted as staging loss (#858)")

	// Both streams' digests must exist for the BH-window container-day.
	for _, scheduleType := range []string{"all_hours", "business_hours"} {
		var count int
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT sample_count FROM daily_container_digests
			 WHERE org_id = $1 AND container_name = 'ctr-1' AND schedule_type = $2`,
			orgID, scheduleType).Scan(&count))
		assert.Equal(t, 1, count, "the %s digest must carry its staged sample", scheduleType)
	}
}

// TestContainerBatch_StagedLossFailsBeforeAnyDigestCommit is #858's
// partial-commit property: a staged-count mismatch must fail the flush
// BEFORE any digest row commits (the pre-count). Baseline: the check
// ran after the streaming finalize's chunk commits — a mismatch left
// partially-committed digests behind.
func TestContainerBatch_StagedLossFailsBeforeAnyDigestCommit(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-858-precount-" + t.Name()

	t.Setenv("ROS_INGEST_FLUSH_BATCH_SIZE", "2")
	config.ResetForTest()
	_ = config.GetConfig()

	batch, err := NewContainerDigestBatch(ctx, pool, orgID, testutil.TestClusterUUID, "m-858-pre", ParseDigestOptions{})
	require.NoError(t, err)

	twoContainerFile := csvHeader + "\n" +
		containerHourRow("08", "2.0") +
		"2026-10-25T08:00:00Z,2026-10-25T08:59:59Z,ns-1,pod-2,wl-1,Deployment,ctr-2,1.0,2.0,1.0,0.0,1048576.0,2097152.0,1048576.0,1048576.0,0\n"
	_, err = batch.AddFile(ctx, strings.NewReader(twoContainerFile))
	require.NoError(t, err)
	require.Equal(t, 1, batch.digestBatchesStaged, "the two-group threshold must have staged")

	// Simulate partial staging loss (a Postgres restart truncates SOME
	// rows — one of the two staged samples vanishes).
	_, err = pool.Exec(ctx, `
		DELETE FROM container_metric_samples_staging WHERE ctid IN (
			SELECT ctid FROM container_metric_samples_staging WHERE manifest_id = $1 LIMIT 1
		)`, "m-858-pre")
	require.NoError(t, err)

	flushErr := batch.Flush(ctx)
	require.Error(t, flushErr, "a staged-count mismatch must fail the flush")
	assert.Contains(t, flushErr.Error(), "staged sample count mismatch",
		"the failure must name the staging-loss mechanism")

	var digestRows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM daily_container_digests WHERE org_id = $1`, orgID).Scan(&digestRows))
	assert.Equal(t, 0, digestRows,
		"the mismatch must fail BEFORE any digest row commits — no partial digests behind a failed flush (#858)")
}

// TestContainerBatch_StagedAccountingAcrossChunkFlushes is #858's
// review-P1 regression: the staging accounting must accumulate across
// the 1,000-row chunk flushes — the first fix counted only the final
// partial chunk (`queued` resets per chunk), so a staging pass of
// 1,001 rows accounted 1 and the finalize integrity check failed an
// intact table. The shape: ONE container-day carrying N minute-level
// samples (group 1) plus one row of a second container (crosses the
// knob=2 group threshold) — a single staging pass of N+1 rows that
// crosses the chunk boundary at 1,000.
func TestContainerBatch_StagedAccountingAcrossChunkFlushes(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	for _, tc := range []struct {
		name string
		rows int
		bh   bool
	}{
		{"999 rows, no BH", 999, false},
		{"1000 rows, no BH", 1000, false},
		{"1001 rows, no BH", 1001, false},
		{"2400 rows, no BH", 2400, false},
		{"1001 rows with BH", 1001, true},
		{"2400 rows with BH", 2400, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := testutil.SetupTestDB(t)
			ctx := context.Background()
			orgID := "org-858-chunk-" + t.Name()
			clusterUUID := testutil.TestClusterUUID

			t.Setenv("ROS_INGEST_FLUSH_BATCH_SIZE", "2")
			config.ResetForTest()
			_ = config.GetConfig()

			if tc.bh {
				require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
					OrgID: orgID, ClusterUUID: clusterUUID, Namespace: "",
					Timezone: "America/New_York", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
					StartTime: "08:00", EndTime: "17:00", OffHoursWeight: 0.0, Enabled: true,
				}))
				t.Cleanup(func() {
					_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
				})
			}
			t.Cleanup(func() {
				_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
			})

			batch, err := NewContainerDigestBatch(ctx, pool, orgID, clusterUUID, "m-858-chunk", ParseDigestOptions{})
			require.NoError(t, err)

			// Group 1: one container-day with tc.rows minute-level samples
			// (2026-10-26 is a Monday; 14:00–15:00 UTC = 10:00–11:00 EDT,
			// inside the BH window when enabled). Then one row of a second
			// container-day crosses the knob=2 group threshold mid-file —
			// the staging pass carries tc.rows + 1 rows.
			var b strings.Builder
			b.WriteString(csvHeader)
			b.WriteString("\n")
			day := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
			for i := 0; i < tc.rows; i++ {
				ts := day.Add(time.Duration(i) * time.Minute)
				fmt.Fprintf(&b, "%s,%s,ns-1,pod-1,wl-1,Deployment,ctr-1,1.0,2.0,1.0,0.0,1048576.0,2097152.0,1048576.0,1048576.0,0\n",
					ts.Format("2006-01-02T15:04:05Z"), ts.Add(time.Minute).Format("2006-01-02T15:04:59Z"))
			}
			fmt.Fprintf(&b, "2026-10-26T14:00:00Z,2026-10-26T14:59:59Z,ns-1,pod-2,wl-1,Deployment,ctr-2,1.0,2.0,1.0,0.0,1048576.0,2097152.0,1048576.0,1048576.0,0\n")
			_, err = batch.AddFile(ctx, strings.NewReader(b.String()))
			require.NoError(t, err)
			require.Positive(t, batch.digestBatchesStaged, "the two-group threshold must have staged")

			require.NoError(t, batch.Flush(ctx),
				"the accounting must hold across chunk flushes for a %d-row staging pass (BH=%v)", tc.rows+1, tc.bh)
		})
	}
}
