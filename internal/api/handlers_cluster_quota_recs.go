package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
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
	CPURequestPercent     *int `json:"cpu_request_percent,omitempty"`
	MemoryRequestPercent  *int `json:"memory_request_percent,omitempty"`
	StorageRequestPercent *int `json:"storage_request_percent,omitempty"`
	PodsPercent           *int `json:"pods_percent,omitempty"`
}

// ClusterQuotaCapacityFreedResponse is capacity that could be reclaimed by tightening CRQ.
type ClusterQuotaCapacityFreedResponse struct {
	CPUCoresFreed       int64 `json:"cpu_cores_freed"`
	MemoryBytes         int64 `json:"memory_bytes"`
	StorageRequestBytes int64 `json:"storage_request_bytes,omitempty"`
	PodsFreed           int64 `json:"pods_freed,omitempty"`
}

// ClusterQuotaSavingsMonthly is estimated monthly savings in whole dollars.
type ClusterQuotaSavingsMonthly struct {
	Value int `json:"value"`
	Units string `json:"units"`
}

// ClusterQuotaRecommendationListResponse wraps cluster-quota list output.
type ClusterQuotaRecommendationListResponse struct {
	Meta struct {
		Count  int `json:"count"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	} `json:"meta"`
	Links Links                                `json:"links"`
	Data  []ClusterQuotaRecommendationListItem `json:"data"`
}

// ClusterQuotaRecommendationListItem is one CRQ recommendation row.
type ClusterQuotaRecommendationListItem struct {
	ClusterUUID          string                           `json:"cluster_uuid"`
	ClusterQuotaName     string                           `json:"cluster_quota_name"`
	RecommendationType   string                           `json:"recommendation_type"`
	RiskLevel            string                           `json:"risk_level"`
	QuotaHard            *ClusterQuotaResourceValues      `json:"quota_hard,omitempty"`
	QuotaUsed            *ClusterQuotaResourceValues      `json:"quota_used,omitempty"`
	QuotaRecommended     *ClusterQuotaResourceValues      `json:"quota_recommended,omitempty"`
	Utilization          *ClusterQuotaUtilizationPercents `json:"utilization,omitempty"`
	CapacityFreed        *ClusterQuotaCapacityFreedResponse `json:"capacity_freed,omitempty"`
	EstimatedSavings     *ClusterQuotaSavingsMonthly                  `json:"estimated_savings,omitempty"`
	Notifications        map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
	Namespaces           []string                                     `json:"namespaces,omitempty"`
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
	namespaceFilter := queryparams.FirstFilter(c, "namespace")
	if namespaceFilter == "" {
		namespaceFilter = queryparams.FirstFilter(c, "project")
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

	orderCol, orderDir, orderErr := queryparams.ParseOrderBy(c, clusterQuotaAllowedOrderBy, clusterQuotaDefaultOrderBy, quotaDefaultOrderHow)
	if orderErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"status": "error", "message": orderErr.Error()})
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
			savings_dollars_monthly, notification_codes, namespaces
		FROM cluster_quota_recommendation_sets
		WHERE org_id = $1` + filterSQL +
		` ORDER BY ` + orderCol + ` ` + orderDir + ` LIMIT $` + strconv.Itoa(argIdx) + ` OFFSET $` + strconv.Itoa(argIdx+1)
	pageArgs := append(args, limit, offset)

	rows, err := pool.Query(ctx, query, pageArgs...)
	if err != nil {
		hlog.Errorf("cluster-quota recommendation query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{"status": "error", "message": "unable to fetch cluster-quota recommendations"})
	}
	defer rows.Close()

	var data []ClusterQuotaRecommendationListItem
	for rows.Next() {
		item, scanErr := scanClusterQuotaListItem(rows)
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

	resp := ClusterQuotaRecommendationListResponse{}
	resp.Meta.Count = total
	resp.Meta.Limit = limit
	resp.Meta.Offset = offset
	resp.Links = buildLinks(c.Request(), total, limit, offset)
	resp.Data = data
	if resp.Data == nil {
		resp.Data = []ClusterQuotaRecommendationListItem{}
	}
	return c.JSON(http.StatusOK, resp)
}

type clusterQuotaRowScanner interface {
	Scan(dest ...any) error
}

func scanClusterQuotaListItem(rows clusterQuotaRowScanner) (ClusterQuotaRecommendationListItem, error) {
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
	if cpuCoresFreed.Valid || memFreed.Valid || storageFreed.Valid || podsFreed.Valid {
		item.CapacityFreed = &ClusterQuotaCapacityFreedResponse{
			CPUCoresFreed:       nullInt64Val(cpuCoresFreed),
			MemoryBytes:         nullInt64Val(memFreed),
			StorageRequestBytes: nullInt64Val(storageFreed),
			PodsFreed:           nullInt64Val(podsFreed),
		}
	}
	if savings.Valid && savings.Int64 > 0 {
		item.EstimatedSavings = &ClusterQuotaSavingsMonthly{
			Value: int(savings.Int64),
			Units: "USD",
		}
	}
	item.Notifications = notifications.MapToKruizeFormat(notifCodes)
	item.Namespaces = clusterQuotaNamespacesFromDB(namespacesRaw)
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
	out := &ClusterQuotaUtilizationPercents{}
	if cpuReq.Valid {
		v := int(cpuReq.Int64)
		out.CPURequestPercent = &v
	}
	if memReq.Valid {
		v := int(memReq.Int64)
		out.MemoryRequestPercent = &v
	}
	if storage.Valid {
		v := int(storage.Int64)
		out.StorageRequestPercent = &v
	}
	if pods.Valid {
		v := int(pods.Int64)
		out.PodsPercent = &v
	}
	return out
}
