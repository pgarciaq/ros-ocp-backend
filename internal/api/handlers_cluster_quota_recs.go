package api

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// ClusterQuotaResourceValues groups quota metrics for cluster-quota API responses.
type ClusterQuotaResourceValues struct {
	CPURequestMillicores *int64 `json:"cpu_request_millicores,omitempty"`
	CPULimitMillicores   *int64 `json:"cpu_limit_millicores,omitempty"`
	MemoryRequestBytes   *int64 `json:"memory_request_bytes,omitempty"`
	MemoryLimitBytes     *int64 `json:"memory_limit_bytes,omitempty"`
	StorageRequestBytes  *int64 `json:"storage_request_bytes,omitempty"`
	Pods                 *int64 `json:"pods,omitempty"`
}

// ClusterQuotaUtilizationPercents exposes utilization as human-readable percentages.
type ClusterQuotaUtilizationPercents struct {
	CPURequestPercent     *float64 `json:"cpu_request_percent,omitempty"`
	MemoryRequestPercent  *float64 `json:"memory_request_percent,omitempty"`
	StorageRequestPercent *float64 `json:"storage_request_percent,omitempty"`
	PodsPercent           *float64 `json:"pods_percent,omitempty"`
}

// ClusterQuotaCapacityFreedResponse is capacity that could be reclaimed by tightening CRQ.
type ClusterQuotaCapacityFreedResponse struct {
	CPUCoresFreed       int64 `json:"cpu_cores_freed"`
	MemoryBytes         int64 `json:"memory_bytes"`
	StorageRequestBytes int64 `json:"storage_request_bytes,omitempty"`
	PodsFreed           int64 `json:"pods_freed,omitempty"`
}

// ClusterQuotaRecommendationListResponse wraps cluster-quota list output.
type ClusterQuotaRecommendationListResponse struct {
	Meta struct {
		Count      int    `json:"count"`
		Limit      int    `json:"limit"`
		Offset     int    `json:"offset"`
		HasNext    bool   `json:"has_next"`
		NextCursor string `json:"next_cursor,omitempty"`
		Currency   string `json:"currency"`
	} `json:"meta"`
	Links Links                                `json:"links"`
	Data  []ClusterQuotaRecommendationListItem `json:"data"`
}

// ClusterQuotaRecommendationListItem is one CRQ recommendation row.
type ClusterQuotaRecommendationListItem struct {
	ID                   string                           `json:"id,omitempty"`
	ClusterUUID          string                           `json:"cluster_uuid"`
	ClusterQuotaName     string                           `json:"cluster_quota_name"`
	RecommendationType   string                           `json:"recommendation_type"`
	RiskLevel            string                           `json:"risk_level"`
	QuotaHard            *ClusterQuotaResourceValues      `json:"quota_hard,omitempty"`
	QuotaUsed            *ClusterQuotaResourceValues      `json:"quota_used,omitempty"`
	QuotaRecommended     *ClusterQuotaResourceValues      `json:"quota_recommended,omitempty"`
	Utilization          *ClusterQuotaUtilizationPercents `json:"utilization,omitempty"`
	CapacityFreed        *ClusterQuotaCapacityFreedResponse `json:"capacity_freed,omitempty"`
	EstimatedSavings     *money.MoneyAmount                         `json:"estimated_savings,omitempty"`
	Notifications        map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
	Namespaces           []string                                     `json:"namespaces,omitempty"`
	Count                int                                          `json:"count,omitempty"`

	rawSavingsCents    *int64 // raw DB value for cursor pagination; not serialized
	rawMaxUtilPercent  int64  // GREATEST of all utilization_*_percent columns; not serialized
}

