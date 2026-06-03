package api

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// QuotaResourceValues groups quota metrics for API responses.
type QuotaResourceValues struct {
	CPURequestMillicores *int64 `json:"cpu_request_millicores,omitempty"`
	CPULimitMillicores   *int64 `json:"cpu_limit_millicores,omitempty"`
	MemoryRequestBytes   *int64 `json:"memory_request_bytes,omitempty"`
	MemoryLimitBytes     *int64 `json:"memory_limit_bytes,omitempty"`
	StorageRequestBytes  *int64 `json:"storage_request_bytes,omitempty"`
	Pods                 *int64 `json:"pods,omitempty"`
}

// QuotaUtilizationPercents exposes utilization as human-readable percentages.
type QuotaUtilizationPercents struct {
	CPURequestPercent    *float64 `json:"cpu_request_percent,omitempty"`
	CPULimitPercent      *float64 `json:"cpu_limit_percent,omitempty"`
	MemoryRequestPercent *float64 `json:"memory_request_percent,omitempty"`
	MemoryLimitPercent   *float64 `json:"memory_limit_percent,omitempty"`
	StorageRequestPercent *float64 `json:"storage_request_percent,omitempty"`
	PodsPercent          *float64 `json:"pods_percent,omitempty"`
}

// QuotaCapacityFreedResponse is capacity that could be reclaimed by tightening quota.
type QuotaCapacityFreedResponse struct {
	CPUMillicores       int64 `json:"cpu_millicores"`
	MemoryBytes         int64 `json:"memory_bytes"`
	StorageRequestBytes int64 `json:"storage_request_bytes,omitempty"`
	PodsFreed           int64 `json:"pods_freed,omitempty"`
}

// QuotaRecommendationListResponse wraps quota recommendation list output.
type QuotaRecommendationListResponse struct {
	Meta struct {
		Count    int      `json:"count"`
		Limit    int      `json:"limit"`
		Offset   int      `json:"offset"`
		Currency string   `json:"currency"`
		Warnings []string `json:"warnings,omitempty"`
	} `json:"meta"`
	Links Links                           `json:"links"`
	Data  []QuotaRecommendationListItem   `json:"data"`
}

// QuotaRecommendationListItem is either a namespace row or a grouped aggregate.
type QuotaRecommendationListItem struct {
	ClusterUUID        string                      `json:"cluster_uuid,omitempty"`
	Namespace          string                      `json:"namespace,omitempty"`
	QuotaName          string                      `json:"quota_name,omitempty"`
	RecommendationType string                      `json:"recommendation_type,omitempty"`
	RiskLevel          string                      `json:"risk_level,omitempty"`
	QuotaHard          *QuotaResourceValues        `json:"quota_hard,omitempty"`
	QuotaUsed          *QuotaResourceValues        `json:"quota_used,omitempty"`
	QuotaRecommended   *QuotaResourceValues        `json:"quota_recommended,omitempty"`
	Utilization        *QuotaUtilizationPercents   `json:"utilization,omitempty"`
	CapacityFreed      *QuotaCapacityFreedResponse `json:"capacity_freed,omitempty"`
	EstimatedSavings   *money.SavingsObject        `json:"estimated_savings,omitempty"`
	LastObservedAt     string                                      `json:"last_observed_at,omitempty"`
	Notifications      map[string]notifications.NotificationEntry  `json:"notifications,omitempty"`
	Count              int                                         `json:"count,omitempty"`
}

