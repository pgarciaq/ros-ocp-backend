package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
)

// RunSnapshotRecommendationsForCluster re-classifies snapshots from recent inventory
// using resolved tenant settings (threshold recalculation after settings PUT).
func RunSnapshotRecommendationsForCluster(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) error {
	log := logging.ForOrg(orgID, clusterUUID)
	appCfg := config.GetConfig()

	var costData *costdata.ClusterCostData
	if appCfg.SavingsEstimatesEnabled {
		now := time.Now().UTC()
		start := now.AddDate(0, 0, -appCfg.MaxLookbackDays)
		costData = fetchRecalcCostData(ctx, orgID, clusterUUID, start, now)
	}

	settings, err := ResolveSnapshotSettings(ctx, pool, orgID, costData)
	if err != nil {
		return fmt.Errorf("snapshot settings: %w", err)
	}

	// Read old snapshot recommendations BEFORE reconciliation (for quality metrics).
	oldSnapRecs, oldErr := ReadClusterOldSnapshotRecommendations(ctx, pool, orgID, clusterUUID)
	if oldErr != nil {
		log.Warnf("snapshot recalc: reading old snapshot recommendations failed: %v", oldErr)
	}

	tSnap := time.Now()
	recs, err := ClassifySnapshots(ctx, pool, orgID, clusterUUID, settings)
	metrics.ObserveRecommendation("snapshot", tSnap)
	if err != nil {
		return fmt.Errorf("classify snapshots: %w", err)
	}

	if len(recs) > 0 {
		if err := WriteSnapshotRecommendations(ctx, pool, recs); err != nil {
			return fmt.Errorf("write snapshot recommendations: %w", err)
		}
		log.Infof("snapshot recalc: wrote %d snapshot recommendations", len(recs))
		metrics.IncRecommendationsWritten("snapshot", len(recs))
	}

	// Write snapshot quality metrics BEFORE reconciliation deletes adopted rows.
	if oldSnapRecs != nil && len(oldSnapRecs) > 0 {
		freshHours := settings.InventoryFreshHours
		if freshHours <= 0 {
			freshHours = 48
		}
		currentInventory, invErr := ReadCurrentSnapshotInventoryNames(ctx, pool, orgID, clusterUUID, freshHours)
		if invErr != nil {
			log.Warnf("snapshot recalc: reading snapshot inventory for quality failed: %v", invErr)
		} else {
			qualityRows := BuildSnapshotQualityRows(orgID, clusterUUID, oldSnapRecs, currentInventory)
			if qualErr := WriteSnapshotQuality(ctx, pool, qualityRows); qualErr != nil {
				log.Warnf("snapshot recalc: writing snapshot quality metrics failed: %v", qualErr)
			}
		}
	}

	staleGrace := appCfg.SnapshotStaleGraceHours
	if staleGrace <= 0 {
		staleGrace = 48
	}
	removed, err := ReconcileSnapshotRecommendations(ctx, pool, orgID, clusterUUID, settings.InventoryFreshHours, staleGrace)
	if err != nil {
		return fmt.Errorf("reconcile snapshots: %w", err)
	}
	if removed > 0 {
		log.Infof("snapshot recalc: reconciled (removed) %d stale recommendations", removed)
	}
	return nil
}
