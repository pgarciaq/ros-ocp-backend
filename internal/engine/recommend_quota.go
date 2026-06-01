package engine

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

const (
	QuotaRecTypeTighten  = "tighten"
	QuotaRecTypeRaise    = "raise"
	QuotaRecTypeOptimal  = "optimal"
	QuotaRecTypeNone     = "none"
	QuotaRiskHigh        = "high"
	QuotaRiskMedium      = "medium"
	QuotaRiskLow         = "low"
	QuotaRiskNone        = "none"
	quotaContainerTerm   = "medium"
	quotaContainerEngine = "cost"
)

// QuotaRecConfig holds quota recommendation thresholds (basis points).
type QuotaRecConfig struct {
	HeadroomBasisPoints     int
	HighRiskThresholdBP     int
	MediumRiskThresholdBP   int
}

// QuotaRecConfigFromApp builds quota config from application settings.
func QuotaRecConfigFromApp(cfg *config.Config) QuotaRecConfig {
	headroomBP := 10000 + cfg.QuotaHeadroomPercent*100
	if headroomBP < 10000 {
		headroomBP = 10000
	}
	return QuotaRecConfig{
		HeadroomBasisPoints:   headroomBP,
		HighRiskThresholdBP:   cfg.QuotaHighRiskThresholdPercent * 100,
		MediumRiskThresholdBP: cfg.QuotaMediumRiskThresholdPercent * 100,
	}
}

// NamespaceQuotaSnapshot is the latest hard/used quota per namespace from digests.
type NamespaceQuotaSnapshot struct {
	Namespace              string
	CPURequestHardMC       int64
	CPULimitHardMC         int64
	MemoryRequestHardBytes int64
	MemoryLimitHardBytes   int64
	CPURequestUsedMC       int64
	CPULimitUsedMC         int64
	MemoryRequestUsedBytes int64
	MemoryLimitUsedBytes   int64
	LastObservedAt         time.Time
}

// ContainerQuotaAggregate sums container recommendations per namespace.
type ContainerQuotaAggregate struct {
	CPURequestSumMC       int64
	CPULimitSumMC         int64
	MemoryRequestSumBytes int64
	MemoryLimitSumBytes   int64
}

// QuotaRec is the output of the quota recommendation engine.
type QuotaRec struct {
	OrgID        string
	ClusterUUID  string
	Namespace    string
	Snapshot     NamespaceQuotaSnapshot
	Recommended  QuotaResourceBundle
	HeadroomBP   int
	Utilization  QuotaUtilizationBP
	CapacityFreed QuotaCapacityFreed
	EstimatedSavingsCents int64
	Currency              string
	RecommendationType    string
	RiskLevel             string
	NotificationCodes     []int16
}

// QuotaResourceBundle holds quota hard, used, or recommended values.
type QuotaResourceBundle struct {
	CPURequestMillicores int64
	CPULimitMillicores   int64
	MemoryRequestBytes   int64
	MemoryLimitBytes     int64
}

// QuotaUtilizationBP holds utilization ratios in basis points (used or recommended vs hard).
type QuotaUtilizationBP struct {
	CPURequestBP   *int
	CPULimitBP     *int
	MemoryRequestBP *int
	MemoryLimitBP   *int
}

// QuotaCapacityFreed holds capacity that could be reclaimed by tightening quota.
type QuotaCapacityFreed struct {
	CPUMillicores int64
	MemoryBytes   int64
}

// RecommendQuotas produces per-namespace quota recommendations for a cluster.
func RecommendQuotas(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, cfg QuotaRecConfig) ([]QuotaRec, error) {
	snapshots, err := QueryLatestNamespaceQuotaSnapshots(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return nil, err
	}
	if len(snapshots) == 0 {
		return nil, nil
	}

	aggregates, err := QueryContainerQuotaAggregates(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return nil, err
	}

	var recs []QuotaRec
	for _, snap := range snapshots {
		if !snap.hasHardLimits() {
			continue
		}
		agg := aggregates[snap.Namespace]
		rec := computeQuotaRecommendation(orgID, clusterUUID, snap, agg, cfg)
		recs = append(recs, rec)
	}
	return recs, nil
}

