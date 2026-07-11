package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

type quotaProjectionParams struct {
	term       string
	engine     string
	reproject  bool
}

func resolveQuotaListProjection(c echo.Context) (quotaProjectionParams, error) {
	rawTerm := strings.TrimSpace(queryparams.FirstFilter(c, "term"))
	dbTerm := engine.DefaultQuotaContainerTerm()
	if rawTerm != "" {
		normalized, err := queryparams.NormalizeRecommendationTermFilter(rawTerm)
		if err != nil {
			return quotaProjectionParams{}, err
		}
		dbTerm = normalized
	}

	engines, err := queryparams.CollectEngineFilterValues(c)
	if err != nil {
		return quotaProjectionParams{}, err
	}
	eng := engine.DefaultQuotaContainerEngine()
	switch len(engines) {
	case 0:
	case 1:
		eng = engines[0]
	default:
		return quotaProjectionParams{}, fmt.Errorf("only one engine filter is allowed")
	}

	return quotaProjectionParams{
		term:      dbTerm,
		engine:    eng,
		reproject: engine.NeedsQuotaReprojection(dbTerm, eng),
	}, nil
}

func applyQuotaListReprojection(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID string,
	cfg engine.QuotaRecConfig,
	projection quotaProjectionParams,
	items []QuotaRecommendationListItem,
) ([]QuotaRecommendationListItem, error) {
	if !projection.reproject || len(items) == 0 {
		return items, nil
	}

	byCluster := make(map[string][]int)
	for i, item := range items {
		if item.ClusterUUID == "" || item.Namespace == "" {
			continue
		}
		byCluster[item.ClusterUUID] = append(byCluster[item.ClusterUUID], i)
	}

	out := make([]QuotaRecommendationListItem, len(items))
	copy(out, items)

	for clusterUUID, indexes := range byCluster {
		aggregates, err := engine.QueryContainerQuotaAggregates(ctx, pool, orgID, clusterUUID, projection.term, projection.engine)
		if err != nil {
			return nil, err
		}
		costData := engine.FetchRecommendationCostData(ctx, orgID, clusterUUID)
		for _, idx := range indexes {
			item := out[idx]
			snap := namespaceSnapshotFromQuotaListItem(item)
			rec := engine.ReprojectQuotaRec(orgID, clusterUUID, snap, aggregates[item.Namespace], cfg, costData)
			out[idx] = quotaListItemFromRec(item, rec)
		}
	}
	return out, nil
}

func applyClusterQuotaListReprojection(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID string,
	cfg engine.QuotaRecConfig,
	projection quotaProjectionParams,
	items []ClusterQuotaRecommendationListItem,
) ([]ClusterQuotaRecommendationListItem, error) {
	if !projection.reproject || len(items) == 0 {
		return items, nil
	}

	byCluster := make(map[string][]int)
	for i, item := range items {
		if item.ClusterUUID == "" || item.ClusterQuotaName == "" {
			continue
		}
		byCluster[item.ClusterUUID] = append(byCluster[item.ClusterUUID], i)
	}

	out := make([]ClusterQuotaRecommendationListItem, len(items))
	copy(out, items)

	for clusterUUID, indexes := range byCluster {
		nsSet := make(map[string]struct{})
		for _, idx := range indexes {
			for _, ns := range items[idx].Namespaces {
				nsSet[ns] = struct{}{}
			}
		}
		allNamespaces := make([]string, 0, len(nsSet))
		for ns := range nsSet {
			allNamespaces = append(allNamespaces, ns)
		}

		containerAggs, err := engine.QueryContainerQuotaAggregates(ctx, pool, orgID, clusterUUID, projection.term, projection.engine)
		if err != nil {
			return nil, err
		}
		snapshots, err := engine.QueryNamespaceQuotaSnapshotsForNamespaces(ctx, pool, orgID, clusterUUID, allNamespaces)
		if err != nil {
			return nil, err
		}
		snapByNS := make(map[string][]engine.NamespaceQuotaSnapshot, len(snapshots))
		for _, snap := range snapshots {
			snapByNS[snap.Namespace] = append(snapByNS[snap.Namespace], snap)
		}
		costData := engine.FetchRecommendationCostData(ctx, orgID, clusterUUID)

		for _, idx := range indexes {
			item := out[idx]
			var nsAgg engine.NamespaceQuotaClusterAggregate
			for _, ns := range item.Namespaces {
				for _, snap := range snapByNS[ns] {
					rec := engine.ComputeQuotaRecommendation(orgID, clusterUUID, snap, containerAggs[snap.Namespace], cfg)
					nsAgg.CPURequestRecommendedMC += rec.Recommended.CPURequestMillicores
					nsAgg.CPULimitRecommendedMC += rec.Recommended.CPULimitMillicores
					nsAgg.MemoryRequestRecommendedBytes += rec.Recommended.MemoryRequestBytes
					nsAgg.MemoryLimitRecommendedBytes += rec.Recommended.MemoryLimitBytes
				}
			}
			snap := clusterQuotaSnapshotFromListItem(item)
			rec := engine.ReprojectClusterQuotaRec(orgID, clusterUUID, snap, nsAgg, cfg, costData)
			out[idx] = clusterQuotaListItemFromRec(item, rec)
		}
	}
	return out, nil
}

