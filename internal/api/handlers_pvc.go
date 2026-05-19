package api

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// PVCRecommendationResponse is a single PVC recommendation in the API response.
type PVCRecommendationResponse struct {
	ClusterUUID        string                                  `json:"cluster_uuid"`
	Namespace          string                                  `json:"namespace"`
	PersistentVolumeClaim string                               `json:"persistentvolumeclaim"`
	PersistentVolume   string                                  `json:"persistentvolume,omitempty"`
	StorageClass       string                                  `json:"storageclass,omitempty"`
	CapacityBytes      int64                                   `json:"capacity_bytes"`
	UsageBytesMax      int64                                   `json:"usage_bytes_max"`
	UsageRatio         float64                                 `json:"usage_ratio"`
	RecommendationType string                                  `json:"recommendation_type"`
	RecommendedBytes   *int64                                  `json:"recommended_bytes,omitempty"`
	DaysToFull         *int                                    `json:"days_to_full,omitempty"`
	GrowthBytesPerDay  *int64                                  `json:"growth_bytes_per_day,omitempty"` // nil when DB column is NULL (insufficient trend data)
	Notifications      map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
	DataDays           int                                     `json:"data_days"`
	ResizeNote         string                                  `json:"resize_note,omitempty"`
}

// PVCRecommendationListResponse wraps the list of PVC recommendations.
type PVCRecommendationListResponse struct {
	Meta struct {
		Count  int `json:"count"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	} `json:"meta"`
	Links Links                      `json:"links"`
	Data  []PVCRecommendationResponse `json:"data"`
}

// GetPVCRecommendations handles GET /recommendations/openshift/pvcs.
func GetPVCRecommendations(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	// Pagination
	limit := 20
	offset := 0
	if l := c.QueryParam("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	if o := c.QueryParam("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	// Optional filters
	clusterFilter := c.QueryParam("cluster_uuid")
	namespaceFilter := c.QueryParam("namespace")
	typeFilter := c.QueryParam("recommendation_type")

	ctx := c.Request().Context()

	filterSQL := ""
	args := []interface{}{orgID}
	argIdx := 2

	if clusterFilter != "" {
		filterSQL += ` AND cluster_uuid = $` + strconv.Itoa(argIdx)
		args = append(args, clusterFilter)
		argIdx++
	}
	if namespaceFilter != "" {
		filterSQL += ` AND namespace = $` + strconv.Itoa(argIdx)
		args = append(args, namespaceFilter)
		argIdx++
	}
	if typeFilter != "" {
		filterSQL += ` AND recommendation_type = $` + strconv.Itoa(argIdx)
		args = append(args, typeFilter)
		argIdx++
	}

	countQuery := `SELECT COUNT(*) FROM pvc_recommendation_sets WHERE org_id = $1` + filterSQL
	var total int
	if err := pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		log.Errorf("PVC recommendation count failed for org=%s: %v", orgID, err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to count PVC recommendations",
		})
	}

	query := `
		SELECT cluster_uuid, namespace, persistentvolumeclaim, persistentvolume,
			storageclass, capacity_bytes, usage_bytes_max, usage_ratio,
			recommendation_type, recommended_bytes, days_to_full,
			growth_bytes_per_day, notification_codes, data_days
		FROM pvc_recommendation_sets
		WHERE org_id = $1` + filterSQL

	query += ` ORDER BY usage_ratio DESC LIMIT $` + strconv.Itoa(argIdx) + ` OFFSET $` + strconv.Itoa(argIdx+1)
	pageArgs := append(args, limit, offset)

	rows, err := pool.Query(ctx, query, pageArgs...)
	if err != nil {
		log.Errorf("PVC recommendation query failed for org=%s: %v", orgID, err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch PVC recommendations",
		})
	}
	defer rows.Close()

	var data []PVCRecommendationResponse
	for rows.Next() {
		var r PVCRecommendationResponse
		var codes []int16
		var growth sql.NullInt64
		if err := rows.Scan(
			&r.ClusterUUID, &r.Namespace, &r.PersistentVolumeClaim, &r.PersistentVolume,
			&r.StorageClass, &r.CapacityBytes, &r.UsageBytesMax, &r.UsageRatio,
			&r.RecommendationType, &r.RecommendedBytes, &r.DaysToFull,
			&growth, &codes, &r.DataDays,
		); err != nil {
			log.Errorf("scanning PVC recommendation row: %v", err)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to read PVC recommendation rows",
			})
		}
		if growth.Valid {
			v := growth.Int64
			r.GrowthBytesPerDay = &v
		}
		r.Notifications = notifications.MapToKruizeFormat(codes)
		switch r.RecommendationType {
		case "oversized":
			r.ResizeNote = "Kubernetes does not support in-place PVC shrinking. Reducing this PVC requires creating a smaller volume, migrating data, and deleting the original."
		case "orphaned":
			r.ResizeNote = "This PVC has zero usage. If the data is no longer needed, deleting the PVC will reclaim the backing storage volume."
		}
		data = append(data, r)
	}
	if err := rows.Err(); err != nil {
		log.Errorf("PVC recommendation row iteration failed for org=%s: %v", orgID, err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch PVC recommendations",
		})
	}

	resp := PVCRecommendationListResponse{}
	resp.Meta.Count = total
	resp.Meta.Limit = limit
	resp.Meta.Offset = offset
	resp.Links = buildLinks(c.Request(), total, limit, offset)
	resp.Data = data
	if resp.Data == nil {
		resp.Data = []PVCRecommendationResponse{}
	}

	return c.JSON(http.StatusOK, resp)
}
