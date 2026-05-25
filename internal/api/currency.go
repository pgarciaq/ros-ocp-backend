package api

import (
	"context"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
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
		if clusterUUID != sampleCluster {
			// Per-cluster currency when clusters differ on the page.
			results[i].Currency = GetCachedCurrency(ctx, orgID, clusterUUID)
		} else {
			results[i].Currency = currency
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
