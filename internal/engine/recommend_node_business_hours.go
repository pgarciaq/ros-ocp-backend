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
	return enrichNodeDetailWithBusinessHours(ctx, pool, orgID, clusterUUID, nodeName, detail, nil, false)
}

// EnrichNodeDetailWithBusinessHoursFromDigests applies BH sizing from preloaded
// business_hours digest rows. Does not query daily_node_digests. Rows should
// already be sliced to the enrich window (MAX-based max term length).
func EnrichNodeDetailWithBusinessHoursFromDigests(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, nodeName string,
	detail *model.NodeUtilizationDetailRec,
	digests []NodeDigestRow,
) error {
	return enrichNodeDetailWithBusinessHours(ctx, pool, orgID, clusterUUID, nodeName, detail, digests, true)
}

func enrichNodeDetailWithBusinessHours(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, nodeName string,
	detail *model.NodeUtilizationDetailRec,
	preloaded []NodeDigestRow,
	usePreloaded bool,
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

	digests := preloaded
	if !usePreloaded {
		digests, _, _, err = QueryNodeBHDetailDigests(ctx, pool, orgID, clusterUUID, nodeName, time.Time{}, time.Time{})
		if err != nil {
			return err
		}
	}

	bhDayCount := uniqueNodeDigestDays(digests)
	recs := RecommendNodes(digests, cfg, nodeSettings, terms)
	attachNodeBusinessHoursToDetail(detail, recs, terms, bhDayCount)
	return nil
}

// QueryNodeBHDetailDigests loads one node's business_hours digest rows in a
// single statement. The enrich window is MAX(bucket_date) minus (windowDays-1).
// When coverStart/coverEnd are set, the fetch expands to also cover that
// inclusive range (Visual Insights chart window) so the handler can slice in
// memory instead of issuing a second round-trip.
func QueryNodeBHDetailDigests(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, nodeName string,
	coverStart, coverEnd time.Time,
) (rows []NodeDigestRow, enrichStart, enrichEnd time.Time, err error) {
	if pool == nil {
		return nil, time.Time{}, time.Time{}, fmt.Errorf("query node BH detail digests: pool is nil")
	}
	terms, termErr := LoadTermConfigCached(ctx, pool, orgID, "node")
	windowDays := 30
	if termErr == nil {
		windowDays = maxTermWindowDays(terms)
	}
	if windowDays < 1 {
		windowDays = 1
	}

	var coverStartArg, coverEndArg any
	if !coverStart.IsZero() {
		coverStartArg = coverStart.Format("2006-01-02")
	}
	if !coverEnd.IsZero() {
		coverEndArg = coverEnd.Format("2006-01-02")
	}

	qrows, err := pool.Query(ctx, `
		WITH bounds AS (
			SELECT COALESCE(MAX(bucket_date), CURRENT_DATE::date) AS max_date
			FROM daily_node_digests
			WHERE org_id = $1 AND cluster_uuid = $2 AND node = $3 AND schedule_type = $4
		)
		SELECT d.bucket_date, d.node,
			COALESCE(d.cpu_usage_p50_mc, 0), COALESCE(d.cpu_usage_p95_mc, 0), COALESCE(d.cpu_usage_max_mc, 0),
			COALESCE(d.mem_usage_p50_kib, 0), COALESCE(d.mem_usage_p95_kib, 0), COALESCE(d.mem_usage_max_kib, 0),
			d.max_cpu_allocatable_mc, d.max_mem_allocatable_kib,
			COALESCE(d.max_cpu_requests_mc, 0), COALESCE(d.max_mem_requests_kib, 0),
			COALESCE(d.max_pod_count, 0), COALESCE(d.pod_capacity, 0),
			COALESCE(d.instance_type, ''), COALESCE(d.machineset_name, ''),
			COALESCE(d.sample_count, 0), d.node_gpu_count,
			b.max_date
		FROM daily_node_digests d
		CROSS JOIN bounds b
		WHERE d.org_id = $1 AND d.cluster_uuid = $2 AND d.node = $3 AND d.schedule_type = $4
		  AND d.bucket_date >= CASE
			WHEN $6::date IS NULL THEN b.max_date - ($5::int - 1)
			ELSE LEAST(b.max_date - ($5::int - 1), $6::date)
		  END
		  AND d.bucket_date <= CASE
			WHEN $7::date IS NULL THEN b.max_date
			ELSE GREATEST(b.max_date, $7::date)
		  END
		ORDER BY d.bucket_date`,
		orgID, clusterUUID, nodeName, digestScheduleBusinessHours, windowDays, coverStartArg, coverEndArg)
	if err != nil {
		return nil, time.Time{}, time.Time{}, fmt.Errorf("query node business_hours detail digests: %w", err)
	}
	defer qrows.Close()

	out := make([]NodeDigestRow, 0, 64)
	var maxDate time.Time
	for qrows.Next() {
		var d NodeDigestRow
		if err := qrows.Scan(
			&d.BucketDate, &d.Node,
			&d.CPUUsageP50MC, &d.CPUUsageP95MC, &d.CPUUsageMaxMC,
			&d.MemUsageP50KiB, &d.MemUsageP95KiB, &d.MemUsageMaxKiB,
			&d.MaxCPUAllocMC, &d.MaxMemAllocKiB,
			&d.MaxCPURequestsMC, &d.MaxMemRequestsKiB,
			&d.MaxPodCount, &d.PodCapacity, &d.InstanceType, &d.MachineSetName, &d.SampleCount,
			&d.NodeGPUCount,
			&maxDate,
		); err != nil {
			return nil, time.Time{}, time.Time{}, fmt.Errorf("scan node business_hours detail digest: %w", err)
		}
		out = append(out, d)
	}
	if err := qrows.Err(); err != nil {
		return nil, time.Time{}, time.Time{}, fmt.Errorf("iterate node business_hours detail digests: %w", err)
	}
	if !maxDate.IsZero() {
		enrichEnd = maxDate.Truncate(24 * time.Hour)
		enrichStart = enrichEnd.AddDate(0, 0, -(windowDays - 1))
	}
	return out, enrichStart, enrichEnd, nil
}

// FilterNodeDigestsByInclusiveRange keeps rows whose bucket_date is in [start, end].
// Zero start or end means that bound is open.
func FilterNodeDigestsByInclusiveRange(rows []NodeDigestRow, start, end time.Time) []NodeDigestRow {
	if len(rows) == 0 {
		return rows
	}
	var startDay, endDay time.Time
	if !start.IsZero() {
		startDay = start.Truncate(24 * time.Hour)
	}
	if !end.IsZero() {
		endDay = end.Truncate(24 * time.Hour)
	}
	out := make([]NodeDigestRow, 0, len(rows))
	for _, r := range rows {
		d := r.BucketDate.Truncate(24 * time.Hour)
		if !startDay.IsZero() && d.Before(startDay) {
			continue
		}
		if !endDay.IsZero() && d.After(endDay) {
			continue
		}
		out = append(out, r)
	}
	return out
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
