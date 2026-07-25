package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// GetNodeUtilizationDetail handles GET /recommendations/openshift/nodes/{node}.
// Returns full recommendation data for one node (all terms and engines), not paginated.
func GetNodeUtilizationDetail(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)

	nodeName := strings.TrimSpace(c.Param("node"))
	if nodeName == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "node name is required"})
	}

	if restrictNodes, allowedNodes := openshiftNodeRBACScope(userPerms); restrictNodes {
		if len(allowedNodes) == 0 {
			return c.JSON(http.StatusNotFound, echo.Map{"status": "error", "message": "node not found"})
		}
		allowed := false
		for _, n := range allowedNodes {
			if n == nodeName {
				allowed = true
				break
			}
		}
		if !allowed {
			return c.JSON(http.StatusNotFound, echo.Map{"status": "error", "message": "node not found"})
		}
	}

	pool := database.GetPool()
	if pool == nil {
		hlog.Warnf("GetNodeUtilizationDetail: database pool unavailable")
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	ctx := c.Request().Context()

	allClusters, err := getClustersForOrg(ctx, orgID)
	if err != nil {
		hlog.Warnf("GetNodeUtilizationDetail: failed to resolve clusters: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}
	allowedClusters := filterClustersByRBAC(allClusters, userPerms)
	if len(allowedClusters) == 0 {
		return c.JSON(http.StatusNotFound, echo.Map{"status": "error", "message": "node not found"})
	}

	clusterFilter := strings.TrimSpace(c.QueryParam("cluster_uuid"))
	if clusterFilter == "" {
		clusterFilter = strings.TrimSpace(c.QueryParam("cluster"))
	}
	if clusterFilter != "" {
		found := false
		for _, id := range allowedClusters {
			if id == clusterFilter {
				found = true
				break
			}
		}
		if !found {
			return c.JSON(http.StatusNotFound, echo.Map{"status": "error", "message": "node not found"})
		}
		allowedClusters = []string{clusterFilter}
	}

	baseFrom := `
		FROM node_recommendations nr
		WHERE nr.org_id = $1 AND nr.cluster_uuid::text = ANY($2) AND nr.node = $3`
	args := []interface{}{orgID, allowedClusters, nodeName}

	detailSQL := `
		SELECT nr.node, nr.cluster_uuid, nr.instance_type, COALESCE(nr.term, 'medium'), COALESCE(nr.engine, 'cost'),
			COALESCE(nr.cpu_util_p50, 0), COALESCE(nr.cpu_util_p95, 0),
			COALESCE(nr.mem_util_p50, 0), COALESCE(nr.mem_util_p95, 0),
			COALESCE(nr.cpu_overcommit_ratio, 0),
			COALESCE(nr.category, 'optimized'),
			COALESCE(nr.idle_state, 'active'),
			nr.stranded_resource, COALESCE(nr.pod_count, 0), nr.pod_capacity,
			COALESCE(nr.trend_slope, 0), COALESCE(nr.notification_codes, '{}'),
			nr.recommended_cpu_cores, nr.recommended_memory_gib, COALESCE(nr.node_count_reduction, 0),
			nr.estimated_savings_cents,
			nr.machineset_name, nr.suggested_instance_type, nr.instance_type_reason,
			nr.confidence_level, nr.data_days,
			COALESCE(nr.updated_at, 'epoch'::timestamptz),
			nr.expl_data_days, nr.expl_target_utilization_bp,
			nr.expl_current_cpu_mc, nr.expl_current_mem_kib,
			nr.expl_max_cpu_usage_p95_mc, nr.expl_max_mem_usage_p95_kib,
			nr.expl_pod_scheduling_headroom_bp, nr.expl_ema_imbalance_bp,
			nr.expl_consolidation_applied, nr.expl_sizing_formula` + baseFrom + `
		ORDER BY nr.term, nr.engine`

	rows, err := pool.Query(ctx, detailSQL, args...)
	if err != nil {
		hlog.Warnf("GetNodeUtilizationDetail: query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to load node utilization recommendations",
		})
	}
	defer rows.Close()

	var rawRows []nodeUtilRow
	for rows.Next() {
		var row nodeUtilRow
		err := rows.Scan(
			&row.Node, &row.ClusterUUID, &row.InstanceType, &row.Term, &row.Engine,
			&row.CPUUtilP50, &row.CPUUtilP95,
			&row.MemUtilP50, &row.MemUtilP95,
			&row.CPUOvercommitRatio,
			&row.Category,
			&row.IdleState,
			&row.StrandedResource, &row.PodCount, &row.PodCapacity,
			&row.TrendSlope, &row.NotificationCodes,
			&row.RecommendedCPUCores, &row.RecommendedMemoryGiB, &row.NodeCountReduction,
			&row.EstimatedMonthlySavings,
			&row.MachineSetName, &row.SuggestedInstanceType, &row.InstanceTypeReason,
			&row.ConfidenceLevel, &row.DataDays,
			&row.UpdatedAt,
			&row.ExplDataDays, &row.ExplTargetUtilizationBP,
			&row.ExplCurrentCPUMC, &row.ExplCurrentMemKiB,
			&row.ExplMaxCPUUsageP95MC, &row.ExplMaxMemUsageP95KiB,
			&row.ExplPodSchedulingHeadroomBP, &row.ExplEMAImbalanceBP,
			&row.ExplConsolidationApplied, &row.ExplSizingFormula,
		)
		if err != nil {
			hlog.Warnf("GetNodeUtilizationDetail: scan failed: %v", err)
			continue
		}
		rawRows = append(rawRows, row)
	}
	if err := rows.Err(); err != nil {
		hlog.Warnf("GetNodeUtilizationDetail: rows iteration failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to load node utilization recommendations",
		})
	}
	if len(rawRows) == 0 {
		return c.JSON(http.StatusNotFound, echo.Map{"status": "error", "message": "node not found"})
	}

	termFilterRaw := queryparams.FirstFilter(c, "term")
	termFilter, termErr := queryparams.NormalizeRecommendationTermFilter(termFilterRaw)
	if termErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": termErr.Error()})
	}
	engineFilters, engineErr := queryparams.CollectEngineFilterValues(c)
	if engineErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": engineErr.Error()})
	}
	engineFilter := ""
	if len(engineFilters) == 1 {
		engineFilter = engineFilters[0]
	}

	storedCurrency := resolveClusterCurrency(ctx, orgID, clusterFilter)
	userCurrency := resolveUserCurrency(ctx, orgID)
	rate := fetchExchangeRate(ctx, orgID, storedCurrency, userCurrency)
	displayCurrency := userCurrency
	if rate == 1.0 && storedCurrency != userCurrency {
		displayCurrency = storedCurrency
	}
	grouped := groupNodeUtilizationRows(rawRows, engineFilter, termFilter, RequestIncludesExplanation(c.QueryParam("include")), storedCurrency)
	if len(grouped) == 0 {
		return c.JSON(http.StatusNotFound, echo.Map{"status": "error", "message": "node not found"})
	}
	convertNodeUtilRecsAmounts(grouped, rate, displayCurrency)

	detail := nodeUtilizationDetailFromRec(grouped[0])

	if config.VisualInsightsEnabled() {
		digests, digestErr := queryNodeDailyDigests(ctx, pool, orgID, allowedClusters, nodeName, c)
		if digestErr != nil {
			hlog.Warnf("GetNodeUtilizationDetail: daily digests query failed: %v", digestErr)
		} else if len(digests) > 0 {
			detail.DailyDigests = digests
		}
	}

	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, detail)
}

