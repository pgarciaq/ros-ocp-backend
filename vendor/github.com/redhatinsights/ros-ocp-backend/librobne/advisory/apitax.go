// Thin W5 webhook rule (#724 robne parity; server rule
// internal/engine/correlate/apitax.go).
//
// Per uploading cluster with API tax evidence in the window: compute
// per-webhook p99 from bucket deltas (the same WindowP99/Quantile path
// as the H leg) and the rejected rate from totals deltas. Fire
// tune_noisy_webhook for the hottest webhook when the p99 is grossly
// high or rejections are material.
//
// Thresholds are consts with rationale (precision-first: advisories must
// be rare and credible; tune after live observation, not before):
//   - p99 >= 2.0s: admission webhook p99s live in ms; 2s is unambiguous pain.
//   - rejected_rate >= 5%: material rejection share, not background noise.
//
// Shared-plane webhooks deliberately carry an EMPTY hc_cluster_id: a
// management-plane webhook serves N hosted clusters and naming one would
// be misattribution. Webhook identity + evidence ride the signals;
// readers join cluster to HCs themselves.
//
// Offline shape (ADR-0350): the rows are the PRIMARY payload's api-tax
// CSVs — the run's cluster identity. Per-source coherence is preserved:
// a webhook's snapshots from different Prometheus jobs never mix into
// one histogram (the SLO amendment lesson).
package advisory

import (
	"fmt"
	"sort"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

// APITaxVerdict is the W5 noisy-webhook advisory verdict (#717's serving
// contract).
const APITaxVerdict = "tune_noisy_webhook"

// APITaxP99ThresholdS and APITaxRejectedRateGate are the precision-first
// consts (the server's consts, not knobs).
const (
	APITaxP99ThresholdS    = 2.0
	APITaxRejectedRateGate = 0.05
)

// EvaluateAPITax fires at most one advisory per cluster: the hottest
// webhook by p99-or-rejected gate. Unknown anywhere is silence. The
// window and minSpan come from the caller (the server shares HWindowHours
// and HMinDataMinutes with the headroom rule; offline the headroom_advisory
// config carries them).
func EvaluateAPITax(rows []csv.APITaxRow, windowStart, windowEnd time.Time, minSpan time.Duration) *Advisory {
	if len(rows) == 0 {
		return nil
	}
	// Per-source, per-webhook coherence: group first by source, then by
	// webhook — snapshots from different jobs never form one histogram.
	bySource := map[string]map[string][]csv.APITaxRow{}
	for _, r := range rows {
		if bySource[r.Source] == nil {
			bySource[r.Source] = map[string][]csv.APITaxRow{}
		}
		bySource[r.Source][r.WebhookName] = append(bySource[r.Source][r.WebhookName], r)
	}
	sources := make([]string, 0, len(bySource))
	for s := range bySource {
		sources = append(sources, s)
	}
	sort.Strings(sources)

	// Totals pairs (first/last per webhook, reset-aware) — the server
	// scans each source's rows in collected order.
	type hot struct {
		webhook      string
		p99          float64
		rejectedRate float64
	}
	var hottest *hot
	for _, source := range sources {
		byWebhook := bySource[source]
		webhooks := make([]string, 0, len(byWebhook))
		for w := range byWebhook {
			webhooks = append(webhooks, w)
		}
		sort.Strings(webhooks)
		for _, webhook := range webhooks {
			sub := byWebhook[webhook]
			samples := make([]Sample, 0, len(sub))
			var first, last [2]float64
			haveTotals := false
			for _, r := range sub {
				samples = append(samples, Sample{Group: r.WebhookName, LE: r.Le, Count: float64(r.BucketCount), Collected: r.CollectedAt})
				if r.HasTotal && r.HasRejected {
					pair := [2]float64{float64(r.TotalCount), float64(r.RejectedCount)}
					if !haveTotals {
						first = pair
						haveTotals = true
					}
					last = pair
				}
			}
			groups, ok := WindowP99(samples, windowStart, windowEnd, minSpan)
			if !ok {
				continue
			}
			p99, ok := groups[webhook]
			if !ok {
				continue
			}
			rejectedRate := 0.0
			if haveTotals {
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
			if p99 >= APITaxP99ThresholdS || rejectedRate >= APITaxRejectedRateGate {
				if hottest == nil || p99 > hottest.p99 {
					hottest = &hot{webhook, p99, rejectedRate}
				}
			}
		}
	}
	if hottest == nil {
		return nil
	}
	return &Advisory{
		HostedClusterID: "",
		Verdict:         APITaxVerdict,
		Confidence:      "high",
		WindowStart:     windowStart,
		WindowEnd:       windowEnd,
		Details: map[string]any{
			"webhook_p99_seconds":           hottest.p99,
			"webhook_p99_threshold_seconds": APITaxP99ThresholdS,
			"webhook_rejected_rate":         hottest.rejectedRate,
			"webhook":                       hottest.webhook,
		},
		Evidence: []string{
			"webhook:" + hottest.webhook,
			fmt.Sprintf("p99:%.3f", hottest.p99),
			fmt.Sprintf("rejected_rate:%.4f", hottest.rejectedRate),
		},
	}
}