// GetClusterQuotaRecommendations handles GET /recommendations/openshift/cluster-quota/.
func GetClusterQuotaRecommendations(c echo.Context) error {
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

	orderCol, orderDir, orderErr := queryparams.ParseOrderBy(c, clusterQuotaAllowedOrderBy, clusterQuotaDefaultOrderBy, quotaDefaultOrderHow)
	if orderErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": orderErr.Error()})
	}

	cursor, hasCursor, cursorErr := applyClusterQuotaCursor(c, orderCol)
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
	crqFilter := queryparams.FirstFilter(c, "cluster_quota_name")
	if crqFilter == "" {
		crqFilter = queryparams.FirstFilter(c, "cluster_resource_quota")
	}
	if crqFilter == "" {
		crqFilter = queryparams.FirstFilter(c, "crq")
	}
	typeFilter := queryparams.FirstFilter(c, "recommendation_type")
	riskFilter := queryparams.FirstFilter(c, "risk_level")
	namespaceFilter := queryparams.FirstFilter(c, "project")

	projection, projErr := resolveQuotaListProjection(c)
	if projErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": projErr.Error()})
	}

	ctx := c.Request().Context()
	filterSQL := ""
	args := []any{orgID}
	argIdx := 2

	if clusterFilter != "" {
		filterSQL += ` AND cluster_uuid = $` + strconv.Itoa(argIdx)
		args = append(args, clusterFilter)
		argIdx++
	}
	if crqFilter != "" {
		filterSQL += ` AND cluster_quota_name = $` + strconv.Itoa(argIdx)
		args = append(args, crqFilter)
		argIdx++
	}
	if typeFilter != "" {
		filterSQL += ` AND recommendation_type = $` + strconv.Itoa(argIdx)
		args = append(args, typeFilter)
		argIdx++
	}
	if riskFilter != "" {
		filterSQL += ` AND risk_level = $` + strconv.Itoa(argIdx)
		args = append(args, riskFilter)
		argIdx++
	}
	if namespaceFilter != "" {
		filterSQL += ` AND EXISTS (
			SELECT 1 FROM unnest(string_to_array(COALESCE(namespaces, ''), ',')) AS member(ns)
			WHERE trim(both ' ' from member.ns) = $` + strconv.Itoa(argIdx) + `
		)`
		args = append(args, namespaceFilter)
		argIdx++
	}

	if config.TagsFeatureEnabled() {
		tagFilters, tagErr := parseTagFiltersFromRequest(c)
		if tagErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": tagErr.Error()})
		}
		if len(tagFilters) > 0 {
			tagClause, tagArgs, nextIdx := model.TagFilterExistsClauseForCommaSeparatedNamespaces(
				orgID, "cluster_uuid", "namespaces", tagFilters, argIdx)
			if tagClause != "" {
				filterSQL += " AND " + tagClause
				args = append(args, tagArgs...)
				argIdx = nextIdx
			}
		}
	}

	groupByCluster := queryparams.GroupByField(c, "cluster")
	if groupByCluster {
		return getClusterQuotaRecommendationsGrouped(c, ctx, pool, hlog, orgID, filterSQL, args, argIdx, limit, offset, responseFormat, cursor, hasCursor)
	}

	countQuery := `SELECT COUNT(*) FROM cluster_quota_recommendation_sets WHERE org_id = $1` + filterSQL
	var total int
	if err := pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		hlog.Errorf("cluster-quota recommendation count failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to count cluster-quota recommendations"})
	}

	query := `
		SELECT cluster_uuid, cluster_quota_name, recommendation_type, risk_level,
			cpu_request_hard, cpu_limit_hard,
			memory_request_hard, memory_limit_hard,
			cpu_request_used, cpu_limit_used,
			memory_request_used, memory_limit_used,
			cpu_request_recommended, cpu_limit_recommended,
			memory_request_recommended, memory_limit_recommended,
			storage_request_hard, storage_request_used, storage_request_recommended,
			pods_hard, pods_used, pods_recommended,
			utilization_cpu_request_percent, utilization_memory_request_percent,
			utilization_storage_request_percent, utilization_pods_percent,
			savings_cpu_cores_freed, savings_memory_bytes_freed,
			savings_storage_bytes_freed, savings_pods_freed,
			estimated_savings_cents, notification_codes, namespaces
		FROM cluster_quota_recommendation_sets
		WHERE org_id = $1` + filterSQL

	if hasCursor {
		seekSQL, seekArgs, nextIdx, seekErr := clusterQuotaSeekSQL(orderCol, orderDir, cursor, cursor.OrderBy != "", argIdx)
		if seekErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": seekErr.Error()})
		}
		query += ` AND ` + seekSQL
		args = append(args, seekArgs...)
		argIdx = nextIdx
	}

	query += ` ORDER BY ` + clusterQuotaOrderNulls(orderCol, orderDir) +
		`, cluster_uuid ASC, cluster_quota_name ASC`

	pageLimit := limit
	if pageLimit > 0 {
		pageLimit++
	}
	query += ` LIMIT $` + strconv.Itoa(argIdx)
	pageArgs := append(args, pageLimit)
	argIdx++

	if !hasCursor {
		query += ` OFFSET $` + strconv.Itoa(argIdx)
		pageArgs = append(pageArgs, offset)
	}

	rows, err := pool.Query(ctx, query, pageArgs...)
	if err != nil {
		hlog.Errorf("cluster-quota recommendation query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to fetch cluster-quota recommendations"})
	}
	defer rows.Close()

	currency := resolveListCurrencyFromRequest(c, orgID)
	var data []ClusterQuotaRecommendationListItem
	for rows.Next() {
		item, scanErr := scanClusterQuotaListItem(rows, currency)
		if scanErr != nil {
			hlog.Errorf("scanning cluster-quota recommendation: %v", scanErr)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to read cluster-quota recommendations"})
		}
		data = append(data, item)
	}
	if err := rows.Err(); err != nil {
		hlog.Errorf("cluster-quota recommendation iteration failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to fetch cluster-quota recommendations"})
	}

	if projection.reproject {
		cfg, cfgErr := engine.ResolveQuotaRecConfig(ctx, pool, orgID)
		if cfgErr != nil {
			hlog.Errorf("resolve quota settings: %v", cfgErr)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to resolve quota settings"})
		}
		var reprojErr error
		data, reprojErr = applyClusterQuotaListReprojection(ctx, pool, orgID, cfg, projection, data)
		if reprojErr != nil {
			hlog.Errorf("cluster-quota list reprojection failed: %v", reprojErr)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to project cluster-quota recommendations"})
		}
	}

	hasNext := false
	var nextCursor string
	if limit > 0 && len(data) > limit {
		hasNext = true
		last := data[limit-1]
		nextCursor = clusterQuotaNextCursor(orderCol, last, clusterQuotaSortValue(last, orderCol))
		data = data[:limit]
	}

	resp := ClusterQuotaRecommendationListResponse{}
	resp.Meta.Count = total
	resp.Meta.Limit = limit
	resp.Meta.Offset = offset
	resp.Meta.HasNext = hasNext
	resp.Meta.NextCursor = nextCursor
	resp.Meta.Currency = resolveListCurrencyFromRequest(c, orgID)
	resp.Links = buildLinks(c.Request(), total, limit, offset)
	finalizeListLinks(&resp.Links, c.Request(), limit, hasNext, nextCursor)
	resp.Data = data
	if resp.Data == nil {
		resp.Data = []ClusterQuotaRecommendationListItem{}
	}
	if responseFormat == listoptions.ResponseFormatCSV {
		return streamCSV(c, csvFilename("cluster-quota-recommendations"), func(ctx context.Context, w io.Writer) error {
			return generateClusterQuotaRecCSV(ctx, w, resp.Data)
		})
	}
	return c.JSON(http.StatusOK, resp)
}