// nodeUtilizationDetailFromRec maps a list DTO to the single-node detail response shape.
func nodeUtilizationDetailFromRec(rec model.NodeUtilizationRec) model.NodeUtilizationDetailRec {
	category := rec.Classification.Category
	if category == "" {
		category = "optimized"
	}
	detail := model.NodeUtilizationDetailRec{
		ID:                    rec.ID,
		Node:                  rec.Node,
		ClusterUUID:           rec.ClusterUUID,
		InstanceType:          rec.InstanceType,
		MachineSetName:        rec.MachineSetName,
		PodCount:              rec.PodCount,
		PodCapacity:           rec.PodCapacity,
		PodSchedulingHeadroom: rec.PodSchedulingHeadroom,
		Category:              category,
		SuggestedInstanceType: rec.SuggestedInstanceType,
		InstanceTypeReason:    rec.InstanceTypeReason,
		Metrics:               rec.Metrics,
		CPUOvercommitRatio:    rec.CPUOvercommitRatio,
		TrendSlope:            rec.TrendSlope,
		RecommendationTerms: rec.RecommendationTerms,
	}
	detail.Notifications = aggregateNodeUtilizationNotifications(rec)
	return detail
}

// nodeUtilDetailTermOrder processes longer windows first so shorter (more recent) terms win on duplicate codes.
var nodeUtilDetailTermOrder = []string{"long_term", "medium_term", "short_term"}

