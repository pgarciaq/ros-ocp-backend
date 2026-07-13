package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/pvc"
)

// RecommendPVCs resolves org-level PVC threshold settings and notification
// thresholds, then delegates to pvc.RecommendPVCs.
func RecommendPVCs(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string, terms []core.TermConfig) ([]pvc.PVCRec, error) {
	pvcSettings, err := ResolvePVCThresholdSettings(ctx, pool, orgID)
	if err != nil {
		return nil, fmt.Errorf("load pvc thresholds: %w", err)
	}
	settings := pvc.ThresholdSettings{
		OversizedThreshold:        pvcSettings.OversizedThreshold,
		NearFullThreshold:         pvcSettings.NearFullThreshold,
		MinTrendDays:              pvcSettings.MinTrendDays,
		RecommendedSizeMultiplier: pvcSettings.RecommendedSizeMultiplier,
		MinRecommendedGiB:         pvcSettings.MinRecommendedGiB,
		DaysToFullAlert:           pvcSettings.DaysToFullAlert,
	}
	notifTh := core.NotificationThresholdsFromSizing(defaultContainerSizingThresholds)
	return pvc.RecommendPVCs(ctx, pool, orgID, clusterUUID, terms, settings, notifTh)
}
