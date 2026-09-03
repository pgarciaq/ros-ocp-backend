package engine

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// EnrichNodeGPUTimeslicingDetailWithBusinessHours attaches nested business_hours
// time-slicing on GET .../gpu/timeslicing/{node} rows only. List, history, and
// summary stay all-hours. Failures are returned so the handler can warn without
// failing the all-hours detail.
func EnrichNodeGPUTimeslicingDetailWithBusinessHours(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, nodeName string,
	recs []model.NodeGPURecommendation,
) error {
	if !config.BusinessHoursFeatureEnabled() || pool == nil || nodeName == "" || len(recs) == 0 {
		return nil
	}

	byCluster := make(map[string][]int, 2)
	for i, rec := range recs {
		if rec.ClusterUUID == "" {
			continue
		}
		byCluster[rec.ClusterUUID] = append(byCluster[rec.ClusterUUID], i)
	}
	for clusterUUID, idxs := range byCluster {
		if err := enrichTimeslicingDetailCluster(ctx, pool, orgID, clusterUUID, nodeName, recs, idxs); err != nil {
			return err
		}
	}
	return nil
}

func enrichTimeslicingDetailCluster(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, nodeName string,
	recs []model.NodeGPURecommendation,
	idxs []int,
) error {
	cache, err := LoadSchedules(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return fmt.Errorf("load business hours schedules: %w", err)
	}
	if cache == nil || !cache.ProducesNodeBusinessHoursDigests() {
		return nil
	}
	clusterSched := cache.ResolveCluster()
	if !clusterSched.Enabled {
		return nil
	}

	terms, err := LoadTermConfigCached(ctx, pool, orgID, "gpu")
	if err != nil {
		return fmt.Errorf("load term config for GPU timeslicing business hours: %w", err)
	}
	settings, err := ResolveGPUThresholdSettings(ctx, pool, orgID)
	if err != nil {
		settings = DefaultGPUThresholdSettings()
	}
	windowDays := MaxWindowDays(terms, 30)
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -windowDays)

	nodeFilter := &GPUQueryFilters{NodeNameExact: nodeName}
	allHoursRecs, allHoursNodeMap, allHoursLastSeen, err := QueryGPURecommendations(
		ctx, pool, orgID, clusterUUID, start, now, terms, nodeFilter,
	)
	if err != nil {
		return fmt.Errorf("query all_hours GPU recommendations for timeslicing BH: %w", err)
	}
	allHoursGroups := GroupGPURecsByNodeAndModel(allHoursRecs, allHoursNodeMap, allHoursLastSeen, clusterUUID)
	heteroModels := timeslicingHeterogeneousModels(allHoursGroups, nodeName, cache, clusterSched)

	bhFilter := &GPUQueryFilters{NodeNameExact: nodeName, ScheduleType: gpuDigestScheduleBusinessHours}
	bhRecs, bhNodeMap, bhLastSeen, err := QueryGPURecommendations(
		ctx, pool, orgID, clusterUUID, start, now, terms, bhFilter,
	)
	if err != nil {
		return fmt.Errorf("query business_hours GPU recommendations for timeslicing BH: %w", err)
	}
	bhGroups := GroupGPURecsByNodeAndModel(bhRecs, bhNodeMap, bhLastSeen, clusterUUID)
	bhByModelTerm := indexTimeslicingGroups(bhGroups, nodeName)

	dayCounts, err := countGPUTimeslicingBusinessHoursDigestDays(ctx, pool, orgID, clusterUUID, nodeName, start, now)
	if err != nil {
		return err
	}

	for _, i := range idxs {
		rec := &recs[i]
		if rec.GPUModel == "" {
			continue
		}
		if heteroModels[rec.GPUModel] {
			continue
		}
		tc := gpuTermConfigByName(terms, rec.Term)
		bhDayCount := dayCounts[rec.GPUModel]
		bhGroup, ok := bhByModelTerm[timeslicingGroupKey(rec.GPUModel, rec.Term)]
		var tsRec *TimeslicingRec
		if ok {
			tsRec = ComputeNodeTimeslicingRecWithSettings(bhGroup, nil, now, settings)
		}
		attachTimeslicingBusinessHours(rec, tsRec, tc, bhDayCount, bhGroup)
	}
	return nil
}

func timeslicingHeterogeneousModels(
	groups []NodeGPUGroup,
	nodeName string,
	cache *ScheduleCache,
	clusterSched bhschedule.Schedule,
) map[string]bool {
	hetero := make(map[string]bool)
	for _, g := range groups {
		if g.NodeName != nodeName || hetero[g.GPUModel] {
			continue
		}
		if !timeslicingGroupMatchesClusterWindow(g, cache, clusterSched) {
			hetero[g.GPUModel] = true
		}
	}
	return hetero
}

