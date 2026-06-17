package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
)

// PVCHistoricalUsagePoint is a daily usage sample for a PVC.
type PVCHistoricalUsagePoint struct {
	Date          string `json:"date"`
	CapacityBytes int64  `json:"capacity_bytes"`
	UsageBytesMin int64  `json:"usage_bytes_min"`
	UsageBytesMax int64  `json:"usage_bytes_max"`
	UsageBytesAvg int64  `json:"usage_bytes_avg"`
}

// PVCRecommendationDetailResponse returns all recommendation terms and usage history for one PVC.
type PVCRecommendationDetailResponse struct {
	ClusterUUID           string                              `json:"cluster_uuid"`
	Namespace             string                              `json:"namespace"`
	PersistentVolumeClaim string                              `json:"persistentvolumeclaim"`
	MountedBy             string                              `json:"mounted_by,omitempty"`
	VMName                string                              `json:"vm_name,omitempty"`
	PersistentVolume      string                              `json:"persistentvolume,omitempty"`
	StorageClass          string                              `json:"storageclass,omitempty"`
	CapacityBytes         int64                               `json:"capacity_bytes"`
	Terms                 map[string]PVCRecommendationResponse `json:"terms"`
	HistoricalUsage       []PVCHistoricalUsagePoint           `json:"historical_usage,omitempty"`
}

type pvcDetailIdentity struct {
	clusterUUID string
	namespace   string
	pvcName     string
}

func parsePVCDetailIdentity(c echo.Context) pvcDetailIdentity {
	cluster := strings.TrimSpace(c.QueryParam("cluster_uuid"))
	if cluster == "" {
		cluster = queryparams.FirstFilter(c, "cluster")
	}
	namespace := strings.TrimSpace(c.QueryParam("namespace"))
	if namespace == "" {
		namespace = queryparams.FirstFilter(c, "project")
	}
	pvcName := strings.TrimSpace(c.QueryParam("persistentvolumeclaim"))
	if pvcName == "" {
		pvcName = strings.TrimSpace(c.QueryParam("pvc_name"))
	}
	return pvcDetailIdentity{clusterUUID: cluster, namespace: namespace, pvcName: pvcName}
}

// GetPVCRecommendationDetail handles GET /recommendations/openshift/pvcs/detail.
func GetPVCRecommendationDetail(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	id := parsePVCDetailIdentity(c)
	if id.clusterUUID == "" || id.namespace == "" || id.pvcName == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": "cluster_uuid, namespace, and persistentvolumeclaim are required",
		})
	}

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	ctx := c.Request().Context()

	termQuery := pvcRecommendationSelectSQL + `
		FROM pvc_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3 AND persistentvolumeclaim = $4
		ORDER BY term`

	rows, err := pool.Query(ctx, termQuery, orgID, id.clusterUUID, id.namespace, id.pvcName)
	if err != nil {
		hlog.Errorf("PVC detail term query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch PVC recommendation detail",
		})
	}
	defer rows.Close()

	terms := make(map[string]PVCRecommendationResponse)
	var summary PVCRecommendationDetailResponse
	summary.ClusterUUID = id.clusterUUID
	summary.Namespace = id.namespace
	summary.PersistentVolumeClaim = id.pvcName

	includeExplanation := RequestIncludesExplanation(c.QueryParam("include"))
	for rows.Next() {
		rec, scanErr := scanPVCRecommendationRow(rows, includeExplanation)
		if scanErr != nil {
			hlog.Errorf("PVC detail scan failed: %v", scanErr)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to read PVC recommendation rows",
			})
		}
		terms[rec.Term] = rec
		if rec.Term == "medium" || summary.CapacityBytes == 0 {
			summary.PersistentVolume = rec.PersistentVolume
			summary.StorageClass = rec.StorageClass
			summary.CapacityBytes = rec.CapacityBytes
			if rec.MountedBy != "" {
				summary.MountedBy = rec.MountedBy
			}
			if rec.VMName != "" {
				summary.VMName = rec.VMName
			}
		}
	}
	if err := rows.Err(); err != nil {
		hlog.Errorf("PVC detail row iteration failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch PVC recommendation detail",
		})
	}
	if len(terms) == 0 {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "not_found",
			"message": "PVC recommendation not found",
		})
	}

	histRows, err := pool.Query(ctx, `
		SELECT bucket_date, capacity_bytes, usage_bytes_min, usage_bytes_max, usage_bytes_avg
		FROM daily_pvc_digests
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace = $3 AND persistentvolumeclaim = $4
		ORDER BY bucket_date ASC`,
		orgID, id.clusterUUID, id.namespace, id.pvcName,
	)
	if err != nil {
		hlog.Errorf("PVC detail historical query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch PVC usage history",
		})
	}
	defer histRows.Close()

	var history []PVCHistoricalUsagePoint
	for histRows.Next() {
		var bucket time.Time
		var point PVCHistoricalUsagePoint
		if err := histRows.Scan(&bucket, &point.CapacityBytes, &point.UsageBytesMin, &point.UsageBytesMax, &point.UsageBytesAvg); err != nil {
			hlog.Errorf("PVC detail historical scan failed: %v", err)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to read PVC usage history",
			})
		}
		point.Date = bucket.Format("2006-01-02")
		history = append(history, point)
	}
	if err := histRows.Err(); err != nil {
		hlog.Errorf("PVC detail historical iteration failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch PVC usage history",
		})
	}

	summary.Terms = terms
	if len(history) > 0 {
		summary.HistoricalUsage = history
	}

	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, summary)
}