func namespaceSnapshotFromQuotaListItem(item QuotaRecommendationListItem) engine.NamespaceQuotaSnapshot {
	snap := engine.NamespaceQuotaSnapshot{
		Namespace: item.Namespace,
		QuotaName: item.QuotaName,
	}
	if item.LastObservedAt != "" {
		if t, err := time.Parse(time.RFC3339, item.LastObservedAt); err == nil {
			snap.LastObservedAt = t
		}
	}
	if item.QuotaHard != nil {
		snap.CPURequestHardMC = int64Val(item.QuotaHard.CPURequestMillicores)
		snap.CPULimitHardMC = int64Val(item.QuotaHard.CPULimitMillicores)
		snap.MemoryRequestHardBytes = int64Val(item.QuotaHard.MemoryRequestBytes)
		snap.MemoryLimitHardBytes = int64Val(item.QuotaHard.MemoryLimitBytes)
		snap.StorageRequestHardBytes = int64Val(item.QuotaHard.StorageRequestBytes)
		snap.PodsHard = int64Val(item.QuotaHard.Pods)
	}
	if item.QuotaUsed != nil {
		snap.CPURequestUsedMC = int64Val(item.QuotaUsed.CPURequestMillicores)
		snap.CPULimitUsedMC = int64Val(item.QuotaUsed.CPULimitMillicores)
		snap.MemoryRequestUsedBytes = int64Val(item.QuotaUsed.MemoryRequestBytes)
		snap.MemoryLimitUsedBytes = int64Val(item.QuotaUsed.MemoryLimitBytes)
		snap.StorageRequestUsedBytes = int64Val(item.QuotaUsed.StorageRequestBytes)
		snap.PodsUsed = int64Val(item.QuotaUsed.Pods)
	}
	return snap
}

func clusterQuotaSnapshotFromListItem(item ClusterQuotaRecommendationListItem) engine.ClusterQuotaSnapshot {
	snap := engine.ClusterQuotaSnapshot{
		ClusterQuotaName: item.ClusterQuotaName,
		Namespaces:       strings.Join(item.Namespaces, ","),
	}
	if item.QuotaHard != nil {
		snap.CPURequestHardMC = int64Val(item.QuotaHard.CPURequestMillicores)
		snap.CPULimitHardMC = int64Val(item.QuotaHard.CPULimitMillicores)
		snap.MemoryRequestHardBytes = int64Val(item.QuotaHard.MemoryRequestBytes)
		snap.MemoryLimitHardBytes = int64Val(item.QuotaHard.MemoryLimitBytes)
		snap.StorageRequestHardBytes = int64Val(item.QuotaHard.StorageRequestBytes)
		snap.PodsHard = int64Val(item.QuotaHard.Pods)
	}
	if item.QuotaUsed != nil {
		snap.CPURequestUsedMC = int64Val(item.QuotaUsed.CPURequestMillicores)
		snap.CPULimitUsedMC = int64Val(item.QuotaUsed.CPULimitMillicores)
		snap.MemoryRequestUsedBytes = int64Val(item.QuotaUsed.MemoryRequestBytes)
		snap.MemoryLimitUsedBytes = int64Val(item.QuotaUsed.MemoryLimitBytes)
		snap.StorageRequestUsedBytes = int64Val(item.QuotaUsed.StorageRequestBytes)
		snap.PodsUsed = int64Val(item.QuotaUsed.Pods)
	}
	return snap
}

