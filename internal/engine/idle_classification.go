package engine

import (
	"context"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// IdleState represents the workload activity classification.
type IdleState string

const (
	IdleStateActive IdleState = "active"
	IdleStateIdle   IdleState = "idle"
	IdleStateZombie IdleState = "zombie"
)

// IdleConfig holds thresholds for idle/zombie classification.
// Values come from the 3-tier config model (env > tenant settings > defaults).
type IdleConfig struct {
	Enabled              bool
	ZombieCPUP95MC       int64 // P95 CPU below this = zombie candidate (default 1)
	ZombieCPUPeakMC      int64 // Peak CPU below this confirms zombie (default 10)
	IdleCPUUtilPct       int64 // P95/request % threshold for idle (default 2)
	IdleMemUtilPct       int64 // P95/request % threshold for idle (default 5)
	BurstRatio           int64 // peak/P95 ratio classifying as bursty (default 10)
	MinObservationDays   int   // Days of data required (default 14)
	ExcludeNamespaces    []string
	ExcludeWorkloadTypes []string
}

// DefaultIdleConfig returns compiled defaults.
func DefaultIdleConfig() IdleConfig {
	return IdleConfig{
		Enabled:              true,
		ZombieCPUP95MC:       1,
		ZombieCPUPeakMC:      10,
		IdleCPUUtilPct:       2,
		IdleMemUtilPct:       5,
		BurstRatio:           10,
		MinObservationDays:   14,
		ExcludeNamespaces:    []string{"kube-system", "openshift-*"},
		ExcludeWorkloadTypes: []string{"DaemonSet"},
	}
}

// IdleResult holds classification output for a single container.
type IdleResult struct {
	State           IdleState
	IdleSince       *time.Time
	DurationDays    int
	PeakCPUMC       int64
	PeakMemoryBytes int64
	WasteCents      int64
}

// LoadIdleConfig resolves idle detection settings: compiled defaults, env overlay,
// then tenant overrides from recommendation_thresholds (additive exclusions).
func LoadIdleConfig(ctx context.Context, pool *pgxpool.Pool, orgID string) IdleConfig {
	settings, err := resolveIdleDetectionSettings(ctx, pool, orgID)
	if err != nil {
		return DefaultIdleConfig()
	}
	return idleConfigFromSettings(settings)
}

func splitCSVList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ClassifyIdleState determines whether a container is zombie, idle, or active
// based on its digest rows and current resource requests.
func ClassifyIdleState(
	rows []DigestRow,
	currentCPURequestMC int64,
	currentMemRequestKiB int64,
	workloadType string,
	namespace string,
	cfg IdleConfig,
) IdleResult {
	result := IdleResult{State: IdleStateActive}

	if !cfg.Enabled || len(rows) == 0 {
		return result
	}

	if isExcludedWorkloadType(workloadType, cfg.ExcludeWorkloadTypes) {
		return result
	}
	if isExcludedNamespace(namespace, cfg.ExcludeNamespaces) {
		return result
	}

	if len(rows) < cfg.MinObservationDays {
		return result
	}

	cpuP95MC := percentile95CPU(rows)
	memP95KiB := percentile95Mem(rows)
	peakCPUMC := maxCPU(rows)
	peakMemBytes := maxMemBytes(rows)

	result.PeakCPUMC = peakCPUMC
	result.PeakMemoryBytes = peakMemBytes

	if cpuP95MC > 0 && peakCPUMC > cfg.BurstRatio*cpuP95MC {
		return result
	}

	if cpuP95MC < cfg.ZombieCPUP95MC && peakCPUMC < cfg.ZombieCPUPeakMC {
		result.State = IdleStateZombie
		result.IdleSince = findIdleSince(rows, func(r DigestRow) bool {
			return r.CPUUsageMaxMC < cfg.ZombieCPUPeakMC
		})
		result.DurationDays = computeIdleDuration(result.IdleSince)
		return result
	}

	if currentCPURequestMC > 0 && currentMemRequestKiB > 0 {
		cpuUtilPct := (cpuP95MC * 100) / currentCPURequestMC
		memUtilPct := (memP95KiB * 100) / currentMemRequestKiB
		if cpuUtilPct < cfg.IdleCPUUtilPct && memUtilPct < cfg.IdleMemUtilPct {
			result.State = IdleStateIdle
			result.IdleSince = findIdleSince(rows, func(r DigestRow) bool {
				if currentCPURequestMC == 0 || currentMemRequestKiB == 0 {
					return false
				}
				return (r.CPUUsageP95MC*100)/currentCPURequestMC < cfg.IdleCPUUtilPct &&
					(r.MemUsageP95KiB*100)/currentMemRequestKiB < cfg.IdleMemUtilPct
			})
			result.DurationDays = computeIdleDuration(result.IdleSince)
			return result
		}
	}

	return result
}

func percentile95CPU(rows []DigestRow) int64 {
	vals := make([]int64, len(rows))
	for i, r := range rows {
		vals[i] = r.CPUUsageP95MC
	}
	return percentile95Int64(vals)
}

func percentile95Mem(rows []DigestRow) int64 {
	vals := make([]int64, len(rows))
	for i, r := range rows {
		vals[i] = r.MemUsageP95KiB
	}
	return percentile95Int64(vals)
}

func percentile95Int64(vals []int64) int64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := append([]int64(nil), vals...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := (len(sorted)*95 + 99) / 100
	if idx < 1 {
		idx = 1
	}
	if idx > len(sorted) {
		idx = len(sorted)
	}
	return sorted[idx-1]
}

func maxCPU(rows []DigestRow) int64 {
	var max int64
	for _, r := range rows {
		if r.CPUUsageMaxMC > max {
			max = r.CPUUsageMaxMC
		}
	}
	return max
}

func maxMemBytes(rows []DigestRow) int64 {
	var maxKiB int64
	for _, r := range rows {
		if r.MemUsageMaxKiB > maxKiB {
			maxKiB = r.MemUsageMaxKiB
		}
	}
	return maxKiB * 1024
}

func findIdleSince(rows []DigestRow, predicate func(DigestRow) bool) *time.Time {
	if len(rows) == 0 {
		return nil
	}
	start := len(rows) - 1
	for start >= 0 && predicate(rows[start]) {
		start--
	}
	firstIdle := start + 1
	if firstIdle >= len(rows) {
		return nil
	}
	t := rows[firstIdle].BucketDate
	return &t
}

func isExcludedWorkloadType(wt string, excludes []string) bool {
	for _, ex := range excludes {
		if wt == ex {
			return true
		}
	}
	return false
}

func isExcludedNamespace(ns string, patterns []string) bool {
	for _, pat := range patterns {
		if pat == "" {
			continue
		}
		matched, err := path.Match(pat, ns)
		if err == nil && matched {
			return true
		}
		if ns == pat {
			return true
		}
	}
	return false
}

func computeIdleDuration(since *time.Time) int {
	if since == nil {
		return 0
	}
	days := int(time.Since(*since).Truncate(24*time.Hour).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

func idleStateForWrite(s IdleState) string {
	if s == "" {
		return string(IdleStateActive)
	}
	return string(s)
}

// idleClassificationAuthoritative reports whether ClassifyIdleState applied full
// observation-window rules (not early-return active from disabled/excluded/insufficient data).
func idleClassificationAuthoritative(cfg IdleConfig, workloadType, namespace string, rows []DigestRow) bool {
	if !cfg.Enabled || len(rows) == 0 {
		return false
	}
	if isExcludedWorkloadType(workloadType, cfg.ExcludeWorkloadTypes) {
		return false
	}
	if isExcludedNamespace(namespace, cfg.ExcludeNamespaces) {
		return false
	}
	return len(rows) >= cfg.MinObservationDays
}

// AggregateNamespaceIdleState rolls container and GPU idle_state up to namespaces.
// Zombie only when every workload in the namespace is zombie; idle when all are
// non-active but at least one is idle (mix of idle and zombie counts as idle).
// Call after container recommendations are written (container plugin priority 10
// runs before namespace priority 90).
func AggregateNamespaceIdleState(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	_, err := pool.Exec(ctx, `
		UPDATE namespace_recommendation_sets ns
		SET idle_state = CASE
			WHEN NOT EXISTS (
				SELECT 1 FROM recommendation_sets rs
				WHERE rs.org_id = ns.org_id
				  AND rs.cluster_uuid = ns.cluster_uuid
				  AND rs.namespace = ns.namespace_name
			) THEN 'active'
			WHEN EXISTS (
				SELECT 1 FROM recommendation_sets rs
				WHERE rs.org_id = ns.org_id
				  AND rs.cluster_uuid = ns.cluster_uuid
				  AND rs.namespace = ns.namespace_name
				  AND rs.idle_state = 'active'
			) OR EXISTS (
				SELECT 1 FROM recommendation_sets rs
				WHERE rs.org_id = ns.org_id
				  AND rs.cluster_uuid = ns.cluster_uuid
				  AND rs.namespace = ns.namespace_name
				  AND rs.has_gpu = true
				  AND rs.gpu_idle_state = 'active'
			) THEN 'active'
			WHEN NOT EXISTS (
				SELECT 1 FROM recommendation_sets rs
				WHERE rs.org_id = ns.org_id
				  AND rs.cluster_uuid = ns.cluster_uuid
				  AND rs.namespace = ns.namespace_name
				  AND (
					COALESCE(rs.idle_state, 'active') <> 'zombie'
					OR (rs.has_gpu = true AND COALESCE(rs.gpu_idle_state, 'active') <> 'zombie')
				  )
			) THEN 'zombie'
			ELSE 'idle'
		END
		WHERE ns.org_id = $1 AND ns.cluster_uuid = $2::uuid
		  AND ns.schedule_type = 'all_hours'`,
		orgID, clusterUUID)
	if err != nil {
		return err
	}
	return nil
}
