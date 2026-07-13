package quota

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
)

// ClusterQuotaSnapshot is the latest hard/used per ClusterResourceQuota from digests.
type ClusterQuotaSnapshot struct {
	ClusterQuotaName       string
	Namespaces             string
	CPURequestHardMC       int64
	CPULimitHardMC         int64
	MemoryRequestHardBytes int64
	MemoryLimitHardBytes   int64
	CPURequestUsedMC       int64
	CPULimitUsedMC         int64
	MemoryRequestUsedBytes int64
	MemoryLimitUsedBytes   int64
	StorageRequestHardBytes int64
	StorageRequestUsedBytes int64
	PodsHard               int64
	PodsUsed               int64
	ObjectCountHard        int64
	ObjectCountUsed        int64
	LastObservedAt         time.Time
}

// NamespaceQuotaClusterAggregate sums namespace quota recommendations for selected namespaces.
type NamespaceQuotaClusterAggregate struct {
	CPURequestRecommendedMC       int64
	CPULimitRecommendedMC         int64
	MemoryRequestRecommendedBytes int64
	MemoryLimitRecommendedBytes   int64
}

// ClusterQuotaRec is the output of the cluster-quota recommendation engine.
type ClusterQuotaRec struct {
	OrgID                            string
	ClusterUUID                      string
	ClusterQuotaName                 string
	Namespaces                       string
	Snapshot                         ClusterQuotaSnapshot
	Recommended                      QuotaResourceBundle
	StorageRecommendedBytes          int64
	PodsRecommended                  int64
	UtilizationCPURequestPercent     *int
	UtilizationMemoryRequestPercent  *int
	UtilizationStorageRequestPercent *int
	UtilizationPodsPercent           *int
	CapacityFreed                    QuotaCapacityFreed
	EstimatedSavingsCents            int64
	RecommendationType               string
	RiskLevel                        string
	NotificationCodes                []int16
	Expl                             core.ClusterQuotaExplanationFactors
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

	var recs []ClusterQuotaRec
	for _, snap := range snapshots {
		if !snap.hasHardLimits() {
			continue
		}
		nsAgg, err := QueryNamespaceQuotaAggregateForNamespaces(ctx, pool, orgID, clusterUUID, parseClusterQuotaNamespaces(snap.Namespaces))
		if err != nil {
			return nil, err
		}
		recs = append(recs, ComputeClusterQuotaRecommendation(orgID, clusterUUID, snap, nsAgg, cfg))
	}
	return recs, nil
}

func parseClusterQuotaNamespaces(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s ClusterQuotaSnapshot) hasHardLimits() bool {
	return s.CPURequestHardMC > 0 || s.CPULimitHardMC > 0 ||
		s.MemoryRequestHardBytes > 0 || s.MemoryLimitHardBytes > 0 ||
		s.StorageRequestHardBytes > 0 || s.PodsHard > 0 || s.ObjectCountHard > 0
}

