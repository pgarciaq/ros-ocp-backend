package api

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// PVCRecommendationResponse is a single PVC recommendation in the API response.
type PVCRecommendationResponse struct {
	ClusterUUID                string                                     `json:"cluster_uuid"`
	Namespace                  string                                     `json:"namespace"`
	PersistentVolumeClaim      string                                     `json:"persistentvolumeclaim"`
	PersistentVolume           string                                     `json:"persistentvolume,omitempty"`
	StorageClass               string                                     `json:"storageclass,omitempty"`
	CapacityBytes              int64                                      `json:"capacity_bytes"`
	UsageBytesMax              int64                                      `json:"usage_bytes_max"`
	UsageRatio                 float64                                    `json:"usage_ratio"`
	RecommendationType         string                                     `json:"recommendation_type"`
	RecommendedBytes           *int64                                     `json:"recommended_bytes,omitempty"`
	DaysToFull                 *int                                       `json:"days_to_full,omitempty"`
	GrowthBytesPerDay          *int64                                     `json:"growth_bytes_per_day,omitempty"`
	EstimatedMonthlySavings *money.SavingsObject                       `json:"estimated_monthly_savings,omitempty"`
	Notifications              map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
	DataDays                   int                                        `json:"data_days"`
	Term                       string                                     `json:"term"`
	ResizeNote                 string                                     `json:"resize_note,omitempty"`
}

const (
	pvcDefaultOrderBy  = "usage_ratio"
	pvcDefaultOrderHow = "desc"
)

var pvcAllowedOrderBy = map[string]string{
	"usage_ratio":                   "usage_ratio",
	"estimated_monthly_savings":     "estimated_monthly_savings_usd",
	"estimated_monthly_savings_usd": "estimated_monthly_savings_usd",
	"pvc_name":                      "persistentvolumeclaim",
	"persistentvolumeclaim":         "persistentvolumeclaim",
	"capacity_bytes":                "capacity_bytes",
}

// PVCRecommendationListResponse wraps the list of PVC recommendations.
type PVCRecommendationListResponse struct {
	Meta struct {
		Count    int      `json:"count"`
		Limit    int      `json:"limit"`
		Offset   int      `json:"offset"`
		Currency string   `json:"currency"`
		Warnings []string `json:"warnings,omitempty"`
	} `json:"meta"`
	Links Links                       `json:"links"`
	Data  []PVCRecommendationResponse `json:"data"`
}

// GetPVCRecommendations handles GET /recommendations/openshift/pvcs.
func GetPVCRecommendations(c echo.Context) error {
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

	// Optional filters: filter[cluster], filter[project], filter[recommendation_type], filter[term], filter[storageclass]
	listFilters := parsePVCListFilters(c)

	ctx := c.Request().Context()

	filterSQL, args, argIdx, tagErr := buildPVCRecommendationFilterSQL(c, orgID, listFilters)
	if tagErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": tagErr.Error()})
	}

	orderCol, orderDir, orderErr := queryparams.ParseOrderBy(c, pvcAllowedOrderBy, pvcDefaultOrderBy, pvcDefaultOrderHow)
	if orderErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": orderErr.Error()})
	}

	countQuery := `SELECT COUNT(*) FROM pvc_recommendation_sets WHERE org_id = $1` + filterSQL
	var total int
	if err := pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		hlog.Errorf("PVC recommendation count failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to count PVC recommendations",
		})
	}

	query := pvcRecommendationSelectSQL + `
		FROM pvc_recommendation_sets
		WHERE org_id = $1` + filterSQL

	query += ` ORDER BY ` + orderCol + ` ` + orderDir + ` LIMIT $` + strconv.Itoa(argIdx) + ` OFFSET $` + strconv.Itoa(argIdx+1)
	pageArgs := append(args, limit, offset)

	rows, err := pool.Query(ctx, query, pageArgs...)
	if err != nil {
		hlog.Errorf("PVC recommendation query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch PVC recommendations",
		})
	}
	defer rows.Close()

	var data []PVCRecommendationResponse
	for rows.Next() {
		r, scanErr := scanPVCRecommendationRow(rows)
		if scanErr != nil {
			hlog.Errorf("scanning PVC recommendation row: %v", scanErr)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to read PVC recommendation rows",
			})
		}
		data = append(data, r)
	}
	if err := rows.Err(); err != nil {
		hlog.Errorf("PVC recommendation row iteration failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch PVC recommendations",
		})
	}

	resp := PVCRecommendationListResponse{}
	resp.Meta.Count = total
	resp.Meta.Limit = limit
	resp.Meta.Offset = offset
	resp.Meta.Currency = fetchClusterCurrency(ctx, orgID, listFilters.clusterFilter)
	resp.Links = buildLinks(c.Request(), total, limit, offset)
	resp.Data = data
	if resp.Data == nil {
		resp.Data = []PVCRecommendationResponse{}
	}

	attachTagWarningsToPVC(&resp, c, orgID, len(resp.Data))
	return c.JSON(http.StatusOK, resp)
}