func (s NamespaceQuotaSnapshot) hasHardLimits() bool {
	return s.CPURequestHardMC > 0 || s.CPULimitHardMC > 0 ||
		s.MemoryRequestHardBytes > 0 || s.MemoryLimitHardBytes > 0
}

func computeQuotaRecommendation(orgID, clusterUUID string, snap NamespaceQuotaSnapshot, agg ContainerQuotaAggregate, cfg QuotaRecConfig) QuotaRec {
	rec := QuotaRec{
		OrgID:       orgID,
		ClusterUUID: clusterUUID,
		Namespace:   snap.Namespace,
		Snapshot:    snap,
		HeadroomBP:  cfg.HeadroomBasisPoints,
		Currency:    money.DefaultCurrency,
	}

	rec.Recommended = QuotaResourceBundle{
		CPURequestMillicores: applyHeadroom(agg.CPURequestSumMC, cfg.HeadroomBasisPoints),
		CPULimitMillicores:   applyHeadroom(agg.CPULimitSumMC, cfg.HeadroomBasisPoints),
		MemoryRequestBytes:   applyHeadroom(agg.MemoryRequestSumBytes, cfg.HeadroomBasisPoints),
		MemoryLimitBytes:     applyHeadroom(agg.MemoryLimitSumBytes, cfg.HeadroomBasisPoints),
	}

	// Signal C: utilization vs hard uses the greater of quota "used" and container rec sums.
	rec.Utilization = QuotaUtilizationBP{
		CPURequestBP: utilizationBP(maxInt64(snap.CPURequestUsedMC, agg.CPURequestSumMC), snap.CPURequestHardMC),
		CPULimitBP:   utilizationBP(maxInt64(snap.CPULimitUsedMC, agg.CPULimitSumMC), snap.CPULimitHardMC),
		MemoryRequestBP: utilizationBP(
			maxInt64(snap.MemoryRequestUsedBytes, agg.MemoryRequestSumBytes), snap.MemoryRequestHardBytes),
		MemoryLimitBP: utilizationBP(
			maxInt64(snap.MemoryLimitUsedBytes, agg.MemoryLimitSumBytes), snap.MemoryLimitHardBytes),
	}

	rec.RiskLevel = classifyQuotaRisk(rec.Utilization, cfg)
	rec.RecommendationType, rec.CapacityFreed = classifyQuotaRecommendation(snap, rec.Recommended, rec.Utilization, cfg)
	rec.NotificationCodes = QuotaNotificationCodes(snap, rec)

	return rec
}

func applyHeadroom(value int64, headroomBP int) int64 {
	if value <= 0 {
		return 0
	}
	return (value * int64(headroomBP)) / 10000
}

func utilizationBP(used, hard int64) *int {
	if hard <= 0 {
		return nil
	}
	bp := int((used * 10000) / hard)
	return &bp
}

func maxUtilizationBP(util QuotaUtilizationBP) int {
	max := 0
	for _, v := range []*int{util.CPURequestBP, util.CPULimitBP, util.MemoryRequestBP, util.MemoryLimitBP} {
		if v != nil && *v > max {
			max = *v
		}
	}
	return max
}

func classifyQuotaRisk(util QuotaUtilizationBP, cfg QuotaRecConfig) string {
	maxBP := maxUtilizationBP(util)
	switch {
	case maxBP >= cfg.HighRiskThresholdBP:
		return QuotaRiskHigh
	case maxBP >= cfg.MediumRiskThresholdBP:
		return QuotaRiskMedium
	case maxBP > 0:
		return QuotaRiskLow
	default:
		return QuotaRiskNone
	}
}