func getClusterQuotaRecommendationsGrouped(
	c echo.Context,
	ctx context.Context,
	pool *pgxpool.Pool,
	hlog *logrus.Entry,
	orgID string,
	filterSQL string,
	args []any,
	argIdx, limit, offset int,
	responseFormat string,
	cursor ClusterQuotaCursor,
	hasCursor bool,
) error {
	countQuery := `SELECT COUNT(DISTINCT cluster_uuid::text) FROM cluster_quota_recommendation_sets WHERE org_id = $1` + filterSQL
	var total int
	if err := pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		hlog.Errorf("cluster-quota recommendation group count failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to count cluster-quota recommendation groups"})
	}

	innerQuery := `
		SELECT cluster_uuid::text AS group_key,
			COUNT(*) AS row_count,
			COALESCE(SUM(savings_cpu_cores_freed), 0) AS cpu_cores_freed,
			COALESCE(SUM(savings_memory_bytes_freed), 0) AS mem_freed,
			COALESCE(SUM(savings_storage_bytes_freed), 0) AS storage_freed,
			COALESCE(SUM(savings_pods_freed), 0) AS pods_freed,
			COALESCE(SUM(estimated_savings_cents), 0) AS savings_cents
		FROM cluster_quota_recommendation_sets
		WHERE org_id = $1` + filterSQL + `
		GROUP BY cluster_uuid::text`

	query := `SELECT group_key, row_count, cpu_cores_freed, mem_freed, storage_freed, pods_freed, savings_cents
		FROM (` + innerQuery + `) crq_groups`

	if hasCursor && cursor.GroupKey != "" {
		query += ` WHERE group_key > $` + strconv.Itoa(argIdx)
		args = append(args, cursor.GroupKey)
		argIdx++
	}

	query += ` ORDER BY group_key ASC`

	pageLimit := limit
	if pageLimit > 0 {
		pageLimit++
	}
	query += ` LIMIT $` + strconv.Itoa(argIdx)
	pageArgs := append(args, pageLimit)
	argIdx++

	if !hasCursor {
		query += ` OFFSET $` + strconv.Itoa(argIdx)
		pageArgs = append(pageArgs, offset)
	}

	rows, err := pool.Query(ctx, query, pageArgs...)
	if err != nil {
		hlog.Errorf("cluster-quota recommendation group query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to fetch cluster-quota recommendation groups"})
	}
	defer rows.Close()

	var data []ClusterQuotaRecommendationListItem
	for rows.Next() {
		var groupKey string
		var count int
		var cpuCoresFreed, memFreed, storageFreed, podsFreed, savingsCents int64
		if err := rows.Scan(&groupKey, &count, &cpuCoresFreed, &memFreed, &storageFreed, &podsFreed, &savingsCents); err != nil {
			return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to read cluster-quota recommendation groups"})
		}
		item := ClusterQuotaRecommendationListItem{
			ClusterUUID: groupKey,
			Count:       count,
		}
		if cpuCoresFreed > 0 || memFreed > 0 || storageFreed > 0 || podsFreed > 0 {
			item.CapacityFreed = &ClusterQuotaCapacityFreedResponse{
				CPUCoresFreed:       cpuCoresFreed,
				MemoryBytes:         memFreed,
				StorageRequestBytes: storageFreed,
				PodsFreed:           podsFreed,
			}
		}
		if savingsCents > 0 {
			currency := resolveListCurrencyFromRequest(c, orgID)
			item.EstimatedSavings = money.FormatCentsToAmountPtr(&savingsCents, currency)
		}
		data = append(data, item)
	}

	hasNext := false
	var nextCursor string
	if limit > 0 && len(data) > limit {
		hasNext = true
		last := data[limit-1]
		nextCursor = clusterQuotaGroupNextCursor(last.ClusterUUID)
		data = data[:limit]
	}

	resp := ClusterQuotaRecommendationListResponse{}
	resp.Meta.Count = total
	resp.Meta.Limit = limit
	resp.Meta.Offset = offset
	resp.Meta.HasNext = hasNext
	resp.Meta.NextCursor = nextCursor
	resp.Meta.Currency = resolveListCurrencyFromRequest(c, orgID)
	resp.Links = buildLinks(c.Request(), total, limit, offset)
	finalizeListLinks(&resp.Links, c.Request(), limit, hasNext, nextCursor)
	resp.Data = data
	if resp.Data == nil {
		resp.Data = []ClusterQuotaRecommendationListItem{}
	}
	if responseFormat == listoptions.ResponseFormatCSV {
		return streamCSV(c, csvFilename("cluster-quota-recommendations"), func(ctx context.Context, w io.Writer) error {
			return generateClusterQuotaRecCSV(ctx, w, resp.Data)
		})
	}
	return c.JSON(http.StatusOK, resp)
}

