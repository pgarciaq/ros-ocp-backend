package api

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

const gpuTermConfigCacheTTL = 60 * time.Second

type gpuTermConfigCacheEntry struct {
	terms []engine.TermConfig
	until time.Time
}

var (
	gpuTermConfigMu    sync.RWMutex
	gpuTermConfigByOrg = map[string]gpuTermConfigCacheEntry{}
)

// loadTermConfigCached returns term configuration for GPU enrichment with a
// short TTL to avoid hitting org_recommendation_terms on every request.
func loadTermConfigCached(ctx context.Context, pool *pgxpool.Pool, orgID string) ([]engine.TermConfig, error) {
	if pool == nil {
		return engine.DefaultTerms(), nil
	}
	now := time.Now().UTC()
	gpuTermConfigMu.RLock()
	e, ok := gpuTermConfigByOrg[orgID]
	gpuTermConfigMu.RUnlock()
	if ok && now.Before(e.until) {
		return e.terms, nil
	}

	terms, err := engine.LoadTermConfig(ctx, pool, orgID)
	if err != nil {
		return nil, err
	}
	gpuTermConfigMu.Lock()
	gpuTermConfigByOrg[orgID] = gpuTermConfigCacheEntry{terms: terms, until: now.Add(gpuTermConfigCacheTTL)}
	gpuTermConfigMu.Unlock()
	return terms, nil
}

// enrichWithGPU queries gpu_container_digests and attaches GPU recommendations
// to each NativeContainerResult that has GPU data. Also fetches GPU cost rates
// from Koku to compute savings estimates. Modifies results in-place.
func enrichWithGPU(ctx context.Context, results []model.NativeContainerResult, orgID string) {
	if len(results) == 0 {
		return
	}

	pool := database.GetPool()
	if pool == nil {
		return
	}

	ctx, cancel := database.ContextWithAcquireTimeout(ctx)
	defer cancel()
	now := time.Now().UTC()

	terms, err := loadTermConfigCached(ctx, pool, orgID)
	if err != nil {
		log.Warnf("enrichWithGPU: load term config failed: %v", err)
		terms = engine.DefaultTerms()
	}
	start := now.AddDate(0, 0, -engine.MaxWindowDays(terms, 30))

	clusterMap := map[string][]int{}
	for i, r := range results {
		clusterMap[r.ClusterUUID] = append(clusterMap[r.ClusterUUID], i)
	}

	costProvider := getGPUCostProvider()

	for clusterUUID, indices := range clusterMap {
		gpuRecs, nodeMap, nodeLastSeen, err := engine.QueryGPURecommendations(ctx, pool, clusterUUID, start, now, terms, nil)
		if err != nil {
			log.Warnf("enrichWithGPU: failed for cluster %s: %v", clusterUUID, err)
			continue
		}
		if gpuRecs == nil {
			continue
		}

		var costData *costdata.ClusterCostData
		if costProvider != nil && orgID != "" {
			kokuOrgID := strings.TrimPrefix(orgID, "org")
			cd, err := costProvider.GetEffectiveRates(ctx, kokuOrgID, clusterUUID, start, now)
			if err != nil {
				log.Warnf("enrichWithGPU: cost data fetch failed for cluster %s: %v", clusterUUID, err)
			} else {
				costData = cd
			}
		}

		var gpuRate *float32
		if costData != nil {
			if rate := engine.GPUMonthlyRate(costData); rate > 0 {
				r := float32(rate)
				gpuRate = &r
			}
		}

		for _, recs := range gpuRecs {
			for _, gpuRec := range recs {
				engine.ApplyGPUSavings(gpuRec, costData)
			}
		}

		groups := groupByNodeAndModel(gpuRecs, nodeMap, nodeLastSeen, clusterUUID)
		for _, group := range groups {
			engine.ComputeNodeTimeslicingRec(group, gpuRate, now)
		}

		for _, idx := range indices {
			r := &results[idx]
			key := fmt.Sprintf("%s/%s/%s", r.Project, r.Workload, r.Container)
			recs, ok := gpuRecs[key]
			if !ok || len(recs) == 0 {
				continue
			}
			gpuMap := make(map[string]*model.GPURecommendation, len(recs))
			for _, rec := range recs {
				gpuMap[rec.Term] = toGPURecommendation(rec)
			}
			r.GPU = gpuMap
		}
	}
}

