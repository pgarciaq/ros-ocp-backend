package advisory

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

// Ports of the server's apitax_test.go cases (internal/engine/correlate)
// — the parity contract: same inputs, same verdicts. The fixture shape
// mirrors seedAPITaxWindow: two consecutive hourly cumulative snapshots
// per webhook, totals denormalized on every bucket row.

var taxLEs = []float64{0.1, 0.5, 1.0, 2.0, 3.0, 10.0}

// taxWindow builds two cumulative snapshots around windowStart: the
// baseline in the previous hour (collected 10m before the window), the
// hot one inside (50m in). peakLE concentrates the delta mass for a
// deterministic p99; totals grow 5% between the snapshots.
func taxWindow(windowStart time.Time, peakLE float64, total, rejected int64) []csv.APITaxRow {
	var rows []csv.APITaxRow
	for i, ws := range []time.Time{windowStart.Add(-time.Hour), windowStart} {
		scale := 1.0 + 0.05*float64(i)
		collected := ws.Add(50 * time.Minute)
		cum := int64(0)
		for _, le := range taxLEs {
			d := int64(1)
			if le == peakLE {
				d = int64(float64(1000) * scale)
			}
			cum += d
			row := csv.APITaxRow{
				WebhookName: "slow.mutating.example",
				WindowStart: ws, WindowEnd: ws.Add(time.Hour),
				Le: le, BucketCount: cum,
				CollectedAt: collected, Source: "kubernetes",
			}
			// Totals ride the last LE row only (the backend joins
			// totals by webhook; other rows carry empty).
			if le == taxLEs[len(taxLEs)-1] {
				row.TotalCount = int64(float64(total) * scale)
				row.HasTotal = true
				row.RejectedCount = int64(float64(rejected) * scale)
				row.HasRejected = true
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func taxFireWindow(now time.Time) (time.Time, time.Time) {
	end := now.UTC().Truncate(time.Hour)
	return end.Add(-time.Hour), end
}

func TestAPITax_FiresOnHotP99(t *testing.T) {
	now := time.Now()
	ws, we := taxFireWindow(now)
	got := EvaluateAPITax(taxWindow(ws, 3.0, 5000, 10), ws, we, 30*time.Minute)
	require.NotNil(t, got, "p99 ~3s webhook must fire")
	assert.Equal(t, APITaxVerdict, got.Verdict)
	assert.Equal(t, "high", got.Confidence)
	assert.Equal(t, "", got.HostedClusterID, "shared-plane evidence names no single HC")
	p99 := got.Details["webhook_p99_seconds"].(float64)
	assert.GreaterOrEqual(t, p99, 2.0)
	assert.Equal(t, 2.0, got.Details["webhook_p99_threshold_seconds"])
	assert.Equal(t, "slow.mutating.example", got.Details["webhook"])
	require.NotEmpty(t, got.Evidence)
	assert.Equal(t, "webhook:slow.mutating.example", got.Evidence[0])
}

func TestAPITax_FiresOnRejectedRate(t *testing.T) {
	now := time.Now()
	ws, we := taxFireWindow(now)
	// Quiet p99 (0.1) but 6% rejected — fires on rate alone.
	rows := taxWindow(ws, 0.1, 1000, 60)
	got := EvaluateAPITax(rows, ws, we, 30*time.Minute)
	require.NotNil(t, got, "6% rejected rate must fire despite calm p99")
	rate := got.Details["webhook_rejected_rate"].(float64)
	assert.GreaterOrEqual(t, rate, 0.05)
}

func TestAPITax_SilentWhenQuiet(t *testing.T) {
	now := time.Now()
	ws, we := taxFireWindow(now)
	got := EvaluateAPITax(taxWindow(ws, 0.1, 1000, 0), ws, we, 30*time.Minute)
	assert.Nil(t, got, "calm webhook must stay silent")
}

func TestAPITax_SilentWithoutEvidence(t *testing.T) {
	now := time.Now()
	ws, we := taxFireWindow(now)
	got := EvaluateAPITax(nil, ws, we, 30*time.Minute)
	assert.Nil(t, got, "no evidence means silence, never error")
}

func TestAPITax_HottestWebhookWins(t *testing.T) {
	now := time.Now()
	ws, we := taxFireWindow(now)
	rows := taxWindow(ws, 3.0, 5000, 10)
	// A second, calmer webhook in the same window: the hottest p99 wins.
	calmer := taxWindow(ws, 0.5, 5000, 10)
	for i := range calmer {
		calmer[i].WebhookName = "calm.mutating.example"
	}
	got := EvaluateAPITax(append(rows, calmer...), ws, we, 30*time.Minute)
	require.NotNil(t, got)
	assert.Equal(t, "slow.mutating.example", got.Details["webhook"], "the hottest p99 is the fired webhook")
}

func TestAPITax_PerSourceCoherence(t *testing.T) {
	now := time.Now()
	ws, we := taxFireWindow(now)
	// The same webhook split across two Prometheus jobs: the baseline
	// snapshot rides one job, the in-window snapshot the other — neither
	// source alone has the two-snapshot delta, so no p99 forms (the
	// per-job coherence lesson).
	rows := taxWindow(ws, 3.0, 5000, 10)
	for i := range rows {
		if rows[i].CollectedAt.Before(ws) {
			rows[i].Source = "kubernetes"
		} else {
			rows[i].Source = "metrics"
		}
	}
	got := EvaluateAPITax(rows, ws, we, 30*time.Minute)
	assert.Nil(t, got, "mixed-source snapshots never form one coherent histogram")
}

func TestAPITax_CounterResetResolvesPostReset(t *testing.T) {
	now := time.Now()
	ws, we := taxFireWindow(now)
	rows := taxWindow(ws, 0.1, 1000, 0)
	// Simulate a totals reset mid-window: the second observation's totals
	// drop below the first's — the delta resolves to the post-reset value.
	for i := range rows {
		if rows[i].HasTotal && rows[i].CollectedAt.After(ws) {
			rows[i].TotalCount = 40
			rows[i].RejectedCount = 10
		}
	}
	got := EvaluateAPITax(rows, ws, we, 30*time.Minute)
	require.NotNil(t, got, "post-reset rejected 10/40 = 25% fires on the reset value")
	rate := got.Details["webhook_rejected_rate"].(float64)
	assert.GreaterOrEqual(t, rate, 0.05)
}

func TestAPITax_AtThresholdRejectedRateFires(t *testing.T) {
	now := time.Now()
	ws, we := taxFireWindow(now)
	// Exactly 5%: dTot = 2100-2000 = 100, dRej = 105-100 = 5. The gate
	// is >= (the server's semantics): at-threshold fires.
	rows := taxWindow(ws, 0.1, 2000, 100)
	got := EvaluateAPITax(rows, ws, we, 30*time.Minute)
	require.NotNil(t, got, "exactly-at-gate rejected rate fires (>= semantics)")
	rate := got.Details["webhook_rejected_rate"].(float64)
	assert.InDelta(t, 0.05, rate, 1e-9)
}
