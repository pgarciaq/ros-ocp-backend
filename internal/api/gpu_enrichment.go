package api

import (
	"context"
	"fmt"
	"time"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// enrichWithGPU queries gpu_container_digests and attaches GPU recommendations
// to each NativeContainerResult that has GPU data. Modifies results in-place.
func enrichWithGPU(results []model.NativeContainerResult) {
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

	for clusterUUID, indices := range clusterMap {
		gpuRecs, err := engine.QueryGPURecommendations(ctx, pool, clusterUUID, start, now)
		if err != nil {
			log.Warnf("enrichWithGPU: failed for cluster %s: %v", clusterUUID, err)
			continue
		}
		if gpuRecs == nil {
			continue
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

	return &model.GPURecommendation{
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
}
