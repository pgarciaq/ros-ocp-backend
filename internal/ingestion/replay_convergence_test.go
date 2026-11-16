package ingestion

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// TestContainerBatch_RebalanceReplayConverges is #803 finding 2's
// prove-benign disposition: the rebalance replay window's shape, executed
// against the real digest upsert path, converges to the sequential state.
//
// The window: during a rebalance the old consumer's in-flight write (from a
// LATER offset) can land before the new consumer replays an EARLIER offset —
// a stale replacement write lands after a fresh one, transiently regressing
// the row. The new consumer then catches up and re-processes the later
// offset. ADR-0154's amendment asserts this converges ("ingestion upserts
// are idempotent replacement writes; a replayed report converges to the same
// row") — this test turns that assertion into evidence.
func TestContainerBatch_RebalanceReplayConverges(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID

	// Offset 3 (the earlier, "stale" report): hour 08, cpu 0.1 cores.
	stale := func() *ContainerDigestBatch {
		batch, err := NewContainerDigestBatch(ctx, pool, orgID, cluster, "manifest-replay-803", ParseDigestOptions{})
		require.NoError(t, err)
		_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("08", "0.1")))
		require.NoError(t, err)
		return batch
	}
	// Offset 5 (the later, "fresh" report): hour 09, cpu 2.0 cores — a later
	// independent payload that replaces the same key.
	fresh := func() *ContainerDigestBatch {
		batch, err := NewContainerDigestBatch(ctx, pool, orgID, cluster, "manifest-replay-803", ParseDigestOptions{})
		require.NoError(t, err)
		_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("09", "2.0")))
		require.NoError(t, err)
		return batch
	}

	// The sequential (no-rebalance) reference: offset 3 then offset 5.
	require.NoError(t, stale().Flush(ctx))
	require.NoError(t, fresh().Flush(ctx))
	wantCount, wantMax := containerDigestRow(t, ctx, pool)
	require.Equal(t, 1, wantCount, "a later independent payload replaces the same key")
	require.Equal(t, int64(2000), wantMax, "the fresh (later) payload's value survives sequentially")

	// Reset the row to replay the interleaving against a clean slate.
	_, err := pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1 AND cluster_uuid = $2
		AND namespace = 'ns-1' AND workload = 'wl-1' AND container_name = 'ctr-1'`, orgID, cluster)
	require.NoError(t, err)

	// The rebalance interleaving: the old consumer's in-flight write for
	// offset 5 lands FIRST (fresh), then the new consumer replays offset 3
	// (stale lands AFTER fresh — the transient regression), then catches up
	// and re-processes offset 5.
	require.NoError(t, fresh().Flush(ctx)) // old consumer's in-flight (offset 5)
	require.NoError(t, stale().Flush(ctx)) // new consumer's replay (offset 3)

	midCount, midMax := containerDigestRow(t, ctx, pool)
	assert.Equal(t, int64(100), midMax,
		"the exposure, documented: the stale replay transiently regresses the row (the ADR-0154 tolerated window)")
	assert.Equal(t, 1, midCount)

	require.NoError(t, fresh().Flush(ctx)) // new consumer's catch-up (offset 5 again)

	gotCount, gotMax := containerDigestRow(t, ctx, pool)
	assert.Equal(t, wantCount, gotCount, "after the catch-up, the replay converges to the sequential sample count")
	assert.Equal(t, wantMax, gotMax, "after the catch-up, the replay converges to the sequential state (#803 finding 2: benign)")
}

// TestContainerBatch_RebalanceReplayReversedOrderConverges is the second
// interleaving, genuinely distinct from the first: the old consumer was
// processing the EARLIER offset when the rebalance hit, so its late
// in-flight write is the STALE one — it lands AFTER the new owner has
// already replayed both offsets (stale then fresh, in offset order). The
// sequence: the new owner's replay writes stale, then fresh; the old
// owner's late stale write lands after (regressing the row); the next
// cycle's fresh payload converges.
func TestContainerBatch_RebalanceReplayReversedOrderConverges(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "1234567"
	cluster := testutil.TestClusterUUID

	stale := func() *ContainerDigestBatch {
		batch, err := NewContainerDigestBatch(ctx, pool, orgID, cluster, "manifest-replay-803b", ParseDigestOptions{})
		require.NoError(t, err)
		_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("08", "0.1")))
		require.NoError(t, err)
		return batch
	}
	fresh := func() *ContainerDigestBatch {
		batch, err := NewContainerDigestBatch(ctx, pool, orgID, cluster, "manifest-replay-803b", ParseDigestOptions{})
		require.NoError(t, err)
		_, err = batch.AddFile(ctx, strings.NewReader(containerHourCSV("09", "2.0")))
		require.NoError(t, err)
		return batch
	}

	// Sequential reference.
	require.NoError(t, stale().Flush(ctx))
	require.NoError(t, fresh().Flush(ctx))
	wantCount, wantMax := containerDigestRow(t, ctx, pool)

	_, err := pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1 AND cluster_uuid = $2
		AND namespace = 'ns-1' AND workload = 'wl-1' AND container_name = 'ctr-1'`, orgID, cluster)
	require.NoError(t, err)

	// The new owner replays BOTH offsets in order (stale, then fresh)…
	require.NoError(t, stale().Flush(ctx))
	require.NoError(t, fresh().Flush(ctx))
	// …and the old owner's late in-flight write (the stale one) lands AFTER.
	require.NoError(t, stale().Flush(ctx))

	midCount, midMax := containerDigestRow(t, ctx, pool)
	assert.Equal(t, 1, midCount)
	assert.Equal(t, int64(100), midMax, "the stale late write transiently regresses the row (the tolerated window)")

	// The next cycle's fresh payload converges.
	require.NoError(t, fresh().Flush(ctx))

	gotCount, gotMax := containerDigestRow(t, ctx, pool)
	assert.Equal(t, wantCount, gotCount)
	assert.Equal(t, wantMax, gotMax, "the reversed interleaving also converges (#803 finding 2)")
}
