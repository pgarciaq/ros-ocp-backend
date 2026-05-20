package api

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
)

// FleetSummaryResponse is the JSON payload for GET /recommendations/openshift/fleet-summary.
type FleetSummaryResponse struct {
	TotalContainers        int     `json:"total_containers"`
	ActiveContainers       int     `json:"active_containers"`
	IdleContainers         int     `json:"idle_containers"`
	AbandonedContainers    int     `json:"abandoned_containers"`
	TotalMonthlySavingsUSD float64 `json:"total_monthly_savings_usd"`
	ClusterCount           int     `json:"cluster_count"`
}

func fleetSummaryNeedsClusterFilter(userPerms map[string][]string) bool {
	cfg := config.GetConfig()
	if !cfg.RBACEnabled {
		return false
	}
	if _, ok := userPerms["*"]; ok {
		return false
	}
	clusterPerms, ok := userPerms["openshift.cluster"]
	if !ok || utils.StringInSlice("*", clusterPerms) {
		return false
	}
	return true
}

// GetFleetSummary returns aggregate recommendation statistics across all clusters
// for the authenticated organization.
func GetFleetSummary(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	ctx := c.Request().Context()

	var summary FleetSummaryResponse

	if fleetSummaryNeedsClusterFilter(userPerms) {
		clusterUUIDs, qerr := getClustersForOrg(ctx, orgID)
		if qerr != nil {
			hlog.Errorf("fleet summary: get clusters failed: %v", qerr)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to fetch fleet summary",
			})
		}
		allowed := filterClustersByRBAC(clusterUUIDs, userPerms)
		if len(allowed) == 0 {
			return c.JSON(http.StatusOK, summary)
		}
		err = pool.QueryRow(ctx, `
			SELECT
				COUNT(*) AS total_containers,
				COUNT(*) FILTER (WHERE stale = false) AS active_containers,
				COUNT(*) FILTER (WHERE notification_codes @> ARRAY[5::smallint]) AS idle_containers,
				COUNT(*) FILTER (WHERE notification_codes @> ARRAY[8::smallint]) AS abandoned_containers,
				COALESCE(SUM(estimated_monthly_savings_usd) FILTER (WHERE stale = false), 0) AS total_monthly_savings_usd,
				COUNT(DISTINCT cluster_uuid) AS cluster_count
			FROM recommendation_sets
			WHERE org_id = $1 AND term = 'medium' AND engine = 'cost'
			  AND cluster_uuid::text = ANY($2::text[])`,
			orgID, allowed,
		).Scan(
			&summary.TotalContainers,
			&summary.ActiveContainers,
			&summary.IdleContainers,
			&summary.AbandonedContainers,
			&summary.TotalMonthlySavingsUSD,
			&summary.ClusterCount,
		)
	} else {
		err = pool.QueryRow(ctx, `
			SELECT
				COUNT(*) AS total_containers,
				COUNT(*) FILTER (WHERE stale = false) AS active_containers,
				COUNT(*) FILTER (WHERE notification_codes @> ARRAY[5::smallint]) AS idle_containers,
				COUNT(*) FILTER (WHERE notification_codes @> ARRAY[8::smallint]) AS abandoned_containers,
				COALESCE(SUM(estimated_monthly_savings_usd) FILTER (WHERE stale = false), 0) AS total_monthly_savings_usd,
				COUNT(DISTINCT cluster_uuid) AS cluster_count
			FROM recommendation_sets
			WHERE org_id = $1 AND term = 'medium' AND engine = 'cost'`,
			orgID,
		).Scan(
			&summary.TotalContainers,
			&summary.ActiveContainers,
			&summary.IdleContainers,
			&summary.AbandonedContainers,
			&summary.TotalMonthlySavingsUSD,
			&summary.ClusterCount,
		)
	}
	if err != nil {
		hlog.Errorf("fleet summary query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch fleet summary",
		})
	}

	return c.JSON(http.StatusOK, summary)
}
