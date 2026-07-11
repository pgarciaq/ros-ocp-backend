package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetheatmap"
)

var heatmapScanErrors = promauto.NewCounter(prometheus.CounterOpts{
	Name: "rosocp_fleet_heatmap_scan_errors_total",
	Help: "Number of fleet heatmap rows that failed to scan (schema mismatch or data corruption)",
})

// FleetHeatmapMeta is the metadata object for fleet heatmap responses.
type FleetHeatmapMeta struct {
	Count        int      `json:"count"`
	Metric       string   `json:"metric"`
	Term         string   `json:"term"`
	Engine       string   `json:"engine"`
	LatestUpdate string   `json:"latest_update"`
	DataWindow   string   `json:"data_window"`
	Warnings     []string `json:"warnings,omitempty"`
}

// FleetHeatmapNode is a single node cell in the fleet heatmap.
type FleetHeatmapNode struct {
	Node                  string  `json:"node"`
	ClusterUUID           string  `json:"cluster_uuid"`
	ClusterAlias          string  `json:"cluster_alias"`
	MachineSetName        string  `json:"machineset_name"`
	InstanceType          string  `json:"instance_type"`
	CPUUtilP95            float32 `json:"cpu_util_p95"`
	MemUtilP95            float32 `json:"mem_util_p95"`
	IdleState             string  `json:"idle_state"`
	UtilizationBand       string  `json:"utilization_band"`
	NodeCountReduction    int     `json:"node_count_reduction"`
	EstimatedSavingsCents int64   `json:"estimated_savings_cents"`
}

// FleetHeatmapResponse is the full response for GET /recommendations/openshift/fleet-heatmap.
type FleetHeatmapResponse struct {
	Meta FleetHeatmapMeta   `json:"meta"`
	Data []FleetHeatmapNode `json:"data"`
}

const (
	defaultHeatmapTerm   = "medium"
	defaultHeatmapEngine = "cost"
	defaultHeatmapMetric = "cpu"
)

// UtilizationBand maps a p95 utilization ratio and idle state to a display band.
func UtilizationBand(utilP95 float32, idleState string) string {
	if idleState == "idle" || idleState == "zombie" || utilP95 < 0.10 {
		return "idle"
	}
	if utilP95 < 0.30 {
		return "low"
	}
	if utilP95 < 0.65 {
		return "moderate"
	}
	if utilP95 < 0.85 {
		return "healthy"
	}
	return "hot"
}

func dataWindowLabel(term string) string {
	switch term {
	case "short":
		return "1 day (short term p95)"
	case "long":
		return "15 days (long term p95)"
	default:
		return "7 days (medium term p95)"
	}
}