type clusterQuotaRowScanner interface {
	Scan(dest ...any) error
}

func scanClusterQuotaListItem(rows clusterQuotaRowScanner, currency string) (ClusterQuotaRecommendationListItem, error) {
	var item ClusterQuotaRecommendationListItem
	var cpuReqHard, cpuLimHard, memReqHard, memLimHard sql.NullInt64
	var cpuReqUsed, cpuLimUsed, memReqUsed, memLimUsed sql.NullInt64
	var cpuReqRec, cpuLimRec, memReqRec, memLimRec sql.NullInt64
	var storageHard, storageUsed, storageRec sql.NullInt64
	var podsHard, podsUsed, podsRec sql.NullInt64
	var cpuReqUtil, memReqUtil, storageUtil, podsUtil sql.NullInt64
	var cpuCoresFreed, memFreed, storageFreed, podsFreed sql.NullInt64
	var savings sql.NullInt64
	var notifCodes []int16
	var namespacesRaw sql.NullString

	err := rows.Scan(
		&item.ClusterUUID, &item.ClusterQuotaName, &item.RecommendationType, &item.RiskLevel,
		&cpuReqHard, &cpuLimHard, &memReqHard, &memLimHard,
		&cpuReqUsed, &cpuLimUsed, &memReqUsed, &memLimUsed,
		&cpuReqRec, &cpuLimRec, &memReqRec, &memLimRec,
		&storageHard, &storageUsed, &storageRec,
		&podsHard, &podsUsed, &podsRec,
		&cpuReqUtil, &memReqUtil, &storageUtil, &podsUtil,
		&cpuCoresFreed, &memFreed, &storageFreed, &podsFreed, &savings, &notifCodes, &namespacesRaw,
	)
	if err != nil {
		return item, err
	}

	item.QuotaHard = clusterQuotaValuesFromNull(cpuReqHard, cpuLimHard, memReqHard, memLimHard, storageHard, podsHard)
	item.QuotaUsed = clusterQuotaValuesFromNull(cpuReqUsed, cpuLimUsed, memReqUsed, memLimUsed, storageUsed, podsUsed)
	item.QuotaRecommended = clusterQuotaValuesFromNull(cpuReqRec, cpuLimRec, memReqRec, memLimRec, storageRec, podsRec)
	item.Utilization = clusterQuotaUtilFromNull(cpuReqUtil, memReqUtil, storageUtil, podsUtil)
	item.rawMaxUtilPercent = maxNullInt64(cpuReqUtil, memReqUtil, storageUtil, podsUtil)
	if cpuCoresFreed.Valid || memFreed.Valid || storageFreed.Valid || podsFreed.Valid {
		item.CapacityFreed = &ClusterQuotaCapacityFreedResponse{
			CPUCoresFreed:       nullInt64Val(cpuCoresFreed),
			MemoryBytes:         nullInt64Val(memFreed),
			StorageRequestBytes: nullInt64Val(storageFreed),
			PodsFreed:           nullInt64Val(podsFreed),
		}
	}
	if savings.Valid && savings.Int64 > 0 {
		v := savings.Int64
		item.rawSavingsCents = &v
		item.EstimatedSavings = money.FormatCentsToAmountPtr(&v, currency)
	}
	item.Notifications = notifications.MapToKruizeFormat(notifCodes)
	item.Namespaces = clusterQuotaNamespacesFromDB(namespacesRaw)
	if item.Count == 0 && item.ClusterUUID != "" && item.ClusterQuotaName != "" {
		item.ID = model.NativeClusterQuotaID(item.ClusterUUID, item.ClusterQuotaName)
	}
	return item, nil
}

