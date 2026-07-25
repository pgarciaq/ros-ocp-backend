package api

import (
	"context"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

func fetchClusterCurrency(ctx context.Context, orgID, clusterUUID string) string {
	return GetCachedCurrency(ctx, orgID, clusterUUID)
}

func enrichContainerCurrency(ctx context.Context, orgID string, results []model.NativeContainerResult) {
	if len(results) == 0 {
		return
	}
	sampleCluster := results[0].ClusterUUID
	currency := GetCachedCurrency(ctx, orgID, sampleCluster)
	for i := range results {
		clusterUUID := results[i].ClusterUUID
		cur := currency
		if clusterUUID != sampleCluster {
			cur = GetCachedCurrency(ctx, orgID, clusterUUID)
		}
		results[i].Currency = cur
		money.PatchUnits(results[i].EstimatedMonthlySavings, cur)
		money.PatchUnits(results[i].CPUSavings, cur)
		money.PatchUnits(results[i].MemorySavings, cur)
		money.PatchUnits(results[i].EstimatedMonthlyWaste, cur)
	}
}

func enrichNamespaceCurrency(ctx context.Context, orgID string, results []model.NativeNamespaceResult) {
	if len(results) == 0 {
		return
	}
	sampleCluster := results[0].ClusterUUID
	currency := GetCachedCurrency(ctx, orgID, sampleCluster)
	for i := range results {
		clusterUUID := results[i].ClusterUUID
		cur := currency
		if clusterUUID != sampleCluster {
			cur = GetCachedCurrency(ctx, orgID, clusterUUID)
		}
		money.PatchUnits(results[i].EstimatedMonthlyWaste, cur)
		for _, v := range results[i].Recommendations {
			if ma, ok := v.(*money.MoneyAmount); ok {
				money.PatchUnits(ma, cur)
			}
		}
	}
}

// resolveClusterCurrency is a helper for handlers that need currency from cost data.
func resolveClusterCurrency(ctx context.Context, orgID, clusterUUID string) string {
	if clusterUUID == "" {
		return costdata.DefaultCurrency
	}
	return fetchClusterCurrency(ctx, orgID, clusterUUID)
}

// resolveListCurrencyFromRequest returns ISO currency for list endpoints using cluster filter when set.
func resolveListCurrencyFromRequest(c echo.Context, orgID string) string {
	clusterFilter := queryparams.FirstFilter(c, "cluster")
	if clusterFilter == "" {
		clusterFilter = strings.TrimSpace(c.QueryParam("cluster_uuid"))
	}
	return resolveClusterCurrency(c.Request().Context(), orgID, clusterFilter)
}
