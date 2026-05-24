package api

import (
	"context"
	"strings"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func fetchClusterCurrency(ctx context.Context, orgID, clusterUUID string) string {
	provider := getGPUCostProvider()
	if provider == nil || clusterUUID == "" {
		return costdata.DefaultCurrency
	}
	kokuOrgID := strings.TrimPrefix(orgID, "org")
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -30)
	cd, err := provider.GetEffectiveRates(ctx, kokuOrgID, clusterUUID, start, now)
	if err != nil {
		return costdata.DefaultCurrency
	}
	return costdata.ResolveCurrency(cd)
}

func enrichContainerCurrency(ctx context.Context, orgID string, results []model.NativeContainerResult) {
	clusterCurrencies := map[string]string{}
	for i := range results {
		clusterUUID := results[i].ClusterUUID
		if currency, ok := clusterCurrencies[clusterUUID]; ok {
			results[i].Currency = currency
			continue
		}
		currency := fetchClusterCurrency(ctx, orgID, clusterUUID)
		clusterCurrencies[clusterUUID] = currency
		results[i].Currency = currency
	}
}
