package api

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
)

// SnapshotRecommendationResponse is a single snapshot recommendation in the API response.
type SnapshotRecommendationResponse struct {
	ClusterUUID          string                                     `json:"cluster_uuid"`
	Namespace            string                                     `json:"namespace"`
	SnapshotName         string                                     `json:"snapshot_name"`
	SourcePVCName        string                                     `json:"source_pvc_name"`
	VolumeSnapshotClass  string                                     `json:"volume_snapshot_class,omitempty"`
	StorageClass         string                                     `json:"storageclass,omitempty"`
	CreationTimestamp    string                                     `json:"creation_timestamp"`
	LastReported         string                                     `json:"last_reported,omitempty"`
	RestoreSizeBytes     int64                                      `json:"restore_size_bytes"`
	AgeDays              int                                        `json:"age_days"`
	SourcePVCExists      bool                                       `json:"source_pvc_exists"`
	RestoredPVCCount     int                                        `json:"restored_pvc_count"`
	ManagedBy            string                                     `json:"managed_by,omitempty"`
	RecommendationType   string                                     `json:"recommendation_type"`
	EstimatedMonthlyCost *money.MoneyAmount                         `json:"estimated_monthly_cost,omitempty"`
	Notifications        map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
	Explanation          *model.SnapshotExplanationAPI              `json:"explanation,omitempty"`
	Count                int                                        `json:"count,omitempty"`
}

// SnapshotRecommendationListResponse wraps the list of snapshot recommendations.
type SnapshotRecommendationListResponse struct {
	Meta struct {
		Count      int    `json:"count"`
		Limit      int    `json:"limit"`
		Offset     int    `json:"offset"`
		HasNext    bool   `json:"has_next"`
		NextCursor string `json:"next_cursor,omitempty"`
		Currency   string `json:"currency"`
	} `json:"meta"`
	Links Links                            `json:"links"`
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
	includeExplanation := RequestIncludesExplanation(c.QueryParam("include"))

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

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

	orderCol, orderDir, orderErr := queryparams.ParseOrderBy(c, snapshotAllowedOrderBy, snapshotDefaultOrderBy, snapshotDefaultOrderHow)
	if orderErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": orderErr.Error()})
	}

	cursor, hasCursor, cursorErr := applySnapshotCursor(c, orderCol)
	if cursorErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": cursorErr.Error()})
	}
	if hasCursor {
		offset = 0
	}

	responseFormat, formatErr := listoptions.ResolveResponseFormat(c.Request().Header.Get("Accept"), c.QueryParam("format"))
	if formatErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": formatErr.Error()})
	}

	clusterFilter := queryparams.FirstFilter(c, "cluster")
	namespaceFilter := queryparams.FirstFilter(c, "project")
	typeFilter := queryparams.FirstFilter(c, "recommendation_type")
	userPerms := get_user_permissions(c)

	ctx := c.Request().Context()

	filterSQL := ""
	args := []interface{}{orgID}
	argIdx := 2

	rbacSQL, rbacArgs, rbacIdx, rbacDeny := snapshotRBACClusterFilter(userPerms, argIdx)
	if rbacDeny {
		resp := emptySnapshotListResponse(limit, offset, fetchClusterCurrency(ctx, orgID, clusterFilter))
		resp.Links = buildLinks(c.Request(), 0, limit, offset)
		if responseFormat == listoptions.ResponseFormatCSV {
			return streamCSV(c, csvFilename("snapshot-recommendations"), func(ctx context.Context, w io.Writer) error {
				return generateSnapshotRecCSV(ctx, w, resp.Data)
			})
		}
		return c.JSON(http.StatusOK, resp)
	}
	filterSQL += rbacSQL
	args = append(args, rbacArgs...)
	argIdx = rbacIdx

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
	sourcePVCFilter := queryparams.FirstFilter(c, "pvc_name")
	if sourcePVCFilter == "" {
		sourcePVCFilter = strings.TrimSpace(c.QueryParam("source_pvc_name"))
	}
	if sourcePVCFilter != "" {
		filterSQL += ` AND source_pvc_name = $` + strconv.Itoa(argIdx)
		args = append(args, sourcePVCFilter)
		argIdx++
	}
	if typeFilter != "" {
		filterSQL += ` AND recommendation_type = $` + strconv.Itoa(argIdx)
		args = append(args, typeFilter)
		argIdx++
	}

	groupByCluster, groupByProject, groupByErr := parseStorageListGroupBy(c)
	if groupByErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": groupByErr.Error()})
	}
	if groupByCluster || groupByProject {
		return getSnapshotRecommendationsGrouped(
			c, ctx, pool, hlog, orgID, filterSQL, args, argIdx, limit, offset,
			groupByCluster, clusterFilter, responseFormat, cursor, hasCursor,
		)
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

	query := snapshotRecommendationSelectSQL + `
		FROM snapshot_recommendation_sets
		WHERE org_id = $1` + filterSQL

	if hasCursor {
		seekSQL, seekArgs, nextIdx, seekErr := snapshotSeekSQL(orderCol, orderDir, cursor, len(cursor.SortValue) > 0, argIdx)
		if seekErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": seekErr.Error()})
		}
		query += ` AND ` + seekSQL
		args = append(args, seekArgs...)
		argIdx = nextIdx
	}

	query += ` ORDER BY ` + snapshotOrderNulls(orderCol, orderDir) +
		`, cluster_uuid ASC, namespace ASC, snapshot_name ASC`

	pageLimit := limit
	if pageLimit > 0 {
		pageLimit++
	}
	query += ` LIMIT $` + strconv.Itoa(argIdx)
	args = append(args, pageLimit)
	argIdx++

	if !hasCursor {
		query += ` OFFSET $` + strconv.Itoa(argIdx)
		args = append(args, offset)
	}

	rows, err := pool.Query(ctx, query, args...)
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
		r, scanErr := scanSnapshotRecommendationRow(rows, includeExplanation)
		if scanErr != nil {
			hlog.Errorf("scanning snapshot recommendation row: %v", scanErr)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to read snapshot recommendation rows",
			})
		}
		data = append(data, r)
	}
	if err := rows.Err(); err != nil {
		hlog.Errorf("snapshot recommendation row iteration failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch snapshot recommendations",
		})
	}

	hasNext := false
	var nextCursor string
	if limit > 0 && len(data) > limit {
		hasNext = true
		last := data[limit-1]
		nextCursor = snapshotNextCursor(orderCol, last, snapshotSortValue(last, orderCol))
		data = data[:limit]
	}

	resp := SnapshotRecommendationListResponse{}
	resp.Meta.Count = total
	resp.Meta.Limit = limit
	resp.Meta.Offset = offset
	resp.Meta.HasNext = hasNext
	resp.Meta.NextCursor = nextCursor
	resp.Meta.Currency = fetchClusterCurrency(ctx, orgID, clusterFilter)
	resp.Links = buildLinks(c.Request(), total, limit, offset)
	applyKeysetNextLink(&resp.Links, c.Request(), limit, hasNext, nextCursor)
	resp.Data = data
	if resp.Data == nil {
		resp.Data = []SnapshotRecommendationResponse{}
	}

	if responseFormat == listoptions.ResponseFormatCSV {
		return streamCSV(c, csvFilename("snapshot-recommendations"), func(ctx context.Context, w io.Writer) error {
			return generateSnapshotRecCSV(ctx, w, resp.Data)
		})
	}
	return c.JSON(http.StatusOK, resp)
}