// GetQuotaRecommendations handles GET /recommendations/openshift/quota/.
func GetQuotaRecommendations(c echo.Context) error {
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

	clusterFilter := queryparams.FirstFilter(c, "cluster")
	namespaceFilter := queryparams.FirstFilter(c, "project")
	quotaNameFilter := queryparams.FirstFilter(c, "quota_name")
	if quotaNameFilter == "" {
		quotaNameFilter = queryparams.FirstFilter(c, "resource_quota_name")
	}
	typeFilter := queryparams.FirstFilter(c, "recommendation_type")
	riskFilter := queryparams.FirstFilter(c, "risk_level")

	groupByCluster := queryparams.GroupByField(c, "cluster")
	groupByProject := queryparams.GroupByField(c, "project")
	if groupByCluster && groupByProject {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": "group_by[cluster] and group_by[project] cannot be used together",
		})
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
	if namespaceFilter != "" {
		filterSQL += ` AND namespace = $` + strconv.Itoa(argIdx)
		args = append(args, namespaceFilter)
		argIdx++
	}
	if quotaNameFilter != "" {
		filterSQL += ` AND quota_name = $` + strconv.Itoa(argIdx)
		args = append(args, quotaNameFilter)
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

	if config.TagsFeatureEnabled() {
		tagFilters, tagErr := parseTagFiltersFromRequest(c)
		if tagErr != nil {
			return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": tagErr.Error()})
		}
		if len(tagFilters) > 0 {
			tagClause, tagArgs, nextIdx := model.TagFilterExistsClause(
				orgID, "quota_recommendation_sets.cluster_uuid", "quota_recommendation_sets.namespace", tagFilters, argIdx)
			if tagClause != "" {
				filterSQL += " AND " + tagClause
				args = append(args, tagArgs...)
				argIdx = nextIdx
			}
		}
	}

	if groupByCluster || groupByProject {
		return getQuotaRecommendationsGrouped(c, ctx, pool, hlog, orgID, filterSQL, args, argIdx, limit, offset, groupByCluster, clusterFilter)
	}

	orderCol, orderDir, orderErr := queryparams.ParseOrderBy(c, quotaAllowedOrderBy, quotaDefaultOrderBy, quotaDefaultOrderHow)
	if orderErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": orderErr.Error()})
	}

	countQuery := `SELECT COUNT(*) FROM quota_recommendation_sets WHERE org_id = $1` + filterSQL
	var total int
	if err := pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		hlog.Errorf("quota recommendation count failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to count quota recommendations"})
	}

	query := `
		SELECT cluster_uuid, namespace, quota_name, recommendation_type, risk_level,
			cpu_request_hard_millicores, cpu_limit_hard_millicores,
			memory_request_hard_bytes, memory_limit_hard_bytes,
			cpu_request_used_millicores, cpu_limit_used_millicores,
			memory_request_used_bytes, memory_limit_used_bytes,
			storage_request_hard_bytes, storage_request_used_bytes,
			storage_request_recommended_bytes,
			pods_hard, pods_used, pods_recommended,
			cpu_request_recommended_millicores, cpu_limit_recommended_millicores,
			memory_request_recommended_bytes, memory_limit_recommended_bytes,
			cpu_request_utilization_bp, cpu_limit_utilization_bp,
			memory_request_utilization_bp, memory_limit_utilization_bp,
			utilization_storage_request_bp, utilization_pods_bp,
			cpu_freed_millicores, memory_freed_bytes,
			storage_freed_bytes, pods_freed,
			estimated_savings_cents, currency, notification_codes, last_observed_at
		FROM quota_recommendation_sets
		WHERE org_id = $1` + filterSQL +
		` ORDER BY ` + orderCol + ` ` + orderDir + ` LIMIT $` + strconv.Itoa(argIdx) + ` OFFSET $` + strconv.Itoa(argIdx+1)
	pageArgs := append(args, limit, offset)

	rows, err := pool.Query(ctx, query, pageArgs...)
	if err != nil {
		hlog.Errorf("quota recommendation query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to fetch quota recommendations"})
	}
	defer rows.Close()

	var data []QuotaRecommendationListItem
	for rows.Next() {
		item, scanErr := scanQuotaListItem(rows)
		if scanErr != nil {
			hlog.Errorf("scanning quota recommendation: %v", scanErr)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to read quota recommendations"})
		}
		data = append(data, item)
	}
	if err := rows.Err(); err != nil {
		hlog.Errorf("quota recommendation iteration failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to fetch quota recommendations"})
	}

	resp := QuotaRecommendationListResponse{}
	resp.Meta.Count = total
	resp.Meta.Limit = limit
	resp.Meta.Offset = offset
	resp.Meta.Currency = fetchClusterCurrency(ctx, orgID, clusterFilter)
	resp.Links = buildLinks(c.Request(), total, limit, offset)
	resp.Data = data
	if resp.Data == nil {
		resp.Data = []QuotaRecommendationListItem{}
	}
	return c.JSON(http.StatusOK, resp)
}

