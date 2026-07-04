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

// VMHourlyActivityRow represents a single hourly digest data point.
type VMHourlyActivityRow struct {
	ReportDate      string `json:"report_date"`
	Hour            int    `json:"hour"`
	CPUUsageP95MC   int    `json:"cpu_usage_p95_mc"`
	MemUsageP95KiB  int    `json:"mem_usage_p95_kib"`
	SampleCount     int    `json:"sample_count"`
	DiskReadIOPSP95 int    `json:"disk_read_iops_p95"`
	DiskWriteIOPSP95 int   `json:"disk_write_iops_p95"`
}

// VMHourlyActivityResponse is the response for GET /vm/hourly-activity.
type VMHourlyActivityResponse struct {
	Meta VMHourlyActivityMeta  `json:"meta"`
	Data []VMHourlyActivityRow `json:"data"`
}

// VMHourlyActivityMeta contains response metadata.
type VMHourlyActivityMeta struct {
	Count int `json:"count"`
	Days  int `json:"days"`
}

const (
	defaultHourlyDays = 14
	maxHourlyDays     = 90
)

// GetVMHourlyActivity handles GET /recommendations/openshift/vm/hourly-activity.
func GetVMHourlyActivity(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	userPerms := get_user_permissions(c)
	hlog := requestLogger(c, orgID)

	clusterID := strings.TrimSpace(c.QueryParam("cluster_uuid"))
	if clusterID == "" {
		clusterID = queryparams.FirstFilter(c, "cluster")
	}
	if clusterID == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "cluster_uuid is required"})
	}

	vmName := strings.TrimSpace(c.QueryParam("vm_name"))
	if vmName == "" {
		vmName = queryparams.FirstFilter(c, "vm_name")
	}
	namespace := strings.TrimSpace(c.QueryParam("namespace"))
	if namespace == "" {
		namespace = queryparams.FirstFilter(c, "project")
	}
	if vmName == "" || namespace == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": "vm_name and namespace are required"})
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
		hlog.Errorf("GetVMHourlyActivity: resolve clusters: %v", clusterErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to resolve clusters for organization",
		})
	}
	allowedClusters := filterClustersByRBAC(allClusters, userPerms)
	if !clusterAllowed(allowedClusters, clusterID) {
		setRecommendationNoStore(c)
		return c.JSON(http.StatusOK, VMHourlyActivityResponse{
			Meta: VMHourlyActivityMeta{Count: 0, Days: days},
			Data: []VMHourlyActivityRow{},
		})
	}

	rows, queryErr := queryHourlyVMDigests(ctx, pool, orgID, clusterID, namespace, vmName, days)
	if queryErr != nil {
		hlog.Errorf("GetVMHourlyActivity: query: %v", queryErr)
		return c.JSON(http.StatusInternalServerError, echo.Map{"status": "error", "message": "unable to fetch records from database"})
	}

	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, VMHourlyActivityResponse{
		Meta: VMHourlyActivityMeta{Count: len(rows), Days: days},
		Data: rows,
	})
}

func queryHourlyVMDigests(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID, namespace, vmName string, days int) ([]VMHourlyActivityRow, error) {
	since := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")
	pgRows, err := pool.Query(ctx, `
		SELECT report_date, hour,
			   cpu_usage_p95_mc, mem_usage_p95_kib, sample_count,
			   disk_read_iops_p95, disk_write_iops_p95
		FROM hourly_vm_digests
		WHERE org_id = $1
		  AND cluster_uuid = $2::uuid
		  AND namespace = $3
		  AND vm_name = $4
		  AND report_date >= $5::date
		ORDER BY report_date, hour`,
		orgID, clusterUUID, namespace, vmName, since,
	)
	if err != nil {
		return nil, err
	}
	defer pgRows.Close()

	var result []VMHourlyActivityRow
	for pgRows.Next() {
		var row VMHourlyActivityRow
		var reportDate time.Time
		if err := pgRows.Scan(
			&reportDate, &row.Hour,
			&row.CPUUsageP95MC, &row.MemUsageP95KiB, &row.SampleCount,
			&row.DiskReadIOPSP95, &row.DiskWriteIOPSP95,
		); err != nil {
			return nil, err
		}
		row.ReportDate = reportDate.Format("2006-01-02")
		result = append(result, row)
	}
	if err := pgRows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []VMHourlyActivityRow{}
	}
	return result, nil
}
