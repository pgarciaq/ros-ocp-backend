package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

const gpuDigestScheduleBusinessHours = "business_hours"

// EnrichContainerDetailGPUWithBusinessHours attaches nested business_hours GPU
// sizing on container detail gpu.{term} only. List payloads are not enriched.
// Failures are returned so the handler can warn without failing all-hours detail.
func EnrichContainerDetailGPUWithBusinessHours(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID string,
	result *model.NativeContainerResult,
) error {
	if !config.BusinessHoursFeatureEnabled() || pool == nil || result == nil || len(result.GPU) == 0 {
		return nil
	}
	if result.ClusterUUID == "" || result.Project == "" {
		return nil
	}

	cache, err := LoadSchedules(ctx, pool, orgID, result.ClusterUUID)
	if err != nil {
		return fmt.Errorf("load business hours schedules: %w", err)
	}
	if cache == nil || !cache.ProducesBusinessHoursDigests() {
		return nil
	}
	sched := cache.Resolve(result.Project)
	if !sched.Enabled {
		return nil
	}

	terms, err := LoadTermConfigCached(ctx, pool, orgID, "gpu")
	if err != nil {
		return fmt.Errorf("load term config for GPU business hours: %w", err)
	}
	windowDays := MaxWindowDays(terms, 30)
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -windowDays)

	pageKeys := []PageGPUKey{{
		ClusterUUID:   result.ClusterUUID,
		Namespace:     result.Project,
		Workload:      result.Workload,
		ContainerName: result.Container,
	}}
	gpuRecs, _, _, err := QueryGPURecommendationsForContainers(
		ctx, pool, orgID, result.ClusterUUID, pageKeys, start, now, terms,
		&GPUQueryFilters{ScheduleType: gpuDigestScheduleBusinessHours},
	)
	if err != nil {
		return fmt.Errorf("query GPU business_hours recommendations: %w", err)
	}

	bhDayCount, err := countGPUBusinessHoursDigestDays(ctx, pool, result.ClusterUUID, result.Project, result.Workload, result.Container, start, now)
	if err != nil {
		return err
	}

	var recs []*GPURec
	if gpuRecs != nil {
		recs = gpuRecs[GPUContainerKey{
			Namespace:     result.Project,
			Workload:      result.Workload,
			ContainerName: result.Container,
		}]
	}
	attachGPUBusinessHoursToDetail(result.GPU, recs, terms, bhDayCount)
	return nil
}

func countGPUBusinessHoursDigestDays(
	ctx context.Context,
	pool *pgxpool.Pool,
	clusterUUID, namespace, workload, container string,
	start, end time.Time,
) (int, error) {
	var n int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT interval_start::date)
		FROM gpu_container_digests
		WHERE cluster_uuid = $1::uuid
		  AND namespace = $2 AND workload = $3 AND container_name = $4
		  AND schedule_type = $5
		  AND interval_start >= $6 AND interval_start <= $7`,
		clusterUUID, namespace, workload, container, gpuDigestScheduleBusinessHours,
		start.UTC().Format("2006-01-02"), end.UTC().Format("2006-01-02"),
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count GPU business_hours digest days: %w", err)
	}
	return n, nil
}

func attachGPUBusinessHoursToDetail(gpuMap map[string]*model.GPURecommendation, recs []*GPURec, terms []TermConfig, bhDayCount int) {
	if gpuMap == nil {
		return
	}
	byTerm := make(map[string]*GPURec, len(recs))
	for _, rec := range recs {
		if rec != nil {
			byTerm[rec.Term] = rec
		}
	}
	for termKey, parent := range gpuMap {
		if parent == nil {
			continue
		}
		tc := gpuTermConfigByName(terms, termKey)
		rec := byTerm[termKey]
		if rec == nil || bhDayCount < tc.MinDataDays {
			parent.BusinessHours = &model.GPUBHRecommendation{
				Reason: insufficientBusinessHoursReason(bhDayCount, tc.MinDataDays),
			}
			continue
		}
		parent.BusinessHours = gpuRecToBHRecommendation(rec)
	}
}

func gpuTermConfigByName(terms []TermConfig, name string) TermConfig {
	for _, tc := range terms {
		if tc.Name == name {
			return tc
		}
	}
	return TermConfig{Name: name, MinDataDays: 1}
}

func gpuRecToBHRecommendation(rec *GPURec) *model.GPUBHRecommendation {
	var recProfile, curProfile *string
	if rec.RecommendedGPUProfile != "" {
		p := rec.RecommendedGPUProfile
		recProfile = &p
	}
	if rec.CurrentGPUProfile != "" {
		p := rec.CurrentGPUProfile
		curProfile = &p
	}
	idle := string(rec.GPUIdleState)
	return &model.GPUBHRecommendation{
		CurrentGPUModel:       rec.GPUModelName,
		CurrentGPUProfile:     curProfile,
		GPUClassification:     string(rec.Classification),
		RecommendedGPUProfile: recProfile,
		MemoryBoundDetected:   rec.MemoryBoundDetected,
		GPUConfidence:         rec.Confidence,
		TensorPipeActiveAvg:   rec.TensorPipeActiveAvg,
		DRAMActiveAvg:         rec.DRAMActiveAvg,
		SMActiveAvg:           rec.SMActiveAvg,
		FBUsageMaxMiB:         rec.FBUsageMaxMiB,
		GPUIdleState:          idle,
		Notifications:         notifications.MapToKruizeFormat([]int16{NotifGPUBHOfficeWindow}),
	}
}
