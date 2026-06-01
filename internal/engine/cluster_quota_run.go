package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
)

// RunClusterQuotaRecommendations computes and persists cluster-quota recommendations.
func RunClusterQuotaRecommendations(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	t0 := time.Now()
	defer func() { metrics.ObserveRecommendation("cluster-quota", t0) }()

	log := logging.ForOrg(orgID, clusterUUID)
	cfg, err := ResolveClusterQuotaRecConfig(ctx, pool, orgID)
	if err != nil {
		return fmt.Errorf("resolve cluster-quota settings: %w", err)
	}

	recs, err := RecommendClusterQuotas(ctx, pool, orgID, clusterUUID, cfg)
	if err != nil {
		return fmt.Errorf("recommend cluster quotas: %w", err)
	}
	if len(recs) == 0 {
		log.Info("cluster-quota recs: no recommendations produced")
		return nil
	}

	appCfg := config.GetConfig()
	if appCfg.SavingsEstimatesEnabled {
		now := time.Now().UTC()
		start := now.AddDate(0, 0, -appCfg.MaxLookbackDays)
		costData := fetchRecalcCostData(ctx, orgID, clusterUUID, start, now)
		ApplyClusterQuotaSavings(recs, costData)
	}

	if err := WriteClusterQuotaRecommendations(ctx, pool, recs); err != nil {
		return fmt.Errorf("write cluster-quota recommendations: %w", err)
	}
	if err := AppendClusterQuotaRecommendationHistory(ctx, pool, recs); err != nil {
		return fmt.Errorf("append cluster-quota history: %w", err)
	}
	if err := PruneClusterQuotaRecommendationHistory(ctx, pool); err != nil {
		return fmt.Errorf("prune cluster-quota history: %w", err)
	}
	metrics.IncRecommendationsWritten("cluster-quota", len(recs))
	log.Infof("cluster-quota recs: wrote %d recommendations", len(recs))
	return nil
}
