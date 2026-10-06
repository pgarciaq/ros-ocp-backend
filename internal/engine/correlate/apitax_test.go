package correlate

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

var apitaxLEs = []float64{0.1, 0.5, 1.0, 2.0, 3.0, 10.0}

// seedAPITaxWindow writes two consecutive hourly cumulative snapshots for one
// webhook so deltas form (cumulative counters must grow like production).
// peakLE concentrates ~99% mass for a deterministic p99; totals grow 5%.
func seedAPITaxWindow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, cluster, webhook string, windowStart time.Time, peakLE float64, total, rejected int64) {
	t.Helper()
	require.NoError(t, ingestion.EnsureAPITaxPartitionsForMonth(ctx, pool, windowStart))
	for i, ws := range []time.Time{windowStart.Add(-time.Hour), windowStart} {
		scale := 1.0 + 0.05*float64(i)
		collected := ws.Add(50 * time.Minute)
		cum := int64(0)
		for _, le := range apitaxLEs {
			d := int64(1)
			if le == peakLE {
				d = int64(float64(1000) * scale)
			}
			cum += d
			_, err := pool.Exec(ctx, `
				INSERT INTO hosted_api_tax_rollups (
					window_start, window_end, org_id, cluster_uuid, webhook_name,
					le, bucket_count, total_count, rejected_count, collected_at, source
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'kubernetes')
				ON CONFLICT (org_id, cluster_uuid, webhook_name, window_start, window_end, le, source)
				DO UPDATE SET bucket_count = EXCLUDED.bucket_count,
					total_count = EXCLUDED.total_count,
					rejected_count = EXCLUDED.rejected_count,
					collected_at = EXCLUDED.collected_at`,
				ws.UTC(), ws.Add(time.Hour).UTC(), orgID, cluster, webhook,
				le, cum, nullIntScaled(total, le, scale), nullIntScaled(rejected, le, scale), collected.UTC())
			require.NoError(t, err)
		}
	}
}

// nullInt scales seed totals onto the +Inf row only (backend joins totals
// by webhook); other rows carry NULL.
func nullIntScaled(v int64, le float64, scale float64) interface{} {
	if le == apitaxLEs[len(apitaxLEs)-1] {
		return int64(float64(v) * scale)
	}
	return nil
}

func apitaxFireWindow() (start time.Time) {
	end := time.Now().UTC().Truncate(time.Hour)
	return end.Add(-time.Hour)
}

func apitaxAdvisories(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string) []map[string]interface{} {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT verdict, confidence, h_p99_s, n_signals, expires_at > now() AS live
		FROM hcp_correlation_advisories WHERE org_id = $1 AND hc_cluster_id = ''`,
		orgID)
	require.NoError(t, err)
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var verdict, confidence string
		var p99 float64
		var signals []string
		var live bool
		require.NoError(t, rows.Scan(&verdict, &confidence, &p99, &signals, &live))
		out = append(out, map[string]interface{}{
			"verdict": verdict, "confidence": confidence, "p99": p99,
			"signals": signals, "live": live,
		})
	}
	require.NoError(t, rows.Err())
	return out
}

func TestAPITax_FiresOnHotP99(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID + "-apitax-fire"
	cluster := "11111111-1111-1111-1111-111111111111"
	start := apitaxFireWindow()

	seedAPITaxWindow(t, ctx, pool, orgID, cluster, "slow.mutating.example", start, 3.0, 5000, 10)

	fired, err := RunAPITaxCycle(ctx, pool)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, fired, 1, "p99 ~3s webhook must fire")

	advs := apitaxAdvisories(t, ctx, pool, orgID)
	require.Len(t, advs, 1)
	assert.Equal(t, "tune_noisy_webhook", advs[0]["verdict"])
	assert.Equal(t, "high", advs[0]["confidence"])
	assert.GreaterOrEqual(t, advs[0]["p99"], 2.0)
	assert.Equal(t, true, advs[0]["live"])
	signals := advs[0]["signals"].([]string)
	assert.Contains(t, signals[0], "slow.mutating.example")
}

func TestAPITax_FiresOnRejectedRate(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID + "-apitax-reject"
	cluster := "22222222-2222-2222-2222-222222222222"
	start := apitaxFireWindow()

	// Quiet p99 (0.1) but 6% rejected — fires on rate alone.
	seedAPITaxWindow(t, ctx, pool, orgID, cluster, "flaky.validating.example", start, 0.1, 1000, 60)

	fired, err := RunAPITaxCycle(ctx, pool)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, fired, 1, "6% rejected rate must fire despite calm p99")
	advs := apitaxAdvisories(t, ctx, pool, orgID)
	require.Len(t, advs, 1)
	assert.Equal(t, "tune_noisy_webhook", advs[0]["verdict"])
}

func TestAPITax_SilentWhenQuiet(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID + "-apitax-quiet"
	cluster := "33333333-3333-3333-3333-333333333333"
	start := apitaxFireWindow()

	seedAPITaxWindow(t, ctx, pool, orgID, cluster, "calm.mutating.example", start, 0.1, 1000, 0)

	fired, err := RunAPITaxCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired)
	assert.Empty(t, apitaxAdvisories(t, ctx, pool, orgID), "calm webhook must stay silent")
}

func TestAPITax_SilentWithoutEvidence(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	fired, err := RunAPITaxCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired, "no evidence means silence, never error")
}