func clusterQuotaNamespacesFromDB(raw sql.NullString) []string {
	if !raw.Valid {
		return nil
	}
	parts := strings.Split(raw.String, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func clusterQuotaValuesFromNull(cpuReq, cpuLim, memReq, memLim, storage, pods sql.NullInt64) *ClusterQuotaResourceValues {
	if !cpuReq.Valid && !cpuLim.Valid && !memReq.Valid && !memLim.Valid && !storage.Valid && !pods.Valid {
		return nil
	}
	return &ClusterQuotaResourceValues{
		CPURequestMillicores: nullInt64Ptr(cpuReq),
		CPULimitMillicores:   nullInt64Ptr(cpuLim),
		MemoryRequestBytes:   nullInt64Ptr(memReq),
		MemoryLimitBytes:     nullInt64Ptr(memLim),
		StorageRequestBytes:  nullInt64Ptr(storage),
		Pods:                 nullInt64Ptr(pods),
	}
}

func clusterQuotaUtilFromNull(cpuReq, memReq, storage, pods sql.NullInt64) *ClusterQuotaUtilizationPercents {
	if !cpuReq.Valid && !memReq.Valid && !storage.Valid && !pods.Valid {
		return nil
	}
	return &ClusterQuotaUtilizationPercents{
		CPURequestPercent:     intPercentToFloat64Ptr(cpuReq),
		MemoryRequestPercent:  intPercentToFloat64Ptr(memReq),
		StorageRequestPercent: intPercentToFloat64Ptr(storage),
		PodsPercent:           intPercentToFloat64Ptr(pods),
	}
}

func intPercentToFloat64Ptr(v sql.NullInt64) *float64 {
	if !v.Valid {
		return nil
	}
	pct := float64(v.Int64)
	return &pct
}
