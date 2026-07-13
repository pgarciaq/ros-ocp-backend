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

// RunQuotaRecommendations computes and persists quota recommendations for a cluster.
func RunQuotaRecommendations(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	t0 := time.Now()
	defer func() { metrics.ObserveRecommendation("quota", t0) }()

	log := logging.ForOrg(orgID, clusterUUID)
	appCfg := config.GetConfig()
	cfg, err := ResolveQuotaRecConfig(ctx, pool, orgID)
	if err != nil {
		return fmt.Errorf("resolve quota settings: %w", err)
	}

	recs, err := RecommendQuotas(ctx, pool, orgID, clusterUUID, cfg)
	if err != nil {
		return fmt.Errorf("recommend quotas: %w", err)
	}
	if len(recs) == 0 {
		log.Info("quota recs: no recommendations produced")
		return nil
	}

	if appCfg.SavingsEstimatesEnabled {
		now := time.Now().UTC()
		start := now.AddDate(0, 0, -appCfg.MaxLookbackDays)
		costData := FetchRecalcCostData(ctx, orgID, clusterUUID, start, now)
		ApplyQuotaSavings(recs, costData)
	}

	if err := WriteQuotaRecommendations(ctx, pool, recs); err != nil {
		return fmt.Errorf("write quota recommendations: %w", err)
	}
	if err := AppendQuotaRecommendationHistory(ctx, pool, recs); err != nil {
		return fmt.Errorf("append quota recommendation history: %w", err)
	}
	if err := PruneQuotaRecommendationHistory(ctx, pool); err != nil {
		return fmt.Errorf("prune quota recommendation history: %w", err)
	}
	metrics.IncRecommendationsWritten("quota", len(recs))
	log.Infof("quota recs: wrote %d recommendations", len(recs))
	return nil
}