func getQuotaRecommendationsGrouped(
	c echo.Context,
	ctx context.Context,
	pool *pgxpool.Pool,
	hlog *logrus.Entry,
	orgID, filterSQL string,
	args []any,
	argIdx, limit, offset int,
	groupByCluster bool,
	clusterFilter string,
) error {
	groupCol := "namespace"
	orderCol := "namespace"
	if groupByCluster {
		groupCol = "cluster_uuid::text"
		orderCol = "cluster_uuid::text"
	}

	countQuery := `SELECT COUNT(DISTINCT ` + groupCol + `) FROM quota_recommendation_sets WHERE org_id = $1` + filterSQL
	var total int
	if err := pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		hlog.Errorf("quota recommendation group count failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to count quota recommendation groups"})
	}

	query := `
		SELECT ` + groupCol + ` AS group_key,
			COUNT(*) AS row_count,
			COALESCE(SUM(cpu_freed_millicores), 0),
			COALESCE(SUM(memory_freed_bytes), 0),
			COALESCE(SUM(storage_freed_bytes), 0),
			COALESCE(SUM(pods_freed), 0),
			COALESCE(SUM(estimated_savings_cents), 0),
			MAX(currency),
			MAX(last_observed_at)
		FROM quota_recommendation_sets
		WHERE org_id = $1` + filterSQL + `
		GROUP BY ` + groupCol + `
		ORDER BY ` + orderCol + `
		LIMIT $` + strconv.Itoa(argIdx) + ` OFFSET $` + strconv.Itoa(argIdx+1)
	pageArgs := append(args, limit, offset)

	rows, err := pool.Query(ctx, query, pageArgs...)
	if err != nil {
		hlog.Errorf("quota recommendation group query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to fetch quota recommendation groups"})
	}
	defer rows.Close()

	var data []QuotaRecommendationListItem
	for rows.Next() {
		var groupKey string
		var count int
		var cpuFreed, memFreed, storageFreed, podsFreed, savingsCents int64
		var currency string
		var lastObserved sql.NullTime
		if err := rows.Scan(&groupKey, &count, &cpuFreed, &memFreed, &storageFreed, &podsFreed, &savingsCents, &currency, &lastObserved); err != nil {
			return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to read quota recommendation groups"})
		}
		item := QuotaRecommendationListItem{
			Count: count,
			CapacityFreed: quotaCapacityFreedFromTotals(cpuFreed, memFreed, storageFreed, podsFreed),
			LastObservedAt: lastObserved.Time.UTC().Format(time.RFC3339),
		}
		if groupByCluster {
			item.ClusterUUID = groupKey
		} else {
			item.Namespace = groupKey
		}
		if savingsCents > 0 {
			item.EstimatedSavings = money.FormatCentsToSavingsPtr(&savingsCents, currency)
		}
		data = append(data, item)
	}

	resp := QuotaRecommendationListResponse{}
	resp.Meta.Count = total
	resp.Meta.Limit = limit
	resp.Meta.Offset = offset
	resp.Meta.Currency = fetchClusterCurrency(ctx, orgID, clusterFilter)
	resp.Links = buildLinks(c.Request(), total, limit, offset)
	resp.Data = data
	if resp.Data == nil {
		resp.Data = []QuotaRecommendationListItem{}
	}
	return c.JSON(http.StatusOK, resp)
}

type quotaRowScanner interface {
	Scan(dest ...any) error
}

func scanQuotaListItem(rows quotaRowScanner) (QuotaRecommendationListItem, error) {
	var item QuotaRecommendationListItem
	var cpuReqHard, cpuLimHard, memReqHard, memLimHard sql.NullInt64
	var cpuReqUsed, cpuLimUsed, memReqUsed, memLimUsed sql.NullInt64
	var storageHard, storageUsed, storageRec sql.NullInt64
	var podsHard, podsUsed, podsRec sql.NullInt64
	var cpuReqRec, cpuLimRec, memReqRec, memLimRec sql.NullInt64
	var cpuReqUtil, cpuLimUtil, memReqUtil, memLimUtil sql.NullInt64
	var storageUtil, podsUtil sql.NullInt64
	var cpuFreed, memFreed, storageFreed, podsFreed sql.NullInt64
	var savings sql.NullInt64
	var currency string
	var notifCodes []int16
	var lastObserved sql.NullTime

	err := rows.Scan(
		&item.ClusterUUID, &item.Namespace, &item.QuotaName, &item.RecommendationType, &item.RiskLevel,
		&cpuReqHard, &cpuLimHard, &memReqHard, &memLimHard,
		&cpuReqUsed, &cpuLimUsed, &memReqUsed, &memLimUsed,
		&storageHard, &storageUsed, &storageRec,
		&podsHard, &podsUsed, &podsRec,
		&cpuReqRec, &cpuLimRec, &memReqRec, &memLimRec,
		&cpuReqUtil, &cpuLimUtil, &memReqUtil, &memLimUtil,
		&storageUtil, &podsUtil,
		&cpuFreed, &memFreed, &storageFreed, &podsFreed,
		&savings, &currency, &notifCodes, &lastObserved,
	)
	if err != nil {
		return item, err
	}

	item.QuotaHard = quotaValuesFromNullExtended(cpuReqHard, cpuLimHard, memReqHard, memLimHard, storageHard, podsHard)
	item.QuotaUsed = quotaValuesFromNullExtended(cpuReqUsed, cpuLimUsed, memReqUsed, memLimUsed, storageUsed, podsUsed)
	item.QuotaRecommended = quotaValuesFromNullExtended(cpuReqRec, cpuLimRec, memReqRec, memLimRec, storageRec, podsRec)
	item.Utilization = quotaUtilFromNullBP(cpuReqUtil, cpuLimUtil, memReqUtil, memLimUtil, storageUtil, podsUtil)
	item.CapacityFreed = quotaCapacityFreedFromNull(cpuFreed, memFreed, storageFreed, podsFreed)
	if savings.Valid {
		item.EstimatedSavings = money.FormatCentsToSavingsPtr(&savings.Int64, currency)
	}
	if lastObserved.Valid {
		item.LastObservedAt = lastObserved.Time.UTC().Format(time.RFC3339)
	}
	item.Notifications = notifications.MapToKruizeFormat(notifCodes)
	return item, nil
}

