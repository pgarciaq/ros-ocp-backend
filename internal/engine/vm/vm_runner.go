package vm

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
)

var vmEngines = []string{vmEngineCost, vmEnginePerformance}

// RunVMRecommendations loads digests, computes recommendations, and upserts results.
func RunVMRecommendations(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUID uuid.UUID, cfg VMRecConfig) error {
	t0 := time.Now()
	defer func() { metrics.ObserveRecommendation("vm", t0) }()

	log := logging.ForOrg(orgID, clusterUUID.String())

	termConfigs, err := engine.LoadTermConfigCached(ctx, pool, orgID, "vm")
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
	grouped := make(map[vmKey][]Digest)
	for _, d := range digests {
		k := vmKey{VMName: d.VMName, Namespace: d.Namespace}
		grouped[k] = append(grouped[k], d)
	}
	clusterLatest := buildClusterLatestDigests(digests)
	clusterCtx := NewClusterContext(clusterLatest)

	nodeMemGiBByNode := map[string]float64(nil)
	end := time.Now().UTC()
	if nodeDigests, nodeErr := engine.QueryNodeDigests(ctx, pool, orgID, clusterUUID.String(), since, end); nodeErr != nil {
		return fmt.Errorf("load node digests for VM NUMA: %w", nodeErr)
	} else if len(nodeDigests) > 0 {
		nodeMemGiBByNode = buildNodeMemoryGiBMap(nodeDigests)
		log.Infof("vm recs: using node memory for NUMA checks on %d nodes", len(nodeMemGiBByNode))
	}

	var recs []Recommendation
	for _, vmDigests := range grouped {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, term := range terms {
			for _, eng := range vmEngines {
				rec, recErr := RecommendVM(vmDigests, cfg, term, eng, clusterTypes, prefCtx, clusterCtx, nodeMemGiBByNode)
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

	// Read old VM recommendations before overwriting (for quality metrics).
	oldVMRecs, oldErr := ReadClusterOldVMRecommendations(ctx, pool, orgID, clusterUUID.String())
	if oldErr != nil {
		log.Warnf("vm recs: reading old VM recommendations failed: %v", oldErr)
	}

	appCfg := config.GetConfig()
	var costData *costdata.ClusterCostData
	if appCfg.SavingsEstimatesEnabled {
		start, end := engine.RecalcDateRange()
		costData = engine.FetchRecalcCostData(ctx, orgID, clusterUUID.String(), start, end)
	}
	now := time.Now().UTC()
	ApplyVMSavings(recs, costData, appCfg.SavingsEstimatesEnabled, engine.HoursInMonth(now.Year(), now.Month()))
	AppendVMPowerOffNotifications(recs)

	validTerms := make([]string, len(terms))
	for i, t := range terms {
		validTerms[i] = t.Name
	}
	if err := PersistVMRecommendations(ctx, pool, recs, validTerms); err != nil {
		return fmt.Errorf("upsert VM recommendations: %w", err)
	}

	metrics.IncRecommendationsWritten("vm", len(recs))
	log.Infof("vm recs: upserted %d recommendations", len(recs))

	// Write VM quality metrics.
	if oldVMRecs != nil {
		digestsByVM := make(map[vmQualityKey][]Digest)
		for _, d := range digests {
			key := vmQualityKey{Namespace: d.Namespace, VMName: d.VMName}
			digestsByVM[key] = append(digestsByVM[key], d)
		}
		qualityRows := BuildVMQualityRows(recs, oldVMRecs, digestsByVM)
		if qualErr := WriteVMQuality(ctx, pool, qualityRows); qualErr != nil {
			log.Warnf("vm recs: writing VM quality metrics failed: %v", qualErr)
		}
	}

	return nil
}