func emptySnapshotListResponse(limit, offset int, currency string) SnapshotRecommendationListResponse {
	resp := SnapshotRecommendationListResponse{}
	resp.Meta.Limit = limit
	resp.Meta.Offset = offset
	resp.Meta.Currency = currency
	resp.Data = []SnapshotRecommendationResponse{}
	return resp
}

// snapshotRBACClusterFilter returns SQL AND args restricting snapshot rows to clusters
// the user may read when RBAC is enabled. rbacDeny is true when the user has cluster
// scope but no permitted clusters.
func snapshotRBACClusterFilter(userPerms map[string][]string, argIdx int) (sql string, args []interface{}, nextIdx int, deny bool) {
	nextIdx = argIdx
	if !config.GetConfig().RBACEnabled {
		return "", nil, nextIdx, false
	}
	if _, ok := userPerms["*"]; ok {
		return "", nil, nextIdx, false
	}
	clusterPerms, hasCluster := userPerms["openshift.cluster"]
	if !hasCluster {
		return "", nil, nextIdx, false
	}
	if utils.StringInSlice("*", clusterPerms) {
		return "", nil, nextIdx, false
	}
	if len(clusterPerms) == 0 {
		return "", nil, nextIdx, true
	}
	placeholders := make([]string, len(clusterPerms))
	args = make([]interface{}, len(clusterPerms))
	for i, cu := range clusterPerms {
		placeholders[i] = "$" + strconv.Itoa(argIdx+i)
		args[i] = cu
	}
	nextIdx = argIdx + len(clusterPerms)
	sql = " AND cluster_uuid IN (" + strings.Join(placeholders, ",") + ")"
	return sql, args, nextIdx, false
}
