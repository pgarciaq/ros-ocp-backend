package engine

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

// ClusterQuotaSnapshot is the latest hard/used per ClusterResourceQuota from digests.
type ClusterQuotaSnapshot struct {
	ClusterQuotaName       string
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

// NamespaceQuotaClusterAggregate sums namespace quota recommendations for a cluster.
// Without CRQ-to-namespace mapping, v1 applies this cluster-wide sum to each CRQ row.
type NamespaceQuotaClusterAggregate struct {
	CPURequestRecommendedMC       int64
	CPULimitRecommendedMC         int64
	MemoryRequestRecommendedBytes int64
	MemoryLimitRecommendedBytes   int64
}

// ClusterQuotaRec is the output of the cluster-quota recommendation engine.
type ClusterQuotaRec struct {
	OrgID                         string
	ClusterUUID                   string
	ClusterQuotaName              string
	Snapshot                      ClusterQuotaSnapshot
	Recommended                   QuotaResourceBundle
	UtilizationCPURequestPercent  *int
	UtilizationMemoryRequestPercent *int
	CapacityFreed                 QuotaCapacityFreed
	SavingsDollarsMonthly         int
	RecommendationType            string
	RiskLevel                     string
}

// RecommendClusterQuotas produces per-CRQ recommendations for a cluster.
func RecommendClusterQuotas(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, cfg QuotaRecConfig) ([]ClusterQuotaRec, error) {
	snapshots, err := QueryLatestClusterQuotaSnapshots(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return nil, err
	}
	if len(snapshots) == 0 {
		return nil, nil
	}

	nsAgg, err := QueryNamespaceQuotaClusterAggregate(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return nil, err
	}

	var recs []ClusterQuotaRec
	for _, snap := range snapshots {
		if !snap.hasHardLimits() {
			continue
		}
		recs = append(recs, computeClusterQuotaRecommendation(orgID, clusterUUID, snap, nsAgg, cfg))
	}
	return recs, nil
}

func (s ClusterQuotaSnapshot) hasHardLimits() bool {
	return s.CPURequestHardMC > 0 || s.CPULimitHardMC > 0 ||
		s.MemoryRequestHardBytes > 0 || s.MemoryLimitHardBytes > 0
}

func computeClusterQuotaRecommendation(
	orgID, clusterUUID string,
	snap ClusterQuotaSnapshot,
	nsAgg NamespaceQuotaClusterAggregate,
	cfg QuotaRecConfig,
) ClusterQuotaRec {
	rec := ClusterQuotaRec{
		OrgID:            orgID,
		ClusterUUID:      clusterUUID,
		ClusterQuotaName: snap.ClusterQuotaName,
		Snapshot:         snap,
	}

	baseRecommended := QuotaResourceBundle{
		CPURequestMillicores: maxInt64(snap.CPURequestUsedMC, nsAgg.CPURequestRecommendedMC),
		CPULimitMillicores:   maxInt64(snap.CPULimitUsedMC, nsAgg.CPULimitRecommendedMC),
		MemoryRequestBytes:   maxInt64(snap.MemoryRequestUsedBytes, nsAgg.MemoryRequestRecommendedBytes),
		MemoryLimitBytes:     maxInt64(snap.MemoryLimitUsedBytes, nsAgg.MemoryLimitRecommendedBytes),
	}
	rec.Recommended = QuotaResourceBundle{
		CPURequestMillicores: applyHeadroom(baseRecommended.CPURequestMillicores, cfg.HeadroomBasisPoints),
		CPULimitMillicores:   applyHeadroom(baseRecommended.CPULimitMillicores, cfg.HeadroomBasisPoints),
		MemoryRequestBytes:   applyHeadroom(baseRecommended.MemoryRequestBytes, cfg.HeadroomBasisPoints),
		MemoryLimitBytes:     applyHeadroom(baseRecommended.MemoryLimitBytes, cfg.HeadroomBasisPoints),
	}

	util := QuotaUtilizationBP{
		CPURequestBP: utilizationBP(maxInt64(snap.CPURequestUsedMC, baseRecommended.CPURequestMillicores), snap.CPURequestHardMC),
		MemoryRequestBP: utilizationBP(
			maxInt64(snap.MemoryRequestUsedBytes, baseRecommended.MemoryRequestBytes), snap.MemoryRequestHardBytes),
	}
	rec.UtilizationCPURequestPercent = bpToPercentInt(util.CPURequestBP)
	rec.UtilizationMemoryRequestPercent = bpToPercentInt(util.MemoryRequestBP)
	rec.RiskLevel = classifyClusterQuotaRisk(util, cfg)
	rec.RecommendationType, rec.CapacityFreed = classifyClusterQuotaRecommendation(snap, rec.Recommended, util, cfg)

	return rec
}

func bpToPercentInt(bp *int) *int {
	if bp == nil {
		return nil
	}
	pct := *bp / 100
	return &pct
}

func classifyClusterQuotaRisk(util QuotaUtilizationBP, cfg QuotaRecConfig) string {
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

func classifyClusterQuotaRecommendation(
	snap ClusterQuotaSnapshot,
	recommended QuotaResourceBundle,
	util QuotaUtilizationBP,
	cfg QuotaRecConfig,
) (string, QuotaCapacityFreed) {
	freed := QuotaCapacityFreed{}
	needsRaise := maxUtilizationBP(util) >= cfg.HighRiskThresholdBP

	cpuTighten := snap.CPURequestHardMC > 0 && recommended.CPURequestMillicores > 0 &&
		recommended.CPURequestMillicores < snap.CPURequestHardMC
	memTighten := snap.MemoryRequestHardBytes > 0 && recommended.MemoryRequestBytes > 0 &&
		recommended.MemoryRequestBytes < snap.MemoryRequestHardBytes

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

// ApplyClusterQuotaSavings computes estimated monthly savings in whole dollars.
func ApplyClusterQuotaSavings(recs []ClusterQuotaRec, costData *costdata.ClusterCostData) {
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
		recs[i].SavingsDollarsMonthly = int(math.Round(savings))
	}
}

// QueryLatestClusterQuotaSnapshots returns the newest digest row per CRQ name.
func QueryLatestClusterQuotaSnapshots(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) ([]ClusterQuotaSnapshot, error) {
	query := `
		SELECT DISTINCT ON (cluster_quota_name)
			cluster_quota_name,
			cpu_request_hard, cpu_limit_hard,
			memory_request_hard, memory_limit_hard,
			cpu_request_used, cpu_limit_used,
			memory_request_used, memory_limit_used,
			report_date
		FROM daily_cluster_quota_digests
		WHERE org_id = $1 AND cluster_uuid = $2::uuid
			AND (
				cpu_request_hard IS NOT NULL OR cpu_limit_hard IS NOT NULL OR
				memory_request_hard IS NOT NULL OR memory_limit_hard IS NOT NULL
			)
		ORDER BY cluster_quota_name, report_date DESC`

	rows, err := pool.Query(ctx, query, orgID, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("query cluster quota snapshots: %w", err)
	}
	defer rows.Close()

	var out []ClusterQuotaSnapshot
	for rows.Next() {
		var s ClusterQuotaSnapshot
		var reportDate time.Time
		var cpuReqHard, cpuLimHard, memReqHard, memLimHard *int64
		var cpuReqUsed, cpuLimUsed, memReqUsed, memLimUsed *int64
		if err := rows.Scan(
			&s.ClusterQuotaName,
			&cpuReqHard, &cpuLimHard, &memReqHard, &memLimHard,
			&cpuReqUsed, &cpuLimUsed, &memReqUsed, &memLimUsed,
			&reportDate,
		); err != nil {
			return nil, fmt.Errorf("scan cluster quota snapshot: %w", err)
		}
		s.CPURequestHardMC = derefInt64(cpuReqHard)
		s.CPULimitHardMC = derefInt64(cpuLimHard)
		s.MemoryRequestHardBytes = derefInt64(memReqHard)
		s.MemoryLimitHardBytes = derefInt64(memLimHard)
		s.CPURequestUsedMC = derefInt64(cpuReqUsed)
		s.CPULimitUsedMC = derefInt64(cpuLimUsed)
		s.MemoryRequestUsedBytes = derefInt64(memReqUsed)
		s.MemoryLimitUsedBytes = derefInt64(memLimUsed)
		s.LastObservedAt = reportDate
		out = append(out, s)
	}
	return out, rows.Err()
}

// QueryNamespaceQuotaClusterAggregate sums namespace quota_recommendation_sets for a cluster.
func QueryNamespaceQuotaClusterAggregate(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) (NamespaceQuotaClusterAggregate, error) {
	var agg NamespaceQuotaClusterAggregate
	err := pool.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(cpu_request_recommended_millicores), 0),
			COALESCE(SUM(cpu_limit_recommended_millicores), 0),
			COALESCE(SUM(memory_request_recommended_bytes), 0),
			COALESCE(SUM(memory_limit_recommended_bytes), 0)
		FROM quota_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid`,
		orgID, clusterUUID,
	).Scan(
		&agg.CPURequestRecommendedMC,
		&agg.CPULimitRecommendedMC,
		&agg.MemoryRequestRecommendedBytes,
		&agg.MemoryLimitRecommendedBytes,
	)
	if err != nil {
		return agg, fmt.Errorf("query namespace quota cluster aggregate: %w", err)
	}
	return agg, nil
}

// WriteClusterQuotaRecommendations upserts cluster-quota recommendations.
func WriteClusterQuotaRecommendations(ctx context.Context, pool *pgxpool.Pool, recs []ClusterQuotaRec) error {
	for _, r := range recs {
		s := r.Snapshot
		cpuCoresFreed := r.CapacityFreed.CPUMillicores / 1000
		_, err := pool.Exec(ctx, `
			INSERT INTO cluster_quota_recommendation_sets (
				org_id, cluster_uuid, cluster_quota_name,
				recommendation_type, risk_level,
				cpu_request_hard, cpu_request_used, cpu_request_recommended,
				cpu_limit_hard, cpu_limit_used, cpu_limit_recommended,
				memory_request_hard, memory_request_used, memory_request_recommended,
				memory_limit_hard, memory_limit_used, memory_limit_recommended,
				utilization_cpu_request_percent, utilization_memory_request_percent,
				savings_cpu_cores_freed, savings_memory_bytes_freed, savings_dollars_monthly,
				updated_at
			) VALUES (
				$1, $2::uuid, $3,
				$4, $5,
				$6, $7, $8,
				$9, $10, $11,
				$12, $13, $14,
				$15, $16, $17,
				$18, $19,
				$20, $21, $22,
				NOW()
			)
			ON CONFLICT (org_id, cluster_uuid, cluster_quota_name)
			DO UPDATE SET
				recommendation_type = EXCLUDED.recommendation_type,
				risk_level = EXCLUDED.risk_level,
				cpu_request_hard = EXCLUDED.cpu_request_hard,
				cpu_request_used = EXCLUDED.cpu_request_used,
				cpu_request_recommended = EXCLUDED.cpu_request_recommended,
				cpu_limit_hard = EXCLUDED.cpu_limit_hard,
				cpu_limit_used = EXCLUDED.cpu_limit_used,
				cpu_limit_recommended = EXCLUDED.cpu_limit_recommended,
				memory_request_hard = EXCLUDED.memory_request_hard,
				memory_request_used = EXCLUDED.memory_request_used,
				memory_request_recommended = EXCLUDED.memory_request_recommended,
				memory_limit_hard = EXCLUDED.memory_limit_hard,
				memory_limit_used = EXCLUDED.memory_limit_used,
				memory_limit_recommended = EXCLUDED.memory_limit_recommended,
				utilization_cpu_request_percent = EXCLUDED.utilization_cpu_request_percent,
				utilization_memory_request_percent = EXCLUDED.utilization_memory_request_percent,
				savings_cpu_cores_freed = EXCLUDED.savings_cpu_cores_freed,
				savings_memory_bytes_freed = EXCLUDED.savings_memory_bytes_freed,
				savings_dollars_monthly = EXCLUDED.savings_dollars_monthly,
				updated_at = NOW()`,
			r.OrgID, r.ClusterUUID, r.ClusterQuotaName,
			r.RecommendationType, r.RiskLevel,
			nullableInt64(s.CPURequestHardMC), nullableInt64(s.CPURequestUsedMC), nullableInt64(r.Recommended.CPURequestMillicores),
			nullableInt64(s.CPULimitHardMC), nullableInt64(s.CPULimitUsedMC), nullableInt64(r.Recommended.CPULimitMillicores),
			nullableInt64(s.MemoryRequestHardBytes), nullableInt64(s.MemoryRequestUsedBytes), nullableInt64(r.Recommended.MemoryRequestBytes),
			nullableInt64(s.MemoryLimitHardBytes), nullableInt64(s.MemoryLimitUsedBytes), nullableInt64(r.Recommended.MemoryLimitBytes),
			r.UtilizationCPURequestPercent, r.UtilizationMemoryRequestPercent,
			nullableInt64(cpuCoresFreed), nullableInt64(r.CapacityFreed.MemoryBytes), nullableInt64(int64(r.SavingsDollarsMonthly)),
		)
		if err != nil {
			return fmt.Errorf("upsert cluster quota recommendation %s: %w", r.ClusterQuotaName, err)
		}
	}
	return nil
}
