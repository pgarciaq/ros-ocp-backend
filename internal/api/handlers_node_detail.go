package api

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
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
			COALESCE(nr.is_underutilized, false), COALESCE(nr.is_overcommitted, false),
			COALESCE(nr.idle_state, 'active'),
			nr.stranded_resource, COALESCE(nr.pod_count, 0), nr.pod_capacity,
			COALESCE(nr.trend_slope, 0), COALESCE(nr.notification_codes, '{}'),
			nr.recommended_cpu_cores, nr.recommended_memory_gib, COALESCE(nr.node_count_reduction, 0),
			nr.estimated_monthly_savings_usd,
			nr.machineset_name, nr.suggested_instance_type, nr.instance_type_reason,
			COALESCE(nr.updated_at, 'epoch'::timestamptz)` + baseFrom + `
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
			&row.IsUnderutilized, &row.IsOvercommitted,
			&row.IdleState,
			&row.StrandedResource, &row.PodCount, &row.PodCapacity,
			&row.TrendSlope, &row.NotificationCodes,
			&row.RecommendedCPUCores, &row.RecommendedMemoryGiB, &row.NodeCountReduction,
			&row.EstimatedMonthlySavings,
			&row.MachineSetName, &row.SuggestedInstanceType, &row.InstanceTypeReason,
			&row.UpdatedAt,
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

	grouped := groupNodeUtilizationRows(rawRows, "", "")
	if len(grouped) == 0 {
		return c.JSON(http.StatusNotFound, echo.Map{"status": "error", "message": "node not found"})
	}

	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, nodeUtilizationDetailFromRec(grouped[0]))
}

// nodeUtilizationDetailFromRec maps a list DTO to the single-node detail response shape.
func nodeUtilizationDetailFromRec(rec model.NodeUtilizationRec) model.NodeUtilizationDetailRec {
	idleState := rec.Classification.IdleState
	if idleState == "" {
		idleState = "active"
	}
	detail := model.NodeUtilizationDetailRec{
		Node:                  rec.Node,
		ClusterUUID:           rec.ClusterUUID,
		InstanceType:          rec.InstanceType,
		MachineSetName:        rec.MachineSetName,
		PodCount:              rec.PodCount,
		PodCapacity:           rec.PodCapacity,
		PodSchedulingHeadroom: rec.PodSchedulingHeadroom,
		IdleState:             idleState,
		SuggestedInstanceType: rec.SuggestedInstanceType,
		InstanceTypeReason:    rec.InstanceTypeReason,
		Metrics:               rec.Metrics,
		CPUOvercommitRatio:    rec.CPUOvercommitRatio,
		TrendSlope:            rec.TrendSlope,
		RecommendationTerms: rec.RecommendationTerms,
	}
	if termRec, ok := rec.RecommendationTerms["medium_term"]; ok && termRec.RecommendationEngines != nil {
		if eng := termRec.RecommendationEngines.Cost; eng != nil && len(eng.Notifications) > 0 {
			detail.Notifications = eng.Notifications
		} else if eng := termRec.RecommendationEngines.Performance; eng != nil && len(eng.Notifications) > 0 {
			detail.Notifications = eng.Notifications
		}
	}
	return detail
}