func quotaListItemFromRec(base QuotaRecommendationListItem, rec engine.QuotaRec) QuotaRecommendationListItem {
	item := base
	item.RecommendationType = rec.RecommendationType
	item.RiskLevel = rec.RiskLevel
	item.QuotaRecommended = quotaResourceValuesFromBundle(rec.Recommended)
	item.Utilization = quotaUtilFromEngineBP(rec.Utilization)
	item.CapacityFreed = quotaCapacityFreedFromEngine(rec.CapacityFreed)
	if rec.EstimatedSavingsCents > 0 {
		item.EstimatedSavings = money.FormatCentsToAmountPtr(&rec.EstimatedSavingsCents, rec.Currency)
	} else {
		item.EstimatedSavings = nil
	}
	item.Notifications = notifications.MapToKruizeFormat(rec.NotificationCodes)
	return item
}

func clusterQuotaListItemFromRec(base ClusterQuotaRecommendationListItem, rec engine.ClusterQuotaRec) ClusterQuotaRecommendationListItem {
	item := base
	item.RecommendationType = rec.RecommendationType
	item.RiskLevel = rec.RiskLevel
	item.QuotaRecommended = clusterQuotaResourceValuesFromBundle(rec.Recommended, rec.StorageRecommendedBytes, rec.PodsRecommended)
	item.Utilization = clusterQuotaUtilFromEngine(rec)
	item.CapacityFreed = clusterQuotaCapacityFreedFromEngine(rec.CapacityFreed)
	if rec.EstimatedSavingsCents > 0 {
		item.EstimatedSavings = money.FormatCentsToAmountPtr(&rec.EstimatedSavingsCents, money.DefaultCurrency)
	} else {
		item.EstimatedSavings = nil
	}
	item.Notifications = notifications.MapToKruizeFormat(rec.NotificationCodes)
	return item
}

func quotaExplanationFromRec(rec engine.QuotaRec) *model.QuotaExplanationAPI {
	expl := rec.Expl
	reason := expl.RecommendationReason
	risk := expl.RiskLevel
	return model.BuildQuotaExplanationAPI(
		int32Ptr(expl.HeadroomBP),
		int32Ptr(expl.MaxUtilizationBP),
		int64Ptr(expl.ContainerCPUSumMC),
		int64Ptr(expl.ContainerMemSumBytes),
		int64Ptr(expl.SignalCCPUUsedMC),
		&risk,
		&reason,
	)
}

func clusterQuotaExplanationFromRec(rec engine.ClusterQuotaRec) *model.ClusterQuotaExplanationAPI {
	expl := rec.Expl
	reason := expl.RecommendationReason
	return model.BuildClusterQuotaExplanationAPI(
		int32Ptr(expl.HeadroomBP),
		int32Ptr(expl.MaxUtilizationBP),
		int64Ptr(expl.NSQuotaCPUSumMC),
		int64Ptr(expl.NSQuotaMemSumBytes),
		int64Ptr(expl.BaseCPUMC),
		&reason,
	)
}