func aggregateNodeUtilizationNotifications(rec model.NodeUtilizationRec) map[string]notifications.NotificationEntry {
	all := map[string]notifications.NotificationEntry{}
	for _, termKey := range nodeUtilDetailTermOrder {
		termRec, ok := rec.RecommendationTerms[termKey]
		if !ok || termRec.RecommendationEngines == nil {
			continue
		}
		eng := termRec.RecommendationEngines
		if eng.Cost != nil {
			mergeNodeUtilNotificationMaps(all, eng.Cost.Notifications)
		}
		if eng.Performance != nil {
			mergeNodeUtilNotificationMaps(all, eng.Performance.Notifications)
		}
	}
	if len(all) == 0 {
		return nil
	}
	return all
}

func mergeNodeUtilNotificationMaps(dst, src map[string]notifications.NotificationEntry) {
	if len(src) == 0 {
		return
	}
	for k, v := range src {
		if existing, ok := dst[k]; !ok || nodeUtilNotificationHigherPriority(v, existing) {
			dst[k] = v
		}
	}
}

func nodeUtilNotificationHigherPriority(a, b notifications.NotificationEntry) bool {
	return nodeUtilNotificationSeverityRank(a) > nodeUtilNotificationSeverityRank(b)
}

func nodeUtilNotificationSeverityRank(e notifications.NotificationEntry) int {
	def, ok := notifications.Definitions[e.Code]
	if !ok {
		return 0
	}
	switch def.Severity {
	case "CRITICAL":
		return 3
	case "WARNING":
		return 2
	case "INFO":
		return 1
	default:
		return 0
	}
}

const defaultNodeDigestDays = 14

// queryNodeDailyDigests fetches daily digest rows from daily_node_digests for the
// given node, respecting start_date/end_date query params (default: last 14 days).
func queryNodeDailyDigests(ctx context.Context, pool *pgxpool.Pool, orgID string, allowedClusters []string, nodeName string, c echo.Context) ([]model.NodeDailyDigestItem, error) {
	endDate := time.Now().UTC().Truncate(24 * time.Hour)
	startDate := endDate.AddDate(0, 0, -defaultNodeDigestDays)

	if sd := strings.TrimSpace(c.QueryParam("start_date")); sd != "" {
		if parsed, err := time.Parse("2006-01-02", sd); err == nil {
			startDate = parsed
		}
	}
	if ed := strings.TrimSpace(c.QueryParam("end_date")); ed != "" {
		if parsed, err := time.Parse("2006-01-02", ed); err == nil {
			endDate = parsed
		}
	}

	sql := `
		SELECT bucket_date, COALESCE(cpu_usage_p50_mc, 0), COALESCE(cpu_usage_p95_mc, 0),
			cpu_usage_max_mc,
			COALESCE(mem_usage_p50_kib, 0), COALESCE(mem_usage_p95_kib, 0),
			mem_usage_max_kib,
			COALESCE(max_cpu_allocatable_mc, 0), COALESCE(max_mem_allocatable_kib, 0),
			COALESCE(max_cpu_requests_mc, 0), COALESCE(max_mem_requests_kib, 0)
		FROM daily_node_digests
		WHERE org_id = $1 AND cluster_uuid::text = ANY($2) AND node = $3
			AND bucket_date >= $4 AND bucket_date <= $5
		ORDER BY bucket_date ASC`

	rows, err := pool.Query(ctx, sql, orgID, allowedClusters, nodeName, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var digests []model.NodeDailyDigestItem
	for rows.Next() {
		var d model.NodeDailyDigestItem
		var bucketDate time.Time
		if err := rows.Scan(&bucketDate, &d.CPUUsageP50MC, &d.CPUUsageP95MC, &d.CPUUsageMaxMC, &d.MemUsageP50KiB, &d.MemUsageP95KiB, &d.MemUsageMaxKiB, &d.MaxCPUAllocatableMC, &d.MaxMemAllocatableKiB, &d.MaxCPURequestsMC, &d.MaxMemRequestsKiB); err != nil {
			return nil, err
		}
		d.BucketDate = bucketDate.Format("2006-01-02")
		digests = append(digests, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return digests, nil
}