func classifyQuotaRecommendation(snap NamespaceQuotaSnapshot, recommended QuotaResourceBundle, util QuotaUtilizationBP, cfg QuotaRecConfig) (string, QuotaCapacityFreed) {
	freed := QuotaCapacityFreed{}
	needsRaise := maxUtilizationBP(util) >= cfg.HighRiskThresholdBP

	cpuTighten := snap.CPURequestHardMC > 0 && recommended.CPURequestMillicores > 0 && recommended.CPURequestMillicores < snap.CPURequestHardMC
	memTighten := snap.MemoryRequestHardBytes > 0 && recommended.MemoryRequestBytes > 0 && recommended.MemoryRequestBytes < snap.MemoryRequestHardBytes

	if cpuTighten {
		freed.CPUMillicores = snap.CPURequestHardMC - recommended.CPURequestMillicores
	}
	if memTighten {
		freed.MemoryBytes = snap.MemoryRequestHardBytes - recommended.MemoryRequestBytes
	}

	if needsRaise {
		return QuotaRecTypeRaise, freed
	}
	if cpuTighten || memTighten {
		return QuotaRecTypeTighten, freed
	}
	if snap.hasHardLimits() {
		return QuotaRecTypeOptimal, freed
	}
	return QuotaRecTypeNone, freed
}

// ApplyQuotaSavings computes estimated monthly savings in cents from freed capacity.
func ApplyQuotaSavings(recs []QuotaRec, costData *costdata.ClusterCostData) {
	if costData == nil {
		return
	}
	cpuRate := CPUCoreHourlyRate(costData)
	memRate := MemoryGBHourlyRate(costData)

	for i := range recs {
		if recs[i].RecommendationType != QuotaRecTypeTighten {
			continue
		}
		cpuDelta := float64(recs[i].CapacityFreed.CPUMillicores) / 1000.0
		memDelta := float64(recs[i].CapacityFreed.MemoryBytes) / (1024.0 * 1024.0 * 1024.0)
		savings := cpuDelta*cpuRate*hoursPerMonth + memDelta*memRate*hoursPerMonth
		if savings < 0 {
			savings = 0
		}
		recs[i].EstimatedSavingsCents = money.USDToCents(math.Round(savings*100) / 100)
	}
}

// QueryLatestNamespaceQuotaSnapshots returns the newest digest row per namespace (schedule_type=all).
func QueryLatestNamespaceQuotaSnapshots(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) ([]NamespaceQuotaSnapshot, error) {
	query := `
		SELECT DISTINCT ON (namespace)
			namespace,
			cpu_request_hard_millicores, cpu_limit_hard_millicores,
			memory_request_hard_bytes, memory_limit_hard_bytes,
			cpu_request_used_millicores, cpu_limit_used_millicores,
			memory_request_used_bytes, memory_limit_used_bytes,
			bucket_date
		FROM daily_namespace_digests
		WHERE org_id = $1 AND cluster_uuid = $2::uuid
			AND schedule_type = $3
			AND (
				cpu_request_hard_millicores IS NOT NULL OR cpu_limit_hard_millicores IS NOT NULL OR
				memory_request_hard_bytes IS NOT NULL OR memory_limit_hard_bytes IS NOT NULL
			)
		ORDER BY namespace, bucket_date DESC`

	rows, err := pool.Query(ctx, query, orgID, clusterUUID, string(ingestion.ScheduleTypeAllHours))
	if err != nil {
		return nil, fmt.Errorf("query namespace quota snapshots: %w", err)
	}
	defer rows.Close()

	var out []NamespaceQuotaSnapshot
	for rows.Next() {
		var s NamespaceQuotaSnapshot
		var bucketDate time.Time
		var cpuReqHard, cpuLimHard, memReqHard, memLimHard *int64
		var cpuReqUsed, cpuLimUsed, memReqUsed, memLimUsed *int64
		if err := rows.Scan(
			&s.Namespace,
			&cpuReqHard, &cpuLimHard, &memReqHard, &memLimHard,
			&cpuReqUsed, &cpuLimUsed, &memReqUsed, &memLimUsed,
			&bucketDate,
		); err != nil {
			return nil, fmt.Errorf("scan namespace quota snapshot: %w", err)
		}
		s.CPURequestHardMC = derefInt64(cpuReqHard)
		s.CPULimitHardMC = derefInt64(cpuLimHard)
		s.MemoryRequestHardBytes = derefInt64(memReqHard)
		s.MemoryLimitHardBytes = derefInt64(memLimHard)
		s.CPURequestUsedMC = derefInt64(cpuReqUsed)
		s.CPULimitUsedMC = derefInt64(cpuLimUsed)
		s.MemoryRequestUsedBytes = derefInt64(memReqUsed)
		s.MemoryLimitUsedBytes = derefInt64(memLimUsed)
		s.LastObservedAt = bucketDate
		out = append(out, s)
	}
	return out, rows.Err()
}

func derefInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// QueryContainerQuotaAggregates sums container recommendation requests per namespace.
//
// Data comes from recommendation_sets (term=medium, engine=cost), not from in-memory
// container plugin output. Rows are written in processContainerCSVNative after digest
// ingest; the quota plugin runs on namespace CSV ingest and again after container recs
// in the same payload. If a report has only namespace CSV, aggregates reflect the
// previous ingestion cycle until container metrics arrive (one-cycle lag on deploy).
func QueryContainerQuotaAggregates(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) (map[string]ContainerQuotaAggregate, error) {
	query := `
		SELECT namespace,
			COALESCE(SUM(rec_cpu_request_millicores), 0),
			COALESCE(SUM(rec_cpu_limit_millicores), 0),
			COALESCE(SUM(rec_memory_request_kib), 0) * 1024,
			COALESCE(SUM(rec_memory_limit_kib), 0) * 1024
		FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid
			AND term = $3 AND engine = $4
		GROUP BY namespace`

	rows, err := pool.Query(ctx, query, orgID, clusterUUID, quotaContainerTerm, quotaContainerEngine)
	if err != nil {
		return nil, fmt.Errorf("query container quota aggregates: %w", err)
	}
	defer rows.Close()

	out := make(map[string]ContainerQuotaAggregate)
	for rows.Next() {
		var ns string
		var agg ContainerQuotaAggregate
		if err := rows.Scan(&ns, &agg.CPURequestSumMC, &agg.CPULimitSumMC, &agg.MemoryRequestSumBytes, &agg.MemoryLimitSumBytes); err != nil {
			return nil, fmt.Errorf("scan container quota aggregate: %w", err)
		}
		out[ns] = agg
	}
	return out, rows.Err()
}

