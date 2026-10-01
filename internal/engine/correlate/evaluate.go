package correlate

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

// hcCandidate is one hosted cluster with fresh bucket evidence plus the
// management cluster serving it (from snapshots).
type hcCandidate struct {
	orgID       string
	clusterUUID string // management cluster for C evaluation + advisory scoping
	hcID        string
}

// enumerateCandidates lists (org, mgmt, hc) triples with bucket evidence in
// the trailing 26h (24h window + margin). No evidence means no evaluation:
// precision-first never reasons about absent planes.
func enumerateCandidates(ctx context.Context, pool *pgxpool.Pool) ([]hcCandidate, error) {
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT b.org_id, s.cluster_uuid, b.hc_cluster_id
		FROM hosted_api_bucket_rollups b
		JOIN manifest_hcp_snapshots s
		  ON s.org_id = b.org_id AND s.hosted_cluster_id = b.hc_cluster_id
		WHERE b.window_end >= $1 AND s.observed_at >= $1 AND s.complete = TRUE`,
		time.Now().UTC().Add(-26*time.Hour))
	if err != nil {
		return nil, fmt.Errorf("enumerate hcp candidates: %w", err)
	}
	defer rows.Close()
	var out []hcCandidate
	for rows.Next() {
		var c hcCandidate
		if err := rows.Scan(&c.orgID, &c.clusterUUID, &c.hcID); err != nil {
			return nil, fmt.Errorf("scan hcp candidate: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// bucketSample is one stored cumulative bucket observation.
type bucketSample struct {
	verbGroup string
	le        float64
	count     float64
	collected time.Time
}

// loadBucketSamples reads cumulative bucket snapshots for hc+groups in [from, to).
func loadBucketSamples(ctx context.Context, pool *pgxpool.Pool, orgID, hcID string, from, to time.Time) ([]bucketSample, error) {
	rows, err := pool.Query(ctx, `
		SELECT verb_group, le, bucket_count, collected_at
		FROM hosted_api_bucket_rollups
		WHERE org_id = $1 AND hc_cluster_id = $2
		  AND window_start >= $3 AND window_end <= $4
		ORDER BY verb_group, le, collected_at ASC`,
		orgID, hcID, from, to)
	if err != nil {
		return nil, fmt.Errorf("load bucket samples: %w", err)
	}
	defer rows.Close()
	var out []bucketSample
	for rows.Next() {
		var s bucketSample
		var count int64
		if err := rows.Scan(&s.verbGroup, &s.le, &count, &s.collected); err != nil {
			return nil, fmt.Errorf("scan bucket sample: %w", err)
		}
		s.count = float64(count)
		out = append(out, s)
	}
	return out, rows.Err()
}

// windowP99 computes the p99 per verb group over one hour window from
// cumulative snapshots. Storage upserts collapse each window to its latest
// snapshot per (verb, le) — at most one cumulative value per boundary per
// window exists — so the delta baseline is the latest snapshot strictly
// before window start (bounded to 2h back for hourly cycles). minSpan gates
// the time between the two snapshots (the 30m min-data rule); 0 disables the
// span gate but never the two-snapshot requirement. Unknown on any gap.
func windowP99(all []bucketSample, windowStart, windowEnd time.Time, minSpan time.Duration) (map[string]float64, bool) {
	type key struct {
		verb string
		le   float64
	}
	latest := make(map[key]bucketSample)
	latestBefore := make(map[key]bucketSample)
	for _, s := range all {
		k := key{s.verbGroup, s.le}
		inWindow := !s.collected.Before(windowStart) && !s.collected.After(windowEnd)
		if inWindow {
			if prev, ok := latest[k]; !ok || s.collected.After(prev.collected) {
				latest[k] = s
			}
			continue
		}
		if s.collected.Before(windowStart) && !s.collected.Before(windowStart.Add(-2*time.Hour)) {
			if prev, ok := latestBefore[k]; !ok || s.collected.After(prev.collected) {
				latestBefore[k] = s
			}
		}
	}
	byGroup := make(map[string][]Bucket)
	prevByGroup := make(map[string][]Bucket)
	var newest, oldest time.Time
	first := true
	for k, s := range latest {
		p, ok := latestBefore[k]
		if !ok {
			continue
		}
		byGroup[s.verbGroup] = append(byGroup[s.verbGroup], Bucket{LE: s.le, Count: s.count})
		prevByGroup[s.verbGroup] = append(prevByGroup[s.verbGroup], Bucket{LE: p.le, Count: p.count})
		if first || s.collected.After(newest) {
			newest = s.collected
		}
		if first || p.collected.Before(oldest) {
			oldest = p.collected
		}
		first = false
	}
	if len(byGroup) == 0 || newest.Sub(oldest) < minSpan {
		return nil, false
	}
	out := make(map[string]float64, len(byGroup))
	for g, cur := range byGroup {
		p99, ok := Quantile(Deltas(prevByGroup[g], cur), 0.99)
		if !ok {
			return nil, false
		}
		out[g] = p99
	}
	return out, len(out) > 0
}

// evalH reports hosted pain: max group p99 vs max(absolute, multiple of the
// 7-day baseline median). Baseline = median of daily p99s (day-aggregated
// deltas), computed with the same quantile path — no second math.
func evalH(ctx context.Context, pool *pgxpool.Pool, orgID, hcID string, windowEnd time.Time, p Policy) (p99, threshold float64, high, known bool) {
	windowStart := windowEnd.Add(-time.Duration(p.HWindowHours) * time.Hour)
	minSpan := time.Duration(p.HMinDataMinutes) * time.Minute
	// One span covers the fire window plus the 7-day baseline (previous-day
	// snapshots double as delta baselines, so a single query suffices).
	all, err := loadBucketSamples(ctx, pool, orgID, hcID, windowStart.AddDate(0, 0, -8), windowEnd)
	if err != nil || len(all) == 0 {
		return 0, 0, false, false
	}
	groups, ok := windowP99(all, windowStart, windowEnd, minSpan)
	if !ok {
		return 0, 0, false, false
	}
	p99 = 0
	for _, v := range groups {
		if v > p99 {
			p99 = v
		}
	}
	// Baseline: one p99 per trailing day from day-aggregated deltas.
	var daily []float64
	for d := 1; d <= 7; d++ {
		dayEnd := windowStart.AddDate(0, 0, -(d - 1))
		dayStart := dayEnd.AddDate(0, 0, -1)
		dayGroups, ok := windowP99(all, dayStart, dayEnd, 0)
		if !ok {
			continue
		}
		dayMax := 0.0
		for _, v := range dayGroups {
			if v > dayMax {
				dayMax = v
			}
		}
		daily = append(daily, dayMax)
	}
	median, ok := MedianFloat64(daily)
	if !ok {
		median = 0
	}
	threshold = p.HAbsoluteThresholdS
	if rel := p.HBaselineMultiple * median; rel > threshold {
		threshold = rel
	}
	return p99, threshold, p99 > threshold, true
}

// evalC reports management control-plane stress for the HC's namespaces:
// trailing-day usage/requests ratio vs threshold. Namespaces come from the
// strict association map (attribution-grade evidence only — guardrail union
// would mix sibling HCs' CPUs into this HC's verdict).
func evalC(ctx context.Context, pool *pgxpool.Pool, orgID, mgmtCluster, hcID string, p Policy) (ratio float64, high, known bool) {
	hcMap, err := engine.LoadHCAssociationForRun(ctx, pool, orgID, mgmtCluster)
	if err != nil {
		return 0, false, false
	}
	var namespaces []string
	for ns, hc := range hcMap {
		if hc == hcID {
			namespaces = append(namespaces, ns)
		}
	}
	if len(namespaces) == 0 {
		return 0, false, false
	}
	var usage, requests *int64
	err = pool.QueryRow(ctx, `
		SELECT SUM(cpu_usage_p95_mc), SUM(cpu_request_p50_mc)
		FROM daily_container_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = ANY($3)
		  AND bucket_date >= CURRENT_DATE - INTERVAL '1 day'`,
		orgID, mgmtCluster, namespaces).Scan(&usage, &requests)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, false, false
		}
		return 0, false, false
	}
	if usage == nil || requests == nil || *requests <= 0 {
		return 0, false, false
	}
	ratio = float64(*usage) / float64(*requests)
	return ratio, ratio*100 > p.CPUThresholdPct, true
}

// evalN reports hosted worker pressure from fresh node recommendations
// carrying codes 12 (overcommitted) or 74 (scheduling-limited). Stale or
// absent recs read as unknown — never as calm. Totals feed the coverage
// fraction of the persisted pressure row.
func evalN(ctx context.Context, pool *pgxpool.Pool, orgID, hcID string, p Policy) (pressured, known bool, signals []string, totalNodes, pressuredNodes int) {
	cutoff := time.Now().UTC().Add(-time.Duration(p.NodeFreshnessHours) * time.Hour)
	rows, err := pool.Query(ctx, `
		SELECT node, notification_codes FROM node_recommendations
		WHERE org_id = $1 AND cluster_uuid = $2 AND updated_at >= $3`,
		orgID, hcID, cutoff)
	if err != nil {
		return false, false, nil, 0, 0
	}
	defer rows.Close()
	nodes := map[string]bool{}
	hotNodes := map[string]bool{}
	hotCodes := map[string]bool{}
	for rows.Next() {
		var node string
		var codes []int16
		if err := rows.Scan(&node, &codes); err != nil {
			return false, false, nil, 0, 0
		}
		nodes[node] = true
		for _, c := range codes {
			if c == 12 || c == 74 {
				hotNodes[node] = true
				hotCodes[strconv.Itoa(int(c))] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return false, false, nil, 0, 0
	}
	if len(nodes) == 0 {
		return false, false, nil, 0, 0
	}
	var out []string
	for s := range hotCodes {
		out = append(out, s)
	}
	slices.Sort(out)
	return len(hotNodes) > 0, true, out, len(nodes), len(hotNodes)
}
