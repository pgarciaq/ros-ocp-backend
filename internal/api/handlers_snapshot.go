package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// SnapshotRecommendationResponse is a single snapshot recommendation in the API response.
type SnapshotRecommendationResponse struct {
	ClusterUUID         string                                     `json:"cluster_uuid"`
	Namespace           string                                     `json:"namespace"`
	SnapshotName        string                                     `json:"snapshot_name"`
	SourcePVCName       string                                     `json:"source_pvc_name"`
	VolumeSnapshotClass string                                     `json:"volume_snapshot_class,omitempty"`
	StorageClass        string                                     `json:"storageclass,omitempty"`
	CreationTimestamp   string                                     `json:"creation_timestamp"`
	RestoreSizeBytes    int64                                      `json:"restore_size_bytes"`
	AgeDays             int                                        `json:"age_days"`
	SourcePVCExists     bool                                       `json:"source_pvc_exists"`
	RestoredPVCCount    int                                        `json:"restored_pvc_count"`
	ManagedBy           string                                     `json:"managed_by,omitempty"`
	RecommendationType  string                                     `json:"recommendation_type"`
	EstimatedMonthlyCost *float32                                  `json:"estimated_monthly_cost_usd,omitempty"`
	Notifications       map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
}

// SnapshotRecommendationListResponse wraps the list of snapshot recommendations.
type SnapshotRecommendationListResponse struct {
	Meta struct {
		Count    int    `json:"count"`
		Limit    int    `json:"limit"`
		Offset   int    `json:"offset"`
		Currency string `json:"currency"`
	} `json:"meta"`
	Links Links                           `json:"links"`
	Data  []SnapshotRecommendationResponse `json:"data"`
}

// GetSnapshotRecommendations handles GET /recommendations/openshift/snapshots.
func GetSnapshotRecommendations(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

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

	countQuery := `SELECT COUNT(*) FROM snapshot_recommendation_sets WHERE org_id = $1` + filterSQL
	var total int
	if err := pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		hlog.Errorf("snapshot recommendation count failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to count snapshot recommendations",
		})
	}

	query := `
		SELECT cluster_uuid, namespace, snapshot_name, source_pvc_name,
			volume_snapshot_class, storageclass, creation_timestamp,
			restore_size_bytes, age_days, source_pvc_exists, restored_pvc_count,
			managed_by, recommendation_type, estimated_monthly_cost_usd,
			notification_codes
		FROM snapshot_recommendation_sets
		WHERE org_id = $1` + filterSQL

	query += ` ORDER BY age_days DESC LIMIT $` + strconv.Itoa(argIdx) + ` OFFSET $` + strconv.Itoa(argIdx+1)
	pageArgs := append(args, limit, offset)

	rows, err := pool.Query(ctx, query, pageArgs...)
	if err != nil {
		hlog.Errorf("snapshot recommendation query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch snapshot recommendations",
		})
	}
	defer rows.Close()

	var data []SnapshotRecommendationResponse
	for rows.Next() {
		var r SnapshotRecommendationResponse
		var codes []int16
		var creationTS interface{}
		if err := rows.Scan(
			&r.ClusterUUID, &r.Namespace, &r.SnapshotName, &r.SourcePVCName,
			&r.VolumeSnapshotClass, &r.StorageClass, &creationTS,
			&r.RestoreSizeBytes, &r.AgeDays, &r.SourcePVCExists, &r.RestoredPVCCount,
			&r.ManagedBy, &r.RecommendationType, &r.EstimatedMonthlyCost,
			&codes,
		); err != nil {
			hlog.Errorf("scanning snapshot recommendation row: %v", err)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to read snapshot recommendation rows",
			})
		}
		if ts, ok := creationTS.(time.Time); ok {
			r.CreationTimestamp = ts.UTC().Format(time.RFC3339)
		}
		r.Notifications = notifications.MapToKruizeFormat(codes)
		data = append(data, r)
	}
	if err := rows.Err(); err != nil {
		hlog.Errorf("snapshot recommendation row iteration failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch snapshot recommendations",
		})
	}

	resp := SnapshotRecommendationListResponse{}
	resp.Meta.Count = total
	resp.Meta.Limit = limit
	resp.Meta.Offset = offset
	resp.Meta.Currency = fetchClusterCurrency(ctx, orgID, clusterFilter)
	resp.Links = buildLinks(c.Request(), total, limit, offset)
	resp.Data = data
	if resp.Data == nil {
		resp.Data = []SnapshotRecommendationResponse{}
	}

	return c.JSON(http.StatusOK, resp)
}
