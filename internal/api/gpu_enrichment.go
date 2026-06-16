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
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
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
		pageKeys := make([]engine.PageGPUKey, len(indices))
		for i, idx := range indices {
			r := results[idx]
			pageKeys[i] = engine.PageGPUKey{
				ClusterUUID:   r.ClusterUUID,
				Namespace:     r.Project,
				Workload:      r.Workload,
				ContainerName: r.Container,
			}
		}
		gpuRecs, nodeMap, nodeLastSeen, err := engine.QueryGPURecommendationsForContainers(ctx, pool, orgID, clusterUUID, pageKeys, start, now, terms, nil)
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

		persistedSavings, loadErr := engine.LoadPersistedGPUSavings(ctx, pool, orgID, clusterUUID)
		if loadErr != nil {
			log.Warnf("enrichWithGPU: load persisted GPU savings failed for cluster %s: %v", clusterUUID, loadErr)
		}
		for key, recs := range gpuRecs {
			parts := strings.SplitN(key, "/", 3)
			if len(parts) != 3 {
				continue
			}
			ns, wl, cn := parts[0], parts[1], parts[2]
			for _, gpuRec := range recs {
				lookup := engine.GPUSavingsLookupKey(ns, wl, cn, gpuRec.Term)
				if cents, ok := persistedSavings[lookup]; ok && cents != nil {
					gpuRec.EstimatedGPUSavingsCents = cents
				} else {
					engine.ApplyGPUSavings(gpuRec, costData)
				}
			}
		}

		groups := engine.GroupGPURecsByNodeAndModel(gpuRecs, nodeMap, nodeLastSeen, clusterUUID)
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
				if rec.GPUIdleState != engine.IdleStateActive && costData != nil {
					if rate := engine.GPUMonthlyRate(costData); rate > 0 {
						rec.GPUEstimatedWasteCents = money.USDToCents(rate)
					}
				}
				gpuRec := toGPURecommendation(rec, blockCurrency)
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

// ResetGPUCostProviderForTest clears the process-wide HTTP cost provider singleton.
// Tests that mock KokuMasuURL must call this in cleanup because t.Setenv is
// process-global and must not be used from parallel tests.
func ResetGPUCostProviderForTest() {
	gpuCostProviderMu.Lock()
	defer gpuCostProviderMu.Unlock()
	gpuCostProvider = nil
	gpuCostProviderBase = ""
}

func toGPURecommendation(rec *engine.GPURec, currency string) *model.GPURecommendation {
	if currency == "" {
		currency = money.DefaultCurrency
	}
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
		EstimatedMonthlyGPUSavings:         money.FormatCentsToAmountPtr(rec.EstimatedGPUSavingsCents, currency),
		EstimatedMonthlyTimeslicingSavings: money.FormatUSDPtrToAmountPtr(rec.EstimatedTimeslicingSavingsUSD, currency),
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
	if rec.GPUIdleState != "" {
		result.GPUIdleState = string(rec.GPUIdleState)
	} else {
		result.GPUIdleState = "active"
	}
	if rec.GPUIdleSince != nil {
		s := rec.GPUIdleSince.UTC().Format("2006-01-02")
		result.GPUIdleSince = &s
	}
	if rec.GPUIdleDurationDays > 0 {
		d := rec.GPUIdleDurationDays
		result.GPUIdleDurationDays = &d
	}
	if rec.GPUEstimatedWasteCents > 0 {
		cents := rec.GPUEstimatedWasteCents
		result.EstimatedMonthlyGPUWaste = money.FormatCentsToAmountPtr(&cents, currency)
	}
	return result
}