func ComputeClusterQuotaRecommendation(
	orgID, clusterUUID string,
	snap ClusterQuotaSnapshot,
	nsAgg NamespaceQuotaClusterAggregate,
	cfg QuotaRecConfig,
) ClusterQuotaRec {
	rec := ClusterQuotaRec{
		OrgID:            orgID,
		ClusterUUID:      clusterUUID,
		ClusterQuotaName: snap.ClusterQuotaName,
		Namespaces:       snap.Namespaces,
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
	rec.StorageRecommendedBytes = applyHeadroom(snap.StorageRequestUsedBytes, cfg.HeadroomBasisPoints)
	rec.PodsRecommended = applyHeadroom(snap.PodsUsed, cfg.HeadroomBasisPoints)

	util := clusterQuotaUtilizationBP(snap, baseRecommended, rec.StorageRecommendedBytes, rec.PodsRecommended)
	rec.UtilizationCPURequestPercent = bpToPercentInt(util.CPURequestBP)
	rec.UtilizationMemoryRequestPercent = bpToPercentInt(util.MemoryRequestBP)
	rec.UtilizationStorageRequestPercent = bpToPercentInt(util.StorageRequestBP)
	rec.UtilizationPodsPercent = bpToPercentInt(util.PodsBP)
	rec.RiskLevel = classifyClusterQuotaRisk(util, cfg)
	rec.RecommendationType, rec.CapacityFreed = classifyClusterQuotaRecommendation(snap, rec.Recommended, rec.StorageRecommendedBytes, rec.PodsRecommended, util, cfg)
	rec.NotificationCodes = ClusterQuotaNotificationCodes(rec)

	rec.Expl = core.ClusterQuotaExplanationFactors{
		HeadroomBP:           int32(cfg.HeadroomBasisPoints),
		NSQuotaCPUSumMC:     nsAgg.CPURequestRecommendedMC,
		NSQuotaMemSumBytes:   nsAgg.MemoryRequestRecommendedBytes,
		BaseCPUMC:            baseRecommended.CPURequestMillicores,
		MaxUtilizationBP:     int32(maxClusterQuotaUtilizationBP(util)),
		RecommendationReason: rec.RecommendationType,
	}

	return rec
}

func bpToPercentInt(bp *int) *int {
	if bp == nil {
		return nil
	}
	pct := *bp / 100
	return &pct
}

type clusterQuotaUtilization struct {
	CPURequestBP     *int
	MemoryRequestBP  *int
	StorageRequestBP *int
	PodsBP           *int
	ObjectCountBP    *int
}

func clusterQuotaUtilizationBP(
	snap ClusterQuotaSnapshot,
	base QuotaResourceBundle,
	storageRecommended, podsRecommended int64,
) clusterQuotaUtilization {
	return clusterQuotaUtilization{
		CPURequestBP: utilizationBP(maxInt64(snap.CPURequestUsedMC, base.CPURequestMillicores), snap.CPURequestHardMC),
		MemoryRequestBP: utilizationBP(
			maxInt64(snap.MemoryRequestUsedBytes, base.MemoryRequestBytes), snap.MemoryRequestHardBytes),
		StorageRequestBP: utilizationBP(maxInt64(snap.StorageRequestUsedBytes, storageRecommended), snap.StorageRequestHardBytes),
		PodsBP:           utilizationBP(maxInt64(snap.PodsUsed, podsRecommended), snap.PodsHard),
		ObjectCountBP:    utilizationBP(snap.ObjectCountUsed, snap.ObjectCountHard),
	}
}

func maxClusterQuotaUtilizationBP(util clusterQuotaUtilization) int {
	maxBP := 0
	for _, bp := range []*int{
		util.CPURequestBP, util.MemoryRequestBP, util.StorageRequestBP, util.PodsBP, util.ObjectCountBP,
	} {
		if bp != nil && *bp > maxBP {
			maxBP = *bp
		}
	}
	return maxBP
}

func classifyClusterQuotaRisk(util clusterQuotaUtilization, cfg QuotaRecConfig) string {
	maxBP := maxClusterQuotaUtilizationBP(util)
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
	storageRecommended, podsRecommended int64,
	util clusterQuotaUtilization,
	cfg QuotaRecConfig,
) (string, QuotaCapacityFreed) {
	freed := QuotaCapacityFreed{}
	needsRaise := maxClusterQuotaUtilizationBP(util) >= cfg.HighRiskThresholdBP

	cpuTighten := snap.CPURequestHardMC > 0 && recommended.CPURequestMillicores > 0 &&
		recommended.CPURequestMillicores < snap.CPURequestHardMC
	memTighten := snap.MemoryRequestHardBytes > 0 && recommended.MemoryRequestBytes > 0 &&
		recommended.MemoryRequestBytes < snap.MemoryRequestHardBytes
	storageTighten := snap.StorageRequestHardBytes > 0 && storageRecommended > 0 &&
		storageRecommended < snap.StorageRequestHardBytes
	podsTighten := snap.PodsHard > 0 && podsRecommended > 0 && podsRecommended < snap.PodsHard

	if cpuTighten {
		freed.CPUMillicores = snap.CPURequestHardMC - recommended.CPURequestMillicores
	}
	if memTighten {
		freed.MemoryBytes = snap.MemoryRequestHardBytes - recommended.MemoryRequestBytes
	}
	if storageTighten {
		freed.StorageBytes = snap.StorageRequestHardBytes - storageRecommended
	}
	if podsTighten {
		freed.PodsFreed = snap.PodsHard - podsRecommended
	}

	if needsRaise {
		return QuotaRecTypeRaise, freed
	}
	if cpuTighten || memTighten || storageTighten || podsTighten {
		return QuotaRecTypeTighten, freed
	}
	if snap.hasHardLimits() {
		return QuotaRecTypeOptimal, freed
	}
	return QuotaRecTypeNone, freed
}

// ApplyClusterQuotaSavings computes estimated monthly savings in cents.
// CPU and memory use hourly rates; storage uses storage_gb_request_per_month (or usage fallback).
// Pods have no cost-model metric — capacity_freed.pods is reported but not monetized.
func ApplyClusterQuotaSavings(recs []ClusterQuotaRec, costData *costdata.ClusterCostData) {
	if costData == nil {
		return
	}
	cpuRate := core.RateMicroCentsPerMCHour(core.CPUCoreHourlyRate(costData))
	memRate := core.RateMicroCentsPerGiBHour(core.MemoryGBHourlyRate(costData))
	storageRate := core.RateMicroCentsPerGiBMonth(core.StorageRequestPerMonth(costData))

	for i := range recs {
		if recs[i].RecommendationType != QuotaRecTypeTighten {
			continue
		}
		cpuDeltaMC := recs[i].CapacityFreed.CPUMillicores
		memDeltaBytes := recs[i].CapacityFreed.MemoryBytes
		storageDeltaBytes := recs[i].CapacityFreed.StorageBytes

		savingsMicroCents := core.QuotaTightenSavingsMicroCents(
			cpuDeltaMC, memDeltaBytes, storageDeltaBytes, cpuRate, memRate, storageRate,
		)
		recs[i].EstimatedSavingsCents = core.MicroCentsToCents(savingsMicroCents)
	}
}

// QueryLatestClusterQuotaSnapshots returns the newest digest row per CRQ name.
func QueryLatestClusterQuotaSnapshots(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) ([]ClusterQuotaSnapshot, error) {
	query := `
		SELECT DISTINCT ON (cluster_quota_name)
			cluster_quota_name, COALESCE(namespaces, ''),
			cpu_request_hard, cpu_limit_hard,
			memory_request_hard, memory_limit_hard,
			cpu_request_used, cpu_limit_used,
			memory_request_used, memory_limit_used,
			storage_request_hard, storage_request_used,
			pods_hard, pods_used,
			object_count_hard, object_count_used,
			report_date
		FROM daily_cluster_quota_digests
		WHERE org_id = $1 AND cluster_uuid = $2::uuid
			AND (
				cpu_request_hard IS NOT NULL OR cpu_limit_hard IS NOT NULL OR
				memory_request_hard IS NOT NULL OR memory_limit_hard IS NOT NULL OR
				storage_request_hard IS NOT NULL OR pods_hard IS NOT NULL OR
				object_count_hard IS NOT NULL
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
		var storageHard, storageUsed, podsHard, podsUsed, objHard, objUsed *int64
		if err := rows.Scan(
			&s.ClusterQuotaName, &s.Namespaces,
			&cpuReqHard, &cpuLimHard, &memReqHard, &memLimHard,
			&cpuReqUsed, &cpuLimUsed, &memReqUsed, &memLimUsed,
			&storageHard, &storageUsed, &podsHard, &podsUsed, &objHard, &objUsed,
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
		s.StorageRequestHardBytes = derefInt64(storageHard)
		s.StorageRequestUsedBytes = derefInt64(storageUsed)
		s.PodsHard = derefInt64(podsHard)
		s.PodsUsed = derefInt64(podsUsed)
		s.ObjectCountHard = derefInt64(objHard)
		s.ObjectCountUsed = derefInt64(objUsed)
		s.LastObservedAt = reportDate
		out = append(out, s)
	}
	return out, rows.Err()
}

// QueryNamespaceQuotaAggregateForNamespaces sums namespace quota recommendations.
// When namespaces is empty, aggregates across the whole cluster (legacy behavior).
func QueryNamespaceQuotaAggregateForNamespaces(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	namespaces []string,
) (NamespaceQuotaClusterAggregate, error) {
	var agg NamespaceQuotaClusterAggregate
	var err error
	if len(namespaces) == 0 {
		err = pool.QueryRow(ctx, `
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
	} else {
		err = pool.QueryRow(ctx, `
			SELECT
				COALESCE(SUM(cpu_request_recommended_millicores), 0),
				COALESCE(SUM(cpu_limit_recommended_millicores), 0),
				COALESCE(SUM(memory_request_recommended_bytes), 0),
				COALESCE(SUM(memory_limit_recommended_bytes), 0)
			FROM quota_recommendation_sets
			WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = ANY($3::text[])`,
			orgID, clusterUUID, namespaces,
		).Scan(
			&agg.CPURequestRecommendedMC,
			&agg.CPULimitRecommendedMC,
			&agg.MemoryRequestRecommendedBytes,
			&agg.MemoryLimitRecommendedBytes,
		)
	}
	if err != nil {
		return agg, fmt.Errorf("query namespace quota aggregate: %w", err)
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
				org_id, cluster_uuid, cluster_quota_name, namespaces,
				recommendation_type, risk_level,
				cpu_request_hard, cpu_request_used, cpu_request_recommended,
				cpu_limit_hard, cpu_limit_used, cpu_limit_recommended,
				memory_request_hard, memory_request_used, memory_request_recommended,
				memory_limit_hard, memory_limit_used, memory_limit_recommended,
				storage_request_hard, storage_request_used, storage_request_recommended,
				pods_hard, pods_used, pods_recommended,
				utilization_cpu_request_percent, utilization_memory_request_percent,
				utilization_storage_request_percent, utilization_pods_percent,
				savings_cpu_cores_freed, savings_memory_bytes_freed,
				savings_storage_bytes_freed, savings_pods_freed,
				estimated_savings_cents,
				notification_codes, updated_at,`+core.ClusterQuotaExplSQLColumns+`
			) VALUES (
				$1, $2::uuid, $3, $4,
				$5, $6,
				$7, $8, $9,
				$10, $11, $12,
				$13, $14, $15,
				$16, $17, $18,
				$19, $20, $21,
				$22, $23, $24,
				$25, $26, $27, $28,
				$29, $30, $31, $32, $33,
				$34,
				NOW(), $35, $36, $37, $38, $39, $40
			)
			ON CONFLICT (org_id, cluster_uuid, cluster_quota_name)
			DO UPDATE SET
				namespaces = EXCLUDED.namespaces,
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
				storage_request_hard = EXCLUDED.storage_request_hard,
				storage_request_used = EXCLUDED.storage_request_used,
				storage_request_recommended = EXCLUDED.storage_request_recommended,
				pods_hard = EXCLUDED.pods_hard,
				pods_used = EXCLUDED.pods_used,
				pods_recommended = EXCLUDED.pods_recommended,
				utilization_cpu_request_percent = EXCLUDED.utilization_cpu_request_percent,
				utilization_memory_request_percent = EXCLUDED.utilization_memory_request_percent,
				utilization_storage_request_percent = EXCLUDED.utilization_storage_request_percent,
				utilization_pods_percent = EXCLUDED.utilization_pods_percent,
				savings_cpu_cores_freed = EXCLUDED.savings_cpu_cores_freed,
				savings_memory_bytes_freed = EXCLUDED.savings_memory_bytes_freed,
				savings_storage_bytes_freed = EXCLUDED.savings_storage_bytes_freed,
				savings_pods_freed = EXCLUDED.savings_pods_freed,
				estimated_savings_cents = EXCLUDED.estimated_savings_cents,
				notification_codes = EXCLUDED.notification_codes,
				updated_at = NOW(),`+core.ClusterQuotaExplUpdateSet,
			append([]any{
				r.OrgID, r.ClusterUUID, r.ClusterQuotaName, nullableString(r.Namespaces),
				r.RecommendationType, r.RiskLevel,
				nullableInt64(s.CPURequestHardMC), nullableInt64(s.CPURequestUsedMC), nullableInt64(r.Recommended.CPURequestMillicores),
				nullableInt64(s.CPULimitHardMC), nullableInt64(s.CPULimitUsedMC), nullableInt64(r.Recommended.CPULimitMillicores),
				nullableInt64(s.MemoryRequestHardBytes), nullableInt64(s.MemoryRequestUsedBytes), nullableInt64(r.Recommended.MemoryRequestBytes),
				nullableInt64(s.MemoryLimitHardBytes), nullableInt64(s.MemoryLimitUsedBytes), nullableInt64(r.Recommended.MemoryLimitBytes),
				nullableInt64(s.StorageRequestHardBytes), nullableInt64(s.StorageRequestUsedBytes), nullableInt64(r.StorageRecommendedBytes),
				nullableInt64(s.PodsHard), nullableInt64(s.PodsUsed), nullableInt64(r.PodsRecommended),
				r.UtilizationCPURequestPercent, r.UtilizationMemoryRequestPercent,
				r.UtilizationStorageRequestPercent, r.UtilizationPodsPercent,
				nullableInt64(cpuCoresFreed), nullableInt64(r.CapacityFreed.MemoryBytes),
				nullableInt64(r.CapacityFreed.StorageBytes), nullableInt64(r.CapacityFreed.PodsFreed),
				nullableInt64(r.EstimatedSavingsCents),
				r.NotificationCodes,
			}, core.AppendClusterQuotaExplArgs(nil, r.Expl)...)...,
		)
		if err != nil {
			return fmt.Errorf("upsert cluster quota recommendation %s: %w", r.ClusterQuotaName, err)
		}
	}
	return nil
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
