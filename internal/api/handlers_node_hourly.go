package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
)

// NodeHourlyUtilizationRow represents a single hourly digest data point for a node.
type NodeHourlyUtilizationRow struct {
	ReportDate     string `json:"report_date"`
	Hour           int    `json:"hour"`
	CPUUsageP95MC  int    `json:"cpu_usage_p95_mc"`
	MemUsageP95KiB int    `json:"mem_usage_p95_kib"`
	SampleCount    int    `json:"sample_count"`
	MaxPodCount    int    `json:"max_pod_count"`
}

// NodeHourlyUtilizationResponse is the response for GET /node/{id}/hourly-utilization.
type NodeHourlyUtilizationResponse struct {
	Meta NodeHourlyUtilizationMeta  `json:"meta"`
	Data []NodeHourlyUtilizationRow `json:"data"`
}

// NodeHourlyUtilizationMeta contains response metadata.
type NodeHourlyUtilizationMeta struct {
	Count int `json:"count"`
	Days  int `json:"days"`
}

// GetNodeHourlyUtilization handles GET /recommendations/openshift/node/{id}/hourly-utilization.
func GetNodeHourlyUtilization(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)

	nodeName := strings.TrimSpace(c.Param("id"))
	if nodeName == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "node name is required in path"})
	}

	clusterID := strings.TrimSpace(c.QueryParam("cluster_uuid"))
	if clusterID == "" {
		clusterID = queryparams.FirstFilter(c, "cluster")
	}
	if clusterID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "cluster_uuid is required"})
	}

	days := defaultHourlyDays
	if daysParam := strings.TrimSpace(c.QueryParam("days")); daysParam != "" {
		if parsed, parseErr := strconv.Atoi(daysParam); parseErr == nil && parsed > 0 {
			days = parsed
		}
	}
	if filterDays := queryparams.FirstFilter(c, "days"); filterDays != "" {
		if parsed, parseErr := strconv.Atoi(filterDays); parseErr == nil && parsed > 0 {
			days = parsed
		}
	}
	if days > maxHourlyDays {
		days = maxHourlyDays
	}

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	ctx := c.Request().Context()
	allClusters, clusterErr := getClustersForOrg(ctx, orgID)
	if clusterErr != nil {
		hlog.Errorf("GetNodeHourlyUtilization: resolve clusters: %v", clusterErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}
	allowedClusters := filterClustersByRBAC(allClusters, userPerms)
	if !clusterAllowed(allowedClusters, clusterID) {
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, NodeHourlyUtilizationResponse{
			Meta: NodeHourlyUtilizationMeta{Count: 0, Days: days},
			Data: []NodeHourlyUtilizationRow{},
		})
	}

	rows, queryErr := queryHourlyNodeDigests(ctx, pool, orgID, clusterID, nodeName, days)
	if queryErr != nil {
		hlog.Errorf("GetNodeHourlyUtilization: query: %v", queryErr)
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": "unable to fetch records from database"})
	}

	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, NodeHourlyUtilizationResponse{
		Meta: NodeHourlyUtilizationMeta{Count: len(rows), Days: days},
		Data: rows,
	})
}

func queryHourlyNodeDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, nodeName string, days int) ([]NodeHourlyUtilizationRow, error) {
	since := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")

	var result []NodeHourlyUtilizationRow
	err := db.WithHeavyStatementTimeout(ctx, pool, func(ctx context.Context, q db.QueryRower) error {
		pgRows, qErr := q.Query(ctx, `
			SELECT report_date, hour,
				   cpu_usage_p95_mc, mem_usage_p95_kib, sample_count,
				   max_pod_count
			FROM hourly_node_digests
			WHERE org_id = $1
			  AND cluster_uuid = $2::uuid
			  AND node_name = $3
			  AND report_date >= $4::date
			ORDER BY report_date, hour`,
			orgID, clusterUUID, nodeName, since,
		)
		if qErr != nil {
			return qErr
		}
		defer pgRows.Close()

		for pgRows.Next() {
			var row NodeHourlyUtilizationRow
			var reportDate time.Time
			if scanErr := pgRows.Scan(
				&reportDate, &row.Hour,
				&row.CPUUsageP95MC, &row.MemUsageP95KiB, &row.SampleCount,
				&row.MaxPodCount,
			); scanErr != nil {
				return scanErr
			}
			row.ReportDate = reportDate.Format("2006-01-02")
			result = append(result, row)
		}
		return pgRows.Err()
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = []NodeHourlyUtilizationRow{}
	}
	return result, nil
}