func timeslicingGroupMatchesClusterWindow(group NodeGPUGroup, cache *ScheduleCache, clusterSched bhschedule.Schedule) bool {
	for _, c := range group.Containers {
		if !schedulesWindowEqual(cache.Resolve(c.Namespace), clusterSched) {
			return false
		}
	}
	return true
}

func schedulesWindowEqual(a, b bhschedule.Schedule) bool {
	if a.Enabled != b.Enabled ||
		a.Timezone != b.Timezone ||
		a.StartTime != b.StartTime ||
		a.EndTime != b.EndTime ||
		a.OffHoursWeight != b.OffHoursWeight {
		return false
	}
	da := append([]string(nil), a.Days...)
	db := append([]string(nil), b.Days...)
	slices.Sort(da)
	slices.Sort(db)
	return slices.Equal(da, db)
}

func indexTimeslicingGroups(groups []NodeGPUGroup, nodeName string) map[string]NodeGPUGroup {
	out := make(map[string]NodeGPUGroup, len(groups))
	for _, g := range groups {
		if g.NodeName != nodeName {
			continue
		}
		out[timeslicingGroupKey(g.GPUModel, g.Term)] = g
	}
	return out
}

func timeslicingGroupKey(gpuModel, term string) string {
	return gpuModel + "|" + term
}

func countGPUTimeslicingBusinessHoursDigestDays(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, nodeName string,
	start, end time.Time,
) (map[string]int, error) {
	rows, err := pool.Query(ctx, `
		SELECT gpu_model_name, COUNT(DISTINCT interval_start::date)
		FROM gpu_container_digests
		WHERE org_id = $1
		  AND cluster_uuid = $2::uuid
		  AND node_name = $3
		  AND schedule_type = $4
		  AND interval_start >= $5 AND interval_start <= $6
		GROUP BY gpu_model_name`,
		orgID, clusterUUID, nodeName, gpuDigestScheduleBusinessHours,
		start.UTC().Format("2006-01-02"), end.UTC().Format("2006-01-02"),
	)
	if err != nil {
		return nil, fmt.Errorf("count GPU timeslicing business_hours digest days: %w", err)
	}
	defer rows.Close()

	out := make(map[string]int)
	for rows.Next() {
		var modelName string
		var n int
		if err := rows.Scan(&modelName, &n); err != nil {
			return nil, fmt.Errorf("scan GPU timeslicing business_hours digest days: %w", err)
		}
		out[modelName] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate GPU timeslicing business_hours digest days: %w", err)
	}
	return out, nil
}

func attachTimeslicingBusinessHours(rec *model.NodeGPURecommendation, tsRec *TimeslicingRec, tc TermConfig, bhDayCount int, bhGroup NodeGPUGroup) {
	if rec == nil {
		return
	}
	if tsRec == nil || bhDayCount < tc.MinDataDays {
		rec.BusinessHours = &model.TimeslicingBHRecommendation{
			Reason: insufficientBusinessHoursReason(bhDayCount, tc.MinDataDays),
		}
		return
	}
	replicas := tsRec.RecommendedReplicas
	conf := tsRec.Confidence
	cand := len(tsRec.CandidateContainers)
	imp := len(tsRec.ImpactedContainers)
	sm, dram, tensor, fb := timeslicingBHUtilization(bhGroup, tsRec.CandidateContainers)
	bh := &model.TimeslicingBHRecommendation{
		RecommendedReplicas: &replicas,
		Confidence:          &conf,
		CandidateCount:      &cand,
		ImpactedCount:       &imp,
		SMActiveAvg:         sm,
		DRAMActiveAvg:       dram,
		TensorPipeActiveAvg: tensor,
		FBUsageMaxMiB:       fb,
		Notifications:       notifications.MapToKruizeFormat([]int16{NotifGPUTSBHClusterWindow}),
	}
	if spec := MatchGPUModel(firstNonEmpty(bhGroup.GPUModel, rec.GPUModel)); spec != nil && spec.TotalFBMiB > 0 {
		total := int64(spec.TotalFBMiB)
		bh.TotalFBMiB = &total
	}
	rec.BusinessHours = bh
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func timeslicingBHUtilization(group NodeGPUGroup, candidates []GPUContainerRef) (sm, dram, tensor, fb float32) {
	type key struct{ ns, wl, c string }
	want := make(map[key]struct{}, len(candidates))
	for _, c := range candidates {
		want[key{c.Namespace, c.Workload, c.Container}] = struct{}{}
	}
	var n float32
	for _, c := range group.Containers {
		if c.Rec == nil {
			continue
		}
		if len(want) > 0 {
			if _, ok := want[key{c.Namespace, c.Workload, c.Container}]; !ok {
				continue
			}
		}
		sm += c.Rec.SMActiveAvg
		dram += c.Rec.DRAMActiveAvg
		tensor += c.Rec.TensorPipeActiveAvg
		fb += c.Rec.FBUsageMaxMiB
		n++
	}
	if n == 0 {
		return 0, 0, 0, 0
	}
	return sm / n, dram / n, tensor / n, fb / n
}