// GetFleetHeatmap returns all nodes colored by utilization, grouped by MachineSet.
func GetFleetHeatmap(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)

	pool := database.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	ctx := c.Request().Context()

	term := queryparams.FirstFilter(c, "term")
	if term == "" {
		term = defaultHeatmapTerm
	}
	if term != "short" && term != "medium" && term != "long" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "invalid term; must be 'short', 'medium', or 'long'"})
	}
	engine := queryparams.FirstFilter(c, "engine")
	if engine == "" {
		engine = defaultHeatmapEngine
	}
	if engine != "cost" && engine != "performance" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "invalid engine; must be 'cost' or 'performance'"})
	}
	metric := c.QueryParam("metric")
	if metric == "" {
		metric = defaultHeatmapMetric
	}
	if metric != "cpu" && metric != "memory" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "invalid metric; must be 'cpu' or 'memory'"})
	}

	clusterFilter := queryparams.FirstFilter(c, "cluster")

	rbacScoped := fleetSummaryNeedsClusterFilter(userPerms)
	cacheKey := fleetheatmap.CacheKey(orgID, rbacScoped, userPerms, metric, term, engine, clusterFilter)
	if cached, ok := fleetheatmap.Get(cacheKey); ok {
		return c.JSON(http.StatusOK, cached)
	}

	allClusters, err := getClustersForOrg(ctx, orgID)
	if err != nil {
		hlog.Errorf("fleet heatmap: get clusters failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}
	allowedClusters := filterClustersByRBAC(allClusters, userPerms)
	if clusterFilter != "" {
		allowedClusters, _ = restrictClustersToQueryFilter(allowedClusters, clusterFilter)
	}

	if len(allowedClusters) == 0 {
		resp := FleetHeatmapResponse{
			Meta: FleetHeatmapMeta{Count: 0, Metric: metric, Term: term, Engine: engine, DataWindow: dataWindowLabel(term)},
			Data: []FleetHeatmapNode{},
		}
		return c.JSON(http.StatusOK, resp)
	}

	maxNodes := config.GetConfig().FleetHeatmapMaxNodes

	var nodes []FleetHeatmapNode
	var latestUpdate time.Time
	var scanErrors int

	err = database.WithHeavyStatementTimeout(ctx, pool, func(ctx context.Context, q database.QueryRower) error {
		rows, qErr := q.Query(ctx, `
			SELECT nr.node, nr.cluster_uuid::text, COALESCE(c.cluster_alias, nr.cluster_uuid::text),
				COALESCE(nr.machineset_name, ''), COALESCE(nr.instance_type, ''),
				COALESCE(nr.cpu_util_p95, 0), COALESCE(nr.mem_util_p95, 0),
				COALESCE(nr.idle_state, 'active'),
				COALESCE(nr.node_count_reduction, 0), COALESCE(nr.estimated_savings_cents, 0),
				nr.updated_at
			FROM node_recommendations nr
			LEFT JOIN (
				clusters c
				JOIN rh_accounts ra ON ra.id = c.tenant_id AND ra.org_id = $1
			) ON nr.cluster_uuid = c.cluster_uuid
			WHERE nr.org_id = $1 AND nr.term = $2 AND nr.engine = $3
				AND nr.cluster_uuid::text = ANY($4)
			ORDER BY nr.machineset_name NULLS LAST, nr.node
			LIMIT $5`,
			orgID, term, engine, allowedClusters, maxNodes+1,
		)
		if qErr != nil {
			return qErr
		}
		defer rows.Close()

		for rows.Next() {
			var n FleetHeatmapNode
			var updatedAt sql.NullTime
			if scanErr := rows.Scan(
				&n.Node, &n.ClusterUUID, &n.ClusterAlias,
				&n.MachineSetName, &n.InstanceType,
				&n.CPUUtilP95, &n.MemUtilP95,
				&n.IdleState,
				&n.NodeCountReduction, &n.EstimatedSavingsCents,
				&updatedAt,
			); scanErr != nil {
				scanErrors++
				heatmapScanErrors.Inc()
				hlog.Warnf("fleet heatmap row scan failed: %v", scanErr)
				continue
			}

			utilP95 := n.CPUUtilP95
			if metric == "memory" {
				utilP95 = n.MemUtilP95
			}
			n.UtilizationBand = UtilizationBand(utilP95, n.IdleState)

			if updatedAt.Valid && updatedAt.Time.After(latestUpdate) {
				latestUpdate = updatedAt.Time
			}

			nodes = append(nodes, n)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			return rowsErr
		}
		return nil
	})
	if err != nil {
		if database.IsStatementTimeoutCancellation(err) {
			database.RecordStatementTimeoutCancellation(err)
			hlog.Warnf("fleet heatmap query timed out: %v", err)
		} else {
			hlog.Errorf("fleet heatmap query failed: %v", err)
		}
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch fleet heatmap data",
		})
	}

	if restrictNodes, allowedNodes := openshiftNodeRBACScope(userPerms); restrictNodes {
		allowedSet := make(map[string]bool, len(allowedNodes))
		for _, name := range allowedNodes {
			allowedSet[name] = true
		}
		filtered := nodes[:0]
		for _, n := range nodes {
			if allowedSet[n.Node] {
				filtered = append(filtered, n)
			}
		}
		nodes = filtered
	}

	var warnings []string
	if len(nodes) > maxNodes {
		nodes = nodes[:maxNodes]
		warnings = append(warnings, fmt.Sprintf(
			"Results capped at %d nodes. Filter by cluster to narrow scope.", maxNodes))
	}
	if scanErrors > 0 {
		rowWord := "rows"
		if scanErrors == 1 {
			rowWord = "row"
		}
		warnings = append(warnings, fmt.Sprintf("%d %s could not be read", scanErrors, rowWord))
	}

	if nodes == nil {
		nodes = []FleetHeatmapNode{}
	}

	latestStr := ""
	if !latestUpdate.IsZero() {
		latestStr = latestUpdate.Format(time.RFC3339)
	}

	resp := FleetHeatmapResponse{
		Meta: FleetHeatmapMeta{
			Count:        len(nodes),
			Metric:       metric,
			Term:         term,
			Engine:       engine,
			LatestUpdate: latestStr,
			DataWindow:   dataWindowLabel(term),
			Warnings:     warnings,
		},
		Data: nodes,
	}

	if config.GetConfig() != nil {
		fleetheatmap.Put(cacheKey, resp)
	}
	return c.JSON(http.StatusOK, resp)
}