func quotaResourceValuesFromBundle(bundle engine.QuotaResourceBundle) *QuotaResourceValues {
	return &QuotaResourceValues{
		CPURequestMillicores: int64PtrAlways(bundle.CPURequestMillicores),
		CPULimitMillicores:   int64PtrAlways(bundle.CPULimitMillicores),
		MemoryRequestBytes:   int64PtrAlways(bundle.MemoryRequestBytes),
		MemoryLimitBytes:     int64PtrAlways(bundle.MemoryLimitBytes),
		StorageRequestBytes:  int64PtrAlways(bundle.StorageRequestBytes),
		Pods:                 int64PtrAlways(bundle.Pods),
	}
}

func clusterQuotaResourceValuesFromBundle(bundle engine.QuotaResourceBundle, storageBytes, pods int64) *ClusterQuotaResourceValues {
	return &ClusterQuotaResourceValues{
		CPURequestMillicores: int64PtrAlways(bundle.CPURequestMillicores),
		CPULimitMillicores:   int64PtrAlways(bundle.CPULimitMillicores),
		MemoryRequestBytes:   int64PtrAlways(bundle.MemoryRequestBytes),
		MemoryLimitBytes:     int64PtrAlways(bundle.MemoryLimitBytes),
		StorageRequestBytes:  int64PtrAlways(storageBytes),
		Pods:                 int64PtrAlways(pods),
	}
}

func quotaUtilFromEngineBP(util engine.QuotaUtilizationBP) *QuotaUtilizationPercents {
	return &QuotaUtilizationPercents{
		CPURequestPercent:     bpToPercentPtrFromInt(util.CPURequestBP),
		CPULimitPercent:       bpToPercentPtrFromInt(util.CPULimitBP),
		MemoryRequestPercent:  bpToPercentPtrFromInt(util.MemoryRequestBP),
		MemoryLimitPercent:    bpToPercentPtrFromInt(util.MemoryLimitBP),
		StorageRequestPercent: bpToPercentPtrFromInt(util.StorageRequestBP),
		PodsPercent:           bpToPercentPtrFromInt(util.PodsBP),
	}
}

func clusterQuotaUtilFromEngine(rec engine.ClusterQuotaRec) *ClusterQuotaUtilizationPercents {
	return &ClusterQuotaUtilizationPercents{
		CPURequestPercent:     intToPercentPtr(rec.UtilizationCPURequestPercent),
		MemoryRequestPercent:  intToPercentPtr(rec.UtilizationMemoryRequestPercent),
		StorageRequestPercent: intToPercentPtr(rec.UtilizationStorageRequestPercent),
		PodsPercent:           intToPercentPtr(rec.UtilizationPodsPercent),
	}
}

func quotaCapacityFreedFromEngine(freed engine.QuotaCapacityFreed) *QuotaCapacityFreedResponse {
	return quotaCapacityFreedFromTotals(freed.CPUMillicores, freed.MemoryBytes, freed.StorageBytes, freed.PodsFreed)
}

func clusterQuotaCapacityFreedFromEngine(freed engine.QuotaCapacityFreed) *ClusterQuotaCapacityFreedResponse {
	if freed.CPUMillicores == 0 && freed.MemoryBytes == 0 && freed.StorageBytes == 0 && freed.PodsFreed == 0 {
		return nil
	}
	return &ClusterQuotaCapacityFreedResponse{
		CPUCoresFreed:       freed.CPUMillicores / 1000,
		MemoryBytes:         freed.MemoryBytes,
		StorageRequestBytes: freed.StorageBytes,
		PodsFreed:           freed.PodsFreed,
	}
}

func int64Val(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func int64PtrAlways(v int64) *int64 {
	return &v
}

func int64Ptr(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

func int32Ptr(v int32) *int32 {
	return &v
}

func bpToPercentPtrFromInt(bp *int) *float64 {
	if bp == nil {
		return nil
	}
	p := float64(*bp) / 100.0
	return &p
}

func intToPercentPtr(v *int) *float64 {
	if v == nil {
		return nil
	}
	p := float64(*v)
	return &p
}
