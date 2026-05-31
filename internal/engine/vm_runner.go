package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

var vmEngines = []string{vmEngineCost, vmEnginePerformance}

// RunVMRecommendations loads digests, computes recommendations, and upserts results.
func RunVMRecommendations(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUID uuid.UUID, cfg VMRecConfig) error {
	t0 := time.Now()
	defer func() { metrics.ObserveRecommendation("vm", t0) }()

	log := logging.ForOrg(orgID, clusterUUID.String())

	termConfigs, err := LoadTermConfigCached(ctx, pool, orgID, "vm")
	if err != nil {
		log.Warnf("vm recs: load term config failed, using defaults: %v", err)
		termConfigs = nil
	}
	terms := VMTermWindowsFromConfig(termConfigs)

	maxDays := MaxVMLookbackDays(terms)
	if maxDays < 1 {
		maxDays = 30
	}
	since := time.Now().UTC().AddDate(0, 0, -maxDays).Truncate(24 * time.Hour)

	digests, err := QueryDailyVMDigests(ctx, pool, orgID, clusterUUID, since)
	if err != nil {
		return fmt.Errorf("get VM digests: %w", err)
	}
	if len(digests) == 0 {
		log.Info("vm recs: no VM digests")
		return nil
	}

	clusterTypes, err := QueryClusterInstanceTypes(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return fmt.Errorf("load cluster instance types: %w", err)
	}
	if len(clusterTypes) > 0 {
		log.Infof("vm recs: using %d cluster instance types for matching", len(clusterTypes))
	}

	prefCtx, err := QueryClusterVMPreferences(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return fmt.Errorf("load cluster vm preferences: %w", err)
	}
	if prefCtx != nil && len(prefCtx.VMToPreferenceName) > 0 {
		log.Infof("vm recs: using %d VM preference mappings", len(prefCtx.VMToPreferenceName))
	}

	type vmKey struct {
		VMName    string
		Namespace string
	}
	grouped := make(map[vmKey][]model.DailyVMDigest)
	for _, d := range digests {
		k := vmKey{VMName: d.VMName, Namespace: d.Namespace}
		grouped[k] = append(grouped[k], d)
	}

	var recs []model.VMRecommendation
	for _, vmDigests := range grouped {
		for _, term := range terms {
			for _, eng := range vmEngines {
				rec, recErr := RecommendVM(vmDigests, cfg, term, eng, clusterTypes, prefCtx)
				if recErr != nil {
					return fmt.Errorf("recommend VM %s/%s: %w", vmDigests[0].Namespace, vmDigests[0].VMName, recErr)
				}
				if rec != nil {
					recs = append(recs, *rec)
				}
			}
		}
	}

	if len(recs) == 0 {
		log.Info("vm recs: no recommendations produced")
		return nil
	}

	validTerms := make([]string, len(terms))
	for i, t := range terms {
		validTerms[i] = t.Name
	}
	if err := PersistVMRecommendations(ctx, pool, recs, validTerms); err != nil {
		return fmt.Errorf("upsert VM recommendations: %w", err)
	}

	metrics.IncRecommendationsWritten("vm", len(recs))
	log.Infof("vm recs: upserted %d recommendations", len(recs))
	return nil
}