// WriteQuotaRecommendations upserts quota recommendations for a cluster.
func WriteQuotaRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []QuotaRec) error {
	for _, r := range recs {
		s := r.Snapshot
		_, err := pool.Exec(ctx, `
			INSERT INTO quota_recommendation_sets (
				org_id, cluster_uuid, namespace,
				cpu_request_hard_millicores, cpu_limit_hard_millicores,
				memory_request_hard_bytes, memory_limit_hard_bytes,
				cpu_request_used_millicores, cpu_limit_used_millicores,
				memory_request_used_bytes, memory_limit_used_bytes,
				cpu_request_recommended_millicores, cpu_limit_recommended_millicores,
				memory_request_recommended_bytes, memory_limit_recommended_bytes,
				headroom_basis_points,
				cpu_request_utilization_bp, cpu_limit_utilization_bp,
				memory_request_utilization_bp, memory_limit_utilization_bp,
				cpu_freed_millicores, memory_freed_bytes,
				estimated_savings_cents, currency,
				recommendation_type, risk_level, notification_codes,
				last_observed_at, updated_at
			) VALUES (
				$1, $2::uuid, $3,
				$4, $5, $6, $7,
				$8, $9, $10, $11,
				$12, $13, $14, $15,
				$16,
				$17, $18, $19, $20,
				$21, $22,
				$23, $24,
				$25, $26, $27,
				$28, NOW()
			)
			ON CONFLICT (org_id, cluster_uuid, namespace)
			DO UPDATE SET
				cpu_request_hard_millicores = EXCLUDED.cpu_request_hard_millicores,
				cpu_limit_hard_millicores = EXCLUDED.cpu_limit_hard_millicores,
				memory_request_hard_bytes = EXCLUDED.memory_request_hard_bytes,
				memory_limit_hard_bytes = EXCLUDED.memory_limit_hard_bytes,
				cpu_request_used_millicores = EXCLUDED.cpu_request_used_millicores,
				cpu_limit_used_millicores = EXCLUDED.cpu_limit_used_millicores,
				memory_request_used_bytes = EXCLUDED.memory_request_used_bytes,
				memory_limit_used_bytes = EXCLUDED.memory_limit_used_bytes,
				cpu_request_recommended_millicores = EXCLUDED.cpu_request_recommended_millicores,
				cpu_limit_recommended_millicores = EXCLUDED.cpu_limit_recommended_millicores,
				memory_request_recommended_bytes = EXCLUDED.memory_request_recommended_bytes,
				memory_limit_recommended_bytes = EXCLUDED.memory_limit_recommended_bytes,
				headroom_basis_points = EXCLUDED.headroom_basis_points,
				cpu_request_utilization_bp = EXCLUDED.cpu_request_utilization_bp,
				cpu_limit_utilization_bp = EXCLUDED.cpu_limit_utilization_bp,
				memory_request_utilization_bp = EXCLUDED.memory_request_utilization_bp,
				memory_limit_utilization_bp = EXCLUDED.memory_limit_utilization_bp,
				cpu_freed_millicores = EXCLUDED.cpu_freed_millicores,
				memory_freed_bytes = EXCLUDED.memory_freed_bytes,
				estimated_savings_cents = EXCLUDED.estimated_savings_cents,
				currency = EXCLUDED.currency,
				recommendation_type = EXCLUDED.recommendation_type,
				risk_level = EXCLUDED.risk_level,
				notification_codes = EXCLUDED.notification_codes,
				last_observed_at = EXCLUDED.last_observed_at,
				updated_at = NOW()`,
			r.OrgID, r.ClusterUUID, r.Namespace,
			nullableInt64(s.CPURequestHardMC), nullableInt64(s.CPULimitHardMC),
			nullableInt64(s.MemoryRequestHardBytes), nullableInt64(s.MemoryLimitHardBytes),
			nullableInt64(s.CPURequestUsedMC), nullableInt64(s.CPULimitUsedMC),
			nullableInt64(s.MemoryRequestUsedBytes), nullableInt64(s.MemoryLimitUsedBytes),
			nullableInt64(r.Recommended.CPURequestMillicores), nullableInt64(r.Recommended.CPULimitMillicores),
			nullableInt64(r.Recommended.MemoryRequestBytes), nullableInt64(r.Recommended.MemoryLimitBytes),
			r.HeadroomBP,
			r.Utilization.CPURequestBP, r.Utilization.CPULimitBP,
			r.Utilization.MemoryRequestBP, r.Utilization.MemoryLimitBP,
			nullableInt64(r.CapacityFreed.CPUMillicores), nullableInt64(r.CapacityFreed.MemoryBytes),
			nullableInt64(r.EstimatedSavingsCents), r.Currency,
			r.RecommendationType, r.RiskLevel, r.NotificationCodes,
			s.LastObservedAt,
		)
		if err != nil {
			return fmt.Errorf("upsert quota recommendation %s: %w", r.Namespace, err)
		}
	}
	return nil
}

func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
