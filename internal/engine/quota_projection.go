package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

// DefaultQuotaContainerTerm is the term used when quota recommendations are persisted.
func DefaultQuotaContainerTerm() string { return quotaContainerTerm }

// DefaultQuotaContainerEngine is the engine used when quota recommendations are persisted.
func DefaultQuotaContainerEngine() string { return quotaContainerEngine }

// NeedsQuotaReprojection reports whether list/detail should recompute from container aggregates.
func NeedsQuotaReprojection(term, engine string) bool {
	if term == "" {
		term = DefaultQuotaContainerTerm()
	}
	if engine == "" {
		engine = DefaultQuotaContainerEngine()
	}
	return term != DefaultQuotaContainerTerm() || engine != DefaultQuotaContainerEngine()
}

// ComputeQuotaRecommendation exposes quota recommendation math for API reprojection.
func ComputeQuotaRecommendation(
	orgID, clusterUUID string,
	snap NamespaceQuotaSnapshot,
	agg ContainerQuotaAggregate,
	cfg QuotaRecConfig,
) QuotaRec {
	return computeQuotaRecommendation(orgID, clusterUUID, snap, agg, cfg)
}

// ComputeClusterQuotaRecommendation exposes cluster-quota recommendation math for API reprojection.
func ComputeClusterQuotaRecommendation(
	orgID, clusterUUID string,
	snap ClusterQuotaSnapshot,
	nsAgg NamespaceQuotaClusterAggregate,
	cfg QuotaRecConfig,
) ClusterQuotaRec {
	return computeClusterQuotaRecommendation(orgID, clusterUUID, snap, nsAgg, cfg)
}

// ReprojectQuotaRec applies savings when cost data is available.
func ReprojectQuotaRec(
	orgID, clusterUUID string,
	snap NamespaceQuotaSnapshot,
	agg ContainerQuotaAggregate,
	cfg QuotaRecConfig,
	costData *costdata.ClusterCostData,
) QuotaRec {
	rec := computeQuotaRecommendation(orgID, clusterUUID, snap, agg, cfg)
	if costData != nil {
		recs := []QuotaRec{rec}
		ApplyQuotaSavings(recs, costData)
		rec = recs[0]
	}
	return rec
}

// ReprojectClusterQuotaRec applies savings when cost data is available.
func ReprojectClusterQuotaRec(
	orgID, clusterUUID string,
	snap ClusterQuotaSnapshot,
	nsAgg NamespaceQuotaClusterAggregate,
	cfg QuotaRecConfig,
	costData *costdata.ClusterCostData,
) ClusterQuotaRec {
	rec := computeClusterQuotaRecommendation(orgID, clusterUUID, snap, nsAgg, cfg)
	if costData != nil {
		recs := []ClusterQuotaRec{rec}
		ApplyClusterQuotaSavings(recs, costData)
		rec = recs[0]
	}
	return rec
}

// QueryNamespaceQuotaSnapshotsForNamespaces loads hard/used snapshots for reprojection.
func QueryNamespaceQuotaSnapshotsForNamespaces(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	namespaces []string,
) ([]NamespaceQuotaSnapshot, error) {
	if len(namespaces) == 0 {
		return nil, nil
	}
	query := `
		SELECT namespace, quota_name,
			cpu_request_hard_millicores, cpu_limit_hard_millicores,
			memory_request_hard_bytes, memory_limit_hard_bytes,
			cpu_request_used_millicores, cpu_limit_used_millicores,
			memory_request_used_bytes, memory_limit_used_bytes,
			storage_request_hard_bytes, storage_request_used_bytes,
			pods_hard, pods_used,
			last_observed_at
		FROM quota_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = ANY($3::text[])`
	rows, err := pool.Query(ctx, query, orgID, clusterUUID, namespaces)
	if err != nil {
		return nil, fmt.Errorf("query namespace quota snapshots: %w", err)
	}
	defer rows.Close()

	var out []NamespaceQuotaSnapshot
	for rows.Next() {
		var snap NamespaceQuotaSnapshot
		if err := rows.Scan(
			&snap.Namespace, &snap.QuotaName,
			&snap.CPURequestHardMC, &snap.CPULimitHardMC,
			&snap.MemoryRequestHardBytes, &snap.MemoryLimitHardBytes,
			&snap.CPURequestUsedMC, &snap.CPULimitUsedMC,
			&snap.MemoryRequestUsedBytes, &snap.MemoryLimitUsedBytes,
			&snap.StorageRequestHardBytes, &snap.StorageRequestUsedBytes,
			&snap.PodsHard, &snap.PodsUsed,
			&snap.LastObservedAt,
		); err != nil {
			return nil, fmt.Errorf("scan namespace quota snapshot: %w", err)
		}
		out = append(out, snap)
	}
	return out, rows.Err()
}

// QueryReprojectedNamespaceQuotaAggregateForNamespaces sums namespace quota recommendations
// recomputed from container aggregates for the requested term/engine.
func QueryReprojectedNamespaceQuotaAggregateForNamespaces(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	namespaces []string,
	term, engine string,
	cfg QuotaRecConfig,
) (NamespaceQuotaClusterAggregate, error) {
	var agg NamespaceQuotaClusterAggregate
	if len(namespaces) == 0 {
		return agg, nil
	}

	containerAggs, err := QueryContainerQuotaAggregates(ctx, pool, orgID, clusterUUID, term, engine)
	if err != nil {
		return agg, err
	}
	snapshots, err := QueryNamespaceQuotaSnapshotsForNamespaces(ctx, pool, orgID, clusterUUID, namespaces)
	if err != nil {
		return agg, err
	}
	for _, snap := range snapshots {
		rec := computeQuotaRecommendation(orgID, clusterUUID, snap, containerAggs[snap.Namespace], cfg)
		agg.CPURequestRecommendedMC += rec.Recommended.CPURequestMillicores
		agg.CPULimitRecommendedMC += rec.Recommended.CPULimitMillicores
		agg.MemoryRequestRecommendedBytes += rec.Recommended.MemoryRequestBytes
		agg.MemoryLimitRecommendedBytes += rec.Recommended.MemoryLimitBytes
	}
	return agg, nil
}

// FetchRecommendationCostData loads cluster cost rates for on-the-fly savings estimates.
func FetchRecommendationCostData(ctx context.Context, orgID, clusterUUID string) *costdata.ClusterCostData {
	appCfg := config.GetConfig()
	if appCfg == nil || !appCfg.SavingsEstimatesEnabled {
		return nil
	}
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -appCfg.MaxLookbackDays)
	return fetchRecalcCostData(ctx, orgID, clusterUUID, start, now)
}
