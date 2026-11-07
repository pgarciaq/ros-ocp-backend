package correlate

import (
	"context"
	"strings"
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
	// #779: the -1h snapshot can fall in the previous month (window straddling
	// a month boundary) — ensure BOTH inserted windows' partitions, not just
	// windowStart's month.
	require.NoError(t, ingestion.EnsureAPITaxPartitionsForMonth(ctx, pool, windowStart))
	require.NoError(t, ingestion.EnsureAPITaxPartitionsForMonth(ctx, pool, windowStart.Add(-time.Hour)))
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

// apitaxFarmAdvisories returns each shared-plane (hc=”) advisory's
// management cluster UUID and signals — #763 pins per-farm attribution.
func apitaxFarmAdvisories(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string) []map[string]interface{} {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT management_cluster_uuid::text, n_signals
		FROM hcp_correlation_advisories WHERE org_id = $1 AND hc_cluster_id = ''`,
		orgID)
	require.NoError(t, err)
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var mgmt string
		var signals []string
		require.NoError(t, rows.Scan(&mgmt, &signals))
		out = append(out, map[string]interface{}{
			"mgmt": mgmt, "signals": signals,
		})
	}
	require.NoError(t, rows.Err())
	return out
}

// signalsJoined renders a row's signals for Contains/NotContains assertions.
func signalsJoined(row map[string]interface{}) string {
	parts := row["signals"].([]string)
	return strings.Join(parts, " ")
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

// TestSeedAPITaxWindow_MonthBoundary (#779): a window starting 00:30 on the
// 1st has its -1h snapshot in the previous month — the seeder must ensure
// that month's partition too. Pre-fix behavior ruled out: the insert fails
// with SQLSTATE 23514 (no partition) whenever the window straddles a month
// boundary, which is every first-of-month 00:00-01:00 UTC CI run.
func TestSeedAPITaxWindow_MonthBoundary(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID + "-apitax-boundary"
	cluster := "22222222-2222-2222-2222-222222222222"

	n := time.Now().UTC()
	windowStart := time.Date(n.Year(), n.Month(), 1, 0, 30, 0, 0, time.UTC)
	seedAPITaxWindow(t, ctx, pool, orgID, cluster, "boundary.mutating.example", windowStart, 1.0, 100, 0)

	var rows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM hosted_api_tax_rollups WHERE org_id = $1 AND window_start IN ($2, $3)`,
		orgID, windowStart.Add(-time.Hour), windowStart).Scan(&rows))
	assert.Equal(t, len(apitaxLEs)*2, rows,
		"both hourly windows (previous-month 23:30 and current-month 00:30) must be seeded")
}

// The #763 (V15-13) farm-identity fixtures: two management clusters in one
// org, each with its own hot webhook — distinct UUIDs, distinct webhook
// names, distinct evidence values so a mis-attributed row cannot pass by
// coincidence.
const (
	farmA = "11111111-1111-4111-8111-111111111111"
	farmB = "22222222-2222-4222-8222-222222222222"
)