func getGPUCostProvider() costdata.CostDataProvider {
	cfg := config.GetConfig()
	if cfg.KokuMasuURL == "" {
		return nil
	}
	timeout := time.Duration(cfg.GlobalHTTPClientTimeoutSecs) * time.Second
	return costdata.NewHTTPCostDataProvider(cfg.KokuMasuURL, timeout)
}

// filterGPUResults applies GPU-specific filters (has_gpu, gpu_model, gpu_classification)
// to the result set. Returns a filtered copy; count is adjusted accordingly.
func filterGPUResults(results []model.NativeContainerResult, hasGPU *bool, gpuModels, gpuClassifications []string) ([]model.NativeContainerResult, int) {
	if hasGPU == nil && len(gpuModels) == 0 && len(gpuClassifications) == 0 {
		return results, len(results)
	}

	filtered := make([]model.NativeContainerResult, 0, len(results))
	for _, r := range results {
		hasGPUField := len(r.GPU) > 0
		if hasGPU != nil {
			if *hasGPU != hasGPUField {
				continue
			}
		}
		if len(gpuModels) > 0 {
			if !hasGPUField {
				continue
			}
			if !anyGPUTermMatches(r.GPU, func(g *model.GPURecommendation) bool {
				return matchesAny(g.CurrentGPUModel, gpuModels)
			}) {
				continue
			}
		}
		if len(gpuClassifications) > 0 {
			if !hasGPUField {
				continue
			}
			if !anyGPUTermMatches(r.GPU, func(g *model.GPURecommendation) bool {
				return matchesAny(g.GPUClassification, gpuClassifications)
			}) {
				continue
			}
		}
		filtered = append(filtered, r)
	}
	return filtered, len(filtered)
}

func anyGPUTermMatches(gpuMap map[string]*model.GPURecommendation, predicate func(*model.GPURecommendation) bool) bool {
	for _, g := range gpuMap {
		if predicate(g) {
			return true
		}
	}
	return false
}

func matchesAny(value string, candidates []string) bool {
	lower := strings.ToLower(value)
	for _, c := range candidates {
		if strings.Contains(lower, strings.ToLower(c)) {
			return true
		}
	}
	return false
}

func toGPURecommendation(rec *engine.GPURec) *model.GPURecommendation {
	var profile *string
	if rec.CurrentGPUProfile != "" {
		p := rec.CurrentGPUProfile
		profile = &p
	}
	var recProfile *string
	if rec.RecommendedGPUProfile != "" {
		p := rec.RecommendedGPUProfile
		recProfile = &p
	}

	result := &model.GPURecommendation{
		CurrentGPUModel:                       rec.GPUModelName,
		CurrentGPUProfile:                     profile,
		GPUClassification:                     string(rec.Classification),
		RecommendedGPUProfile:                 recProfile,
		MemoryBoundDetected:                   rec.MemoryBoundDetected,
		GPUConfidence:                         rec.Confidence,
		TensorPipeActiveAvg:                   rec.TensorPipeActiveAvg,
		DRAMActiveAvg:                         rec.DRAMActiveAvg,
		SMActiveAvg:                           rec.SMActiveAvg,
		FBUsageMaxMiB:                         rec.FBUsageMaxMiB,
		EstimatedMonthlyGPUSavingsUSD:         rec.EstimatedGPUSavingsUSD,
		EstimatedMonthlyTimeslicingSavingsUSD: rec.EstimatedTimeslicingSavingsUSD,
		Notifications:                         rec.NotificationCodes,
	}
	if rec.TimeSlicingNode != "" {
		n := rec.TimeSlicingNode
		result.TimeSlicingNode = &n
	}
	if rec.TimeSlicingReplicas > 0 {
		r := rec.TimeSlicingReplicas
		result.TimeSlicingReplicas = &r
	}
	return result
}
