package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/quota"
)

// DefaultQuotaContainerTerm is the term used when quota recommendations are persisted.
func DefaultQuotaContainerTerm() string { return quota.QuotaContainerTerm }

// DefaultQuotaContainerEngine is the engine used when quota recommendations are persisted.
func DefaultQuotaContainerEngine() string { return quota.QuotaContainerEngine }

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
	snap quota.NamespaceQuotaSnapshot,
	agg quota.ContainerQuotaAggregate,
	cfg quota.QuotaRecConfig,
) quota.QuotaRec {
	return quota.ComputeQuotaRecommendation(orgID, clusterUUID, snap, agg, cfg)
}

// ComputeClusterQuotaRecommendation exposes cluster-quota recommendation math for API reprojection.
func ComputeClusterQuotaRecommendation(
	orgID, clusterUUID string,
	snap quota.ClusterQuotaSnapshot,
	nsAgg quota.NamespaceQuotaClusterAggregate,
	cfg quota.QuotaRecConfig,
) quota.ClusterQuotaRec {
	return quota.ComputeClusterQuotaRecommendation(orgID, clusterUUID, snap, nsAgg, cfg)
}

// ReprojectQuotaRec applies savings when cost data is available.
func ReprojectQuotaRec(
	orgID, clusterUUID string,
	snap quota.NamespaceQuotaSnapshot,
	agg quota.ContainerQuotaAggregate,
	cfg quota.QuotaRecConfig,
	costData *costdata.ClusterCostData,
) quota.QuotaRec {
	rec := quota.ComputeQuotaRecommendation(orgID, clusterUUID, snap, agg, cfg)
	if costData != nil {
		recs := []quota.QuotaRec{rec}
		quota.ApplyQuotaSavings(recs, costData)
		rec = recs[0]
	}
	return rec
}

// ReprojectClusterQuotaRec applies savings when cost data is available.
func ReprojectClusterQuotaRec(
	orgID, clusterUUID string,
	snap quota.ClusterQuotaSnapshot,
	nsAgg quota.NamespaceQuotaClusterAggregate,
	cfg quota.QuotaRecConfig,
	costData *costdata.ClusterCostData,
) quota.ClusterQuotaRec {
	rec := quota.ComputeClusterQuotaRecommendation(orgID, clusterUUID, snap, nsAgg, cfg)
	if costData != nil {
		recs := []quota.ClusterQuotaRec{rec}
		quota.ApplyClusterQuotaSavings(recs, costData)
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
) ([]quota.NamespaceQuotaSnapshot, error) {
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

	var out []quota.NamespaceQuotaSnapshot
	for rows.Next() {
		var snap quota.NamespaceQuotaSnapshot
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
	cfg quota.QuotaRecConfig,
) (quota.NamespaceQuotaClusterAggregate, error) {
	var agg quota.NamespaceQuotaClusterAggregate
	if len(namespaces) == 0 {
		return agg, nil
	}

	containerAggs, err := quota.QueryContainerQuotaAggregates(ctx, pool, orgID, clusterUUID, term, engine)
	if err != nil {
		return agg, err
	}
	snapshots, err := QueryNamespaceQuotaSnapshotsForNamespaces(ctx, pool, orgID, clusterUUID, namespaces)
	if err != nil {
		return agg, err
	}
	for _, snap := range snapshots {
		rec := quota.ComputeQuotaRecommendation(orgID, clusterUUID, snap, containerAggs[snap.Namespace], cfg)
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
	return FetchRecalcCostData(ctx, orgID, clusterUUID, start, now)
}
