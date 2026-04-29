package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// enrichWithGPU queries gpu_container_digests and attaches GPU recommendations
// to each NativeContainerResult that has GPU data. Also fetches GPU cost rates
// from Koku to compute savings estimates. Modifies results in-place.
func enrichWithGPU(results []model.NativeContainerResult, orgID string) {
	if len(results) == 0 {
		return
	}

	pool := database.GetPool()
	if pool == nil {
		return
	}

	ctx := context.Background()
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -30)

	clusterMap := map[string][]int{}
	for i, r := range results {
		clusterMap[r.ClusterUUID] = append(clusterMap[r.ClusterUUID], i)
	}

	costProvider := getGPUCostProvider()

	for clusterUUID, indices := range clusterMap {
		gpuRecs, nodeMap, nodeLastSeen, err := engine.QueryGPURecommendations(ctx, pool, clusterUUID, start, now)
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

		for _, gpuRec := range gpuRecs {
			engine.ApplyGPUSavings(gpuRec, costData)
		}

		groups := groupByNodeAndModel(gpuRecs, nodeMap, nodeLastSeen, clusterUUID)
		for _, group := range groups {
			engine.AnnotateTimeslicingCandidates(group)
		}

		for _, idx := range indices {
			r := &results[idx]
			key := fmt.Sprintf("%s/%s/%s", r.Project, r.Workload, r.Container)
			gpuRec, ok := gpuRecs[key]
			if !ok || gpuRec == nil {
				continue
			}
			r.GPU = toGPURecommendation(gpuRec)
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
		if hasGPU != nil {
			hasGPUField := r.GPU != nil
			if *hasGPU != hasGPUField {
				continue
			}
		}
		if len(gpuModels) > 0 {
			if r.GPU == nil {
				continue
			}
			if !matchesAny(r.GPU.CurrentGPUModel, gpuModels) {
				continue
			}
		}
		if len(gpuClassifications) > 0 {
			if r.GPU == nil {
				continue
			}
			if !matchesAny(r.GPU.GPUClassification, gpuClassifications) {
				continue
			}
		}
		filtered = append(filtered, r)
	}
	return filtered, len(filtered)
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
		CurrentGPUModel:               rec.GPUModelName,
		CurrentGPUProfile:             profile,
		GPUClassification:             string(rec.Classification),
		RecommendedGPUProfile:         recProfile,
		MemoryBoundDetected:           rec.MemoryBoundDetected,
		GPUConfidence:                 rec.Confidence,
		TensorPipeActiveAvg:           rec.TensorPipeActiveAvg,
		DRAMActiveAvg:                 rec.DRAMActiveAvg,
		SMActiveAvg:                   rec.SMActiveAvg,
		FBUsageMaxMiB:                 rec.FBUsageMaxMiB,
		EstimatedMonthlyGPUSavingsUSD: rec.EstimatedGPUSavingsUSD,
		Notifications:                 rec.NotificationCodes,
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
