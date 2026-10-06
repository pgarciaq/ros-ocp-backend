package correlate

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// Thin W5 webhook rule (#393).
//
// Per (org, uploading-cluster) with API tax evidence in the window: compute
// per-webhook p99 from bucket deltas (same windowP99/Quantile path as H) and
// rejected rate from totals deltas. Fire tune_noisy_webhook for the hottest
// webhook when p99 is grossly high or rejections are material.
//
// Thresholds are consts with rationale (precision-first: advisories must be
// rare and credible; tune after live observation, not before):
//   - p99 >= 2.0s: admission webhook p99s live in ms; 2s is unambiguous pain.
//   - rejected_rate >= 5%: material rejection share, not background noise.
//
// Shared-plane webhooks deliberately carry an EMPTY hc_cluster_id: a
// management-plane webhook serves N hosted clusters and naming one would be
// misattribution. Webhook identity + evidence ride n_signals; readers join
// cluster to HCs themselves. No schema change, no UI surface.
const (
	apiTaxVerdict          = "tune_noisy_webhook"
	apiTaxP99ThresholdS    = 2.0
	apiTaxRejectedRateGate = 0.05
)

// evaluateAPITaxClusters lists (org, cluster) with webhook evidence in the
// trailing window. No evidence means no evaluation.
func evaluateAPITaxClusters(ctx context.Context, pool *pgxpool.Pool, windowStart time.Time) ([]struct {
	orgID   string
	cluster string
}, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT org_id, cluster_uuid::text
		FROM hosted_api_tax_rollups
		WHERE window_end >= $1`,
		windowStart.Add(-time.Hour))
	if err != nil {
		return nil, fmt.Errorf("enumerate api tax clusters: %w", err)
	}
	defer rows.Close()
	var out []struct {
		orgID   string
		cluster string
	}
	for rows.Next() {
		var c struct {
			orgID   string
			cluster string
		}
		if err := rows.Scan(&c.orgID, &c.cluster); err != nil {
			return nil, fmt.Errorf("scan api tax cluster: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// loadAPITaxSamples reads cumulative webhook bucket snapshots for one
// cluster in [from, to), shaped for windowP99 (verbGroup carries webhook;
// callers split per source for per-job coherence). It also returns
// per-webhook first/last (total, rejected) pairs for reset-aware delta
// rates: resets (last < first) resolve to the post-reset value.
func loadAPITaxSamples(ctx context.Context, pool *pgxpool.Pool, orgID, cluster, source string, from, to time.Time) ([]bucketSample, map[string][2][2]float64, error) {
	rows, err := pool.Query(ctx, `
		SELECT webhook_name, le, bucket_count, total_count, rejected_count, collected_at
		FROM hosted_api_tax_rollups
		WHERE org_id = $1 AND cluster_uuid = $2 AND source = $3
		  AND window_start >= $4 AND window_end <= $5
		ORDER BY webhook_name, le, collected_at ASC`,
		orgID, cluster, source, from, to)
	if err != nil {
		return nil, nil, fmt.Errorf("load api tax samples: %w", err)
	}
	defer rows.Close()
	var samples []bucketSample
	totals := map[string][2][2]float64{}
	for rows.Next() {
		var webhook string
		var le float64
		var bucket int64
		var total, rejected *int64
		var collected time.Time
		if err := rows.Scan(&webhook, &le, &bucket, &total, &rejected, &collected); err != nil {
			return nil, nil, fmt.Errorf("scan api tax sample: %w", err)
		}
		samples = append(samples, bucketSample{verbGroup: webhook, le: le, count: float64(bucket), collected: collected})
		if total != nil && rejected != nil {
			// First and last observations bound the window delta; a
			// counter reset (last < first) resolves to the post-reset
			// value rather than a negative delta.
			pair := totals[webhook]
			if pair[0] == [2]float64{} {
				pair[0] = [2]float64{float64(*total), float64(*rejected)}
			}
			pair[1] = [2]float64{float64(*total), float64(*rejected)}
			totals[webhook] = pair
		}
	}
	return samples, totals, rows.Err()
}

// apiTaxSources lists distinct Prometheus jobs with webhook evidence for one
// cluster in range (per-job coherence, SLO amendment lesson).
func apiTaxSources(ctx context.Context, pool *pgxpool.Pool, orgID, cluster string, from, to time.Time) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT source FROM hosted_api_tax_rollups
		WHERE org_id = $1 AND cluster_uuid = $2
		  AND window_start >= $3 AND window_end <= $4`,
		orgID, cluster, from, to)
	if err != nil {
		return nil, fmt.Errorf("load api tax sources: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("scan api tax source: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// writeAPITaxAdvisory upserts one fired webhook advisory. hc is always empty
// (shared-plane evidence names no single HC); verdict distinguishes the row.
func writeAPITaxAdvisory(ctx context.Context, pool *pgxpool.Pool, orgID, cluster, webhook string, p99, rejectedRate float64, windowStart, windowEnd time.Time, p Policy) error {
	signals := []string{
		"webhook:" + webhook,
		fmt.Sprintf("p99:%.3f", p99),
		fmt.Sprintf("rejected_rate:%.4f", rejectedRate),
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO hcp_correlation_advisories (
			org_id, hc_cluster_id, management_cluster_uuid,
			window_start, window_end, verdict, confidence,
			h_p99_s, h_threshold_s, c_ratio, n_signals,
			expires_at
		) VALUES ($1,'',$2,$3,$4,'tune_noisy_webhook','high',$5,$6,$7,$8,$9)
		ON CONFLICT (org_id, hc_cluster_id, window_start)
		DO UPDATE SET verdict = EXCLUDED.verdict, confidence = EXCLUDED.confidence,
			h_p99_s = EXCLUDED.h_p99_s, h_threshold_s = EXCLUDED.h_threshold_s,
			c_ratio = EXCLUDED.c_ratio, n_signals = EXCLUDED.n_signals,
			expires_at = EXCLUDED.expires_at`,
		orgID, cluster,
		windowStart.UTC(), windowEnd.UTC(),
		p99, apiTaxP99ThresholdS, rejectedRate, signals,
		windowEnd.UTC().Add(time.Duration(p.AdvisoryExpiryHours)*time.Hour))
	if err != nil {
		return fmt.Errorf("write api tax advisory: %w", err)
	}
	metrics.HCPAdvisoriesTotal.WithLabelValues(apiTaxVerdict).Inc()
	return nil
}

// evaluateAPITaxForCluster fires at most one advisory per cluster: the
// hottest webhook by p99-or-rejected gate. Unknown anywhere is silence.
func evaluateAPITaxForCluster(ctx context.Context, pool *pgxpool.Pool, orgID, cluster string, windowStart, windowEnd time.Time, p Policy) (fired bool, err error) {
	minSpan := time.Duration(p.HMinDataMinutes) * time.Minute
	sources, err := apiTaxSources(ctx, pool, orgID, cluster, windowStart.Add(-2*time.Hour), windowEnd)
	if err != nil || len(sources) == 0 {
		return false, err
	}
	type hot struct {
		webhook      string
		p99          float64
		rejectedRate float64
	}
	var hottest *hot
	for _, source := range sources {
		samples, totals, err := loadAPITaxSamples(ctx, pool, orgID, cluster, source, windowStart.Add(-2*time.Hour), windowEnd)
		if err != nil || len(samples) == 0 {
			continue
		}
		// Per-webhook p99 via the shared tested path.
		byWebhook := map[string][]bucketSample{}
		for _, s := range samples {
			byWebhook[s.verbGroup] = append(byWebhook[s.verbGroup], s)
		}
		for webhook, sub := range byWebhook {
			groups, ok := windowP99(sub, windowStart, windowEnd, minSpan)
			if !ok {
				continue
			}
			p99, ok := groups[webhook]
			if !ok {
				continue
			}
			rejectedRate := 0.0
			if t, ok := totals[webhook]; ok {
				first, last := t[0], t[1]
				dTot, dRej := last[0]-first[0], last[1]-first[1]
				if dTot < 0 || dRej < 0 {
					// Counter reset mid-window: fall back to post-reset
					// totals rather than a negative delta.
					dTot, dRej = last[0], last[1]
				}
				if dTot > 0 {
					rejectedRate = dRej / dTot
				}
			}
			if p99 >= apiTaxP99ThresholdS || rejectedRate >= apiTaxRejectedRateGate {
				if hottest == nil || p99 > hottest.p99 {
					hottest = &hot{webhook, p99, rejectedRate}
				}
			}
		}
	}
	if hottest == nil {
		return false, nil
	}
	if err := writeAPITaxAdvisory(ctx, pool, orgID, cluster, hottest.webhook, hottest.p99, hottest.rejectedRate, windowStart, windowEnd, p); err != nil {
		return false, err
	}
	return true, nil
}

// RunAPITaxCycle evaluates webhook evidence for every cluster with hourly
// data and writes tune_noisy_webhook advisories. Called from RunCycle after
// the HC pass; shares its sweep and metrics. Never fails on evidence
// problems — those are silence.
func RunAPITaxCycle(ctx context.Context, pool *pgxpool.Pool) (fired int, err error) {
	windowEnd := time.Now().UTC().Truncate(time.Hour)
	clusters, err := evaluateAPITaxClusters(ctx, pool, windowEnd.Add(-time.Hour))
	if err != nil {
		if pgrec.IsUndefinedTable(err) {
			// Pre-migration database (code ahead of the apitax table):
			// degrade to an empty run, never error-loop hourly.
			logging.GetLogger().Warnf("correlator: api tax table unavailable (pre-migration, skipping): %v", err)
			return 0, nil
		}
		return 0, err
	}
	for _, c := range clusters {
		if err := ctx.Err(); err != nil {
			return fired, err
		}
		// Policy resolves per-org exactly like the HC pass.
		p := policyForOrg(ctx, pool, c.orgID)
		windowStart := windowEnd.Add(-time.Duration(p.HWindowHours) * time.Hour)
		ok, err := evaluateAPITaxForCluster(ctx, pool, c.orgID, c.cluster, windowStart, windowEnd, p)
		if err != nil {
			logging.ForOrg(c.orgID, c.cluster).Warnf("correlator: api tax evaluation failed, silent: %v", err)
			continue
		}
		if ok {
			fired++
		}
	}
	return fired, nil
}