// TestAPITax_TwoFarmsDoNotMix is #763's end-to-end repro: two management
// clusters (farms) with hot webhooks in one org/window must produce TWO
// shared-plane advisories — each farm's UUID carrying its own webhook
// evidence. Baseline: the (org_id, hc_cluster_id, window_start) conflict
// key collapsed both writes into one row and the DO UPDATE replaced the
// evidence but not the identity — the first farm's UUID served with the
// second farm's webhook.
func TestAPITax_TwoFarmsDoNotMix(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID + "-apitax-763"
	start := apitaxFireWindow()

	seedAPITaxWindow(t, ctx, pool, orgID, farmA, "slow.a.example", start, 3.0, 5000, 10)
	seedAPITaxWindow(t, ctx, pool, orgID, farmB, "slow.b.example", start, 3.0, 6000, 20)

	fired, err := RunAPITaxCycle(ctx, pool)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, fired, 2, "both farms' hot webhooks must fire")

	rows := apitaxFarmAdvisories(t, ctx, pool, orgID)
	require.Len(t, rows, 2,
		"two farms in one org/window must produce two attributed rows — the 3-column key collapsed them into one mixed row (#763)")
	byFarm := map[string]map[string]interface{}{}
	for _, r := range rows {
		byFarm[r["mgmt"].(string)] = r
	}
	require.Contains(t, byFarm, farmA, "farm A's row must exist with its own identity")
	require.Contains(t, byFarm, farmB, "farm B's row must exist with its own identity")
	assert.Contains(t, signalsJoined(byFarm[farmA]), "slow.a.example",
		"farm A's advisory must carry farm A's webhook evidence")
	assert.NotContains(t, signalsJoined(byFarm[farmA]), "slow.b.example",
		"farm A's advisory must not carry farm B's evidence (#763 mixing)")
	assert.Contains(t, signalsJoined(byFarm[farmB]), "slow.b.example",
		"farm B's advisory must carry farm B's webhook evidence")
	assert.NotContains(t, signalsJoined(byFarm[farmB]), "slow.a.example",
		"farm B's advisory must not carry farm A's evidence (#763 mixing)")
}

// TestAPITax_FarmUpsertSemantics pins the writer's upsert contract under the
// extended key: farm order never matters (the baseline's UPDATE-evidence-
// but-not-identity asymmetry made it matter), and the same farm rewritten
// stays one row with evidence refreshed in place.
func TestAPITax_FarmUpsertSemantics(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	p := DefaultPolicy()
	ws := apitaxFireWindow()
	we := ws.Add(time.Hour)

	// Both write orders: two attributed rows either way.
	for i, order := range [][]string{{farmA, farmB}, {farmB, farmA}} {
		orgID := testutil.TestOrgID + "-apitax-763-order"
		if i > 0 {
			orgID += "-rev"
		}
		for _, farm := range order {
			webhook := "slow.a.example"
			if farm == farmB {
				webhook = "slow.b.example"
			}
			require.NoError(t, writeAPITaxAdvisory(ctx, pool, orgID, farm, webhook, 3.0, 0.05, ws, we, p))
		}
		rows := apitaxFarmAdvisories(t, ctx, pool, orgID)
		require.Len(t, rows, 2,
			"write order must not change farm attribution — the baseline kept the first writer's identity with the second writer's evidence (#763)")
		for _, r := range rows {
			mgmt := r["mgmt"].(string)
			want := "slow.a.example"
			notWant := "slow.b.example"
			if mgmt == farmB {
				want, notWant = "slow.b.example", "slow.a.example"
			}
			assert.Contains(t, signalsJoined(r), want,
				"farm %s must carry its own evidence (order %v)", mgmt, order)
			assert.NotContains(t, signalsJoined(r), notWant,
				"farm %s must not carry the other farm's evidence (order %v)", mgmt, order)
		}
	}

	// The same farm rewritten: one row, evidence refreshed in place.
	orgID := testutil.TestOrgID + "-apitax-763-repeat"
	require.NoError(t, writeAPITaxAdvisory(ctx, pool, orgID, farmA, "first.example", 3.0, 0.05, ws, we, p))
	require.NoError(t, writeAPITaxAdvisory(ctx, pool, orgID, farmA, "second.example", 3.2, 0.06, ws, we, p))
	rows := apitaxFarmAdvisories(t, ctx, pool, orgID)
	require.Len(t, rows, 1, "the same farm rewritten must stay one row — the upsert contract")
	assert.Equal(t, farmA, rows[0]["mgmt"])
	assert.Contains(t, signalsJoined(rows[0]), "second.example",
		"the repeated write must refresh evidence in place")
	assert.NotContains(t, signalsJoined(rows[0]), "first.example",
		"stale evidence must not survive the refresh")
}