func quotaValuesFromNull(cpuReq, cpuLim, memReq, memLim sql.NullInt64) *QuotaResourceValues {
	return quotaValuesFromNullExtended(cpuReq, cpuLim, memReq, memLim, sql.NullInt64{}, sql.NullInt64{})
}

func quotaValuesFromNullExtended(cpuReq, cpuLim, memReq, memLim, storage, pods sql.NullInt64) *QuotaResourceValues {
	if !cpuReq.Valid && !cpuLim.Valid && !memReq.Valid && !memLim.Valid && !storage.Valid && !pods.Valid {
		return nil
	}
	return &QuotaResourceValues{
		CPURequestMillicores: nullInt64Ptr(cpuReq),
		CPULimitMillicores:   nullInt64Ptr(cpuLim),
		MemoryRequestBytes:   nullInt64Ptr(memReq),
		MemoryLimitBytes:     nullInt64Ptr(memLim),
		StorageRequestBytes:  nullInt64Ptr(storage),
		Pods:                 nullInt64Ptr(pods),
	}
}

func quotaCapacityFreedFromNull(cpuFreed, memFreed, storageFreed, podsFreed sql.NullInt64) *QuotaCapacityFreedResponse {
	if !cpuFreed.Valid && !memFreed.Valid && !storageFreed.Valid && !podsFreed.Valid {
		return nil
	}
	return quotaCapacityFreedFromTotals(
		nullInt64Val(cpuFreed), nullInt64Val(memFreed),
		nullInt64Val(storageFreed), nullInt64Val(podsFreed),
	)
}

func quotaCapacityFreedFromTotals(cpuFreed, memFreed, storageFreed, podsFreed int64) *QuotaCapacityFreedResponse {
	if cpuFreed == 0 && memFreed == 0 && storageFreed == 0 && podsFreed == 0 {
		return nil
	}
	resp := &QuotaCapacityFreedResponse{
		CPUMillicores: cpuFreed,
		MemoryBytes:   memFreed,
	}
	if storageFreed > 0 {
		resp.StorageRequestBytes = storageFreed
	}
	if podsFreed > 0 {
		resp.PodsFreed = podsFreed
	}
	return resp
}

func quotaUtilFromNullBP(cpuReq, cpuLim, memReq, memLim, storage, pods sql.NullInt64) *QuotaUtilizationPercents {
	if !cpuReq.Valid && !cpuLim.Valid && !memReq.Valid && !memLim.Valid && !storage.Valid && !pods.Valid {
		return nil
	}
	return &QuotaUtilizationPercents{
		CPURequestPercent:     bpToPercentPtr(cpuReq),
		CPULimitPercent:       bpToPercentPtr(cpuLim),
		MemoryRequestPercent:  bpToPercentPtr(memReq),
		MemoryLimitPercent:    bpToPercentPtr(memLim),
		StorageRequestPercent: bpToPercentPtr(storage),
		PodsPercent:           bpToPercentPtr(pods),
	}
}

func nullInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func nullInt64Val(v sql.NullInt64) int64 {
	if !v.Valid {
		return 0
	}
	return v.Int64
}

func bpToPercentPtr(v sql.NullInt64) *float64 {
	if !v.Valid {
		return nil
	}
	pct := float64(v.Int64) / 100.0
	return &pct
}
