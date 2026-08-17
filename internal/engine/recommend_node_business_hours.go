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

const kibPerGiB = 1024 * 1024

// EnrichNodeDetailWithBusinessHours attaches nested business_hours sizing on node
// detail engines. List payloads are not enriched. Failures are returned so the
// handler can warn without failing the all-hours detail.
func EnrichNodeDetailWithBusinessHours(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, nodeName string,
	detail *model.NodeUtilizationDetailRec,
) error {
	if !config.BusinessHoursFeatureEnabled() || pool == nil || detail == nil || nodeName == "" || clusterUUID == "" {
		return nil
	}

	cache, err := LoadSchedules(ctx, pool, orgID, clusterUUID)
	if err != nil {
		return fmt.Errorf("load business hours schedules: %w", err)
	}
	if cache == nil || !cache.ProducesNodeBusinessHoursDigests() {
		return nil
	}
	sched := cache.ResolveCluster()
	if !sched.Enabled {
		return nil
	}

	terms, err := LoadTermConfigCached(ctx, pool, orgID, "node")
	if err != nil {
		return fmt.Errorf("load term config for node business hours: %w", err)
	}
	nodeSettings, err := ResolveNodeThresholdSettings(ctx, pool, orgID)
	if err != nil {
		nodeSettings = DefaultNodeThresholdSettings()
	}
	cfg := NodeRecConfigFromThresholds(nodeSettings)
	windowDays := maxTermWindowDays(terms)

	start, end, err := nodeBHDigestWindow(ctx, pool, orgID, clusterUUID, nodeName, windowDays)
	if err != nil {
		return err
	}
	digests, err := QueryNodeDigestsForNodeBySchedule(ctx, pool, orgID, clusterUUID, nodeName, start, end, digestScheduleBusinessHours)
	if err != nil {
		return fmt.Errorf("query node business_hours digests: %w", err)
	}

	bhDayCount := uniqueNodeDigestDays(digests)
	recs := RecommendNodes(digests, cfg, nodeSettings, terms)
	attachNodeBusinessHoursToDetail(detail, recs, terms, bhDayCount)
	return nil
}

func nodeBHDigestWindow(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, nodeName string,
	windowDays int,
) (start, end time.Time, err error) {
	var maxDate time.Time
	err = pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(bucket_date), CURRENT_DATE)
		FROM daily_node_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND node = $3 AND schedule_type = $4`,
		orgID, clusterUUID, nodeName, digestScheduleBusinessHours).Scan(&maxDate)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("max node business_hours digest date: %w", err)
	}
	end = maxDate.Truncate(24 * time.Hour)
	if windowDays < 1 {
		windowDays = 1
	}
	start = end.AddDate(0, 0, -(windowDays - 1))
	return start, end, nil
}

func uniqueNodeDigestDays(digests []NodeDigestRow) int {
	seen := make(map[string]struct{}, len(digests))
	for _, d := range digests {
		seen[d.BucketDate.Format("2006-01-02")] = struct{}{}
	}
	return len(seen)
}

func attachNodeBusinessHoursToDetail(detail *model.NodeUtilizationDetailRec, recs []NodeRec, terms []TermConfig, bhDayCount int) {
	if detail == nil {
		return
	}
	byTermEngine := make(map[string]NodeRec, len(recs))
	for _, rec := range recs {
		byTermEngine[rec.Term+"|"+rec.Engine] = rec
	}
	for _, tc := range terms {
		termKey := model.NodeUtilTermAPIKey(tc.Name)
		termRec, ok := detail.RecommendationTerms[termKey]
		if !ok || termRec.RecommendationEngines == nil {
			continue
		}
		attachNodeBHEngine(termRec.RecommendationEngines.Cost, tc, "cost", byTermEngine, bhDayCount)
		attachNodeBHEngine(termRec.RecommendationEngines.Performance, tc, "performance", byTermEngine, bhDayCount)
		detail.RecommendationTerms[termKey] = termRec
	}
}

func attachNodeBHEngine(eng *model.NodeUtilizationEngineRec, tc TermConfig, engineName string, byTermEngine map[string]NodeRec, bhDayCount int) {
	if eng == nil {
		return
	}
	rec, ok := byTermEngine[tc.Name+"|"+engineName]
	if !ok || bhDayCount < tc.MinDataDays {
		eng.BusinessHours = &model.NodeBHRecommendation{
			Reason: insufficientBusinessHoursReason(bhDayCount, tc.MinDataDays),
		}
		return
	}
	cpu := float32(float64(rec.RecommendedCPUMC) / 1000.0)
	mem := float32(float64(rec.RecommendedMemKiB) / float64(kibPerGiB))
	eng.BusinessHours = &model.NodeBHRecommendation{
		RecommendedCPUCores:  &cpu,
		RecommendedMemoryGiB: &mem,
		Notifications:        notifications.MapToKruizeFormat([]int16{NotifNodeBHNotPeakSafe}),
	}
}
