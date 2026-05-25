package api

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

var (
	gpuCostProvider     costdata.CostDataProvider
	gpuCostProviderMu   sync.RWMutex
	gpuCostProviderBase string
)

// EnrichNativeContainerResultsWithGPU attaches GPU utilization and savings data (APIEnricher delegate).
func EnrichNativeContainerResultsWithGPU(ctx context.Context, orgID string, results []model.NativeContainerResult) {
	enrichWithGPU(ctx, results, orgID)
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

	terms, err := engine.LoadTermConfigCached(ctx, pool, orgID, "gpu")
	if err != nil {
		log.Warnf("enrichWithGPU: load term config failed: %v", err)
		terms = engine.DefaultTermsForPlugin("gpu")
	}
	start := now.AddDate(0, 0, -engine.MaxWindowDays(terms, 30))

	clusterMap := map[string][]int{}
	for i, r := range results {
		clusterMap[r.ClusterUUID] = append(clusterMap[r.ClusterUUID], i)
	}

	costProvider := getGPUCostProvider()

	for clusterUUID, indices := range clusterMap {
		gpuRecs, nodeMap, nodeLastSeen, err := engine.QueryGPURecommendations(ctx, pool, orgID, clusterUUID, start, now, terms, nil)
		if err != nil {
			log.Warnf("enrichWithGPU: failed for cluster %s: %v", clusterUUID, err)
			continue
		}
		if gpuRecs == nil {
			continue
		}

		var costData *costdata.ClusterCostData
		if costProvider != nil && orgID != "" {
			costData = GetCachedCostRates(ctx, orgID, clusterUUID, start, now)
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
			engine.ComputeNodeTimeslicingRecForOrg(ctx, pool, orgID, group, gpuRate, now)
		}

		for _, idx := range indices {
			r := &results[idx]
			key := r.Project + "/" + r.Workload + "/" + r.Container
			recs, ok := gpuRecs[key]
			if !ok || len(recs) == 0 {
				continue
			}
			blockCurrency := costdata.DefaultCurrency
			if costData != nil {
				blockCurrency = costdata.ResolveCurrency(costData)
			}
			gpuMap := make(map[string]*model.GPURecommendation, len(recs))
			for _, rec := range recs {
				gpuRec := toGPURecommendation(rec)
				gpuRec.Currency = blockCurrency
				gpuMap[rec.Term] = gpuRec
			}
			r.GPU = gpuMap
		}
	}
}

func getGPUCostProvider() costdata.CostDataProvider {
	cfg := config.GetConfig()
	if !cfg.SavingsEstimatesEnabled || cfg.KokuMasuURL == "" {
		return nil
	}

	gpuCostProviderMu.RLock()
	if gpuCostProvider != nil && gpuCostProviderBase == cfg.KokuMasuURL {
		p := gpuCostProvider
		gpuCostProviderMu.RUnlock()
		return p
	}
	gpuCostProviderMu.RUnlock()

	gpuCostProviderMu.Lock()
	defer gpuCostProviderMu.Unlock()
	if gpuCostProvider != nil && gpuCostProviderBase == cfg.KokuMasuURL {
		return gpuCostProvider
	}
	timeout := time.Duration(cfg.GlobalHTTPClientTimeoutSecs) * time.Second
	gpuCostProvider = costdata.NewHTTPCostDataProvider(cfg.KokuMasuURL, timeout)
	gpuCostProviderBase = cfg.KokuMasuURL
	return gpuCostProvider
}

// filterGPUResults is a no-op: all GPU filters (has_gpu, gpu_model,
// gpu_classification) are now pushed to SQL via MapNativeQueryParameters
// for correct pagination and total counts.
func filterGPUResults(results []model.NativeContainerResult, totalCount int, _, _ []string) ([]model.NativeContainerResult, int) {
	return results, totalCount
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
