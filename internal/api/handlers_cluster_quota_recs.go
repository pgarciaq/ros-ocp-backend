package api

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
)

// ClusterQuotaResourceValues groups quota metrics for cluster-quota API responses.
type ClusterQuotaResourceValues struct {
	CPURequestMillicores *int64 `json:"cpu_request_millicores,omitempty"`
	CPULimitMillicores   *int64 `json:"cpu_limit_millicores,omitempty"`
	MemoryRequestBytes   *int64 `json:"memory_request_bytes,omitempty"`
	MemoryLimitBytes     *int64 `json:"memory_limit_bytes,omitempty"`
}

// ClusterQuotaUtilizationPercents exposes utilization as human-readable percentages.
type ClusterQuotaUtilizationPercents struct {
	CPURequestPercent    *int `json:"cpu_request_percent,omitempty"`
	MemoryRequestPercent *int `json:"memory_request_percent,omitempty"`
}

// ClusterQuotaCapacityFreedResponse is capacity that could be reclaimed by tightening CRQ.
type ClusterQuotaCapacityFreedResponse struct {
	CPUCoresFreed int64 `json:"cpu_cores_freed"`
	MemoryBytes   int64 `json:"memory_bytes"`
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
	EstimatedSavings     *ClusterQuotaSavingsMonthly      `json:"estimated_savings,omitempty"`
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
			utilization_cpu_request_percent, utilization_memory_request_percent,
			savings_cpu_cores_freed, savings_memory_bytes_freed,
			savings_dollars_monthly
		FROM cluster_quota_recommendation_sets
		WHERE org_id = $1` + filterSQL +
		` ORDER BY cluster_quota_name LIMIT $` + strconv.Itoa(argIdx) + ` OFFSET $` + strconv.Itoa(argIdx+1)
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
	var cpuReqUtil, memReqUtil sql.NullInt64
	var cpuCoresFreed, memFreed sql.NullInt64
	var savings sql.NullInt64

	err := rows.Scan(
		&item.ClusterUUID, &item.ClusterQuotaName, &item.RecommendationType, &item.RiskLevel,
		&cpuReqHard, &cpuLimHard, &memReqHard, &memLimHard,
		&cpuReqUsed, &cpuLimUsed, &memReqUsed, &memLimUsed,
		&cpuReqRec, &cpuLimRec, &memReqRec, &memLimRec,
		&cpuReqUtil, &memReqUtil,
		&cpuCoresFreed, &memFreed, &savings,
	)
	if err != nil {
		return item, err
	}

	item.QuotaHard = clusterQuotaValuesFromNull(cpuReqHard, cpuLimHard, memReqHard, memLimHard)
	item.QuotaUsed = clusterQuotaValuesFromNull(cpuReqUsed, cpuLimUsed, memReqUsed, memLimUsed)
	item.QuotaRecommended = clusterQuotaValuesFromNull(cpuReqRec, cpuLimRec, memReqRec, memLimRec)
	item.Utilization = clusterQuotaUtilFromNull(cpuReqUtil, memReqUtil)
	if cpuCoresFreed.Valid || memFreed.Valid {
		item.CapacityFreed = &ClusterQuotaCapacityFreedResponse{
			CPUCoresFreed: nullInt64Val(cpuCoresFreed),
			MemoryBytes:   nullInt64Val(memFreed),
		}
	}
	if savings.Valid && savings.Int64 > 0 {
		item.EstimatedSavings = &ClusterQuotaSavingsMonthly{
			Value: int(savings.Int64),
			Units: "USD",
		}
	}
	return item, nil
}

func clusterQuotaValuesFromNull(cpuReq, cpuLim, memReq, memLim sql.NullInt64) *ClusterQuotaResourceValues {
	if !cpuReq.Valid && !cpuLim.Valid && !memReq.Valid && !memLim.Valid {
		return nil
	}
	return &ClusterQuotaResourceValues{
		CPURequestMillicores: nullInt64Ptr(cpuReq),
		CPULimitMillicores:   nullInt64Ptr(cpuLim),
		MemoryRequestBytes:   nullInt64Ptr(memReq),
		MemoryLimitBytes:     nullInt64Ptr(memLim),
	}
}

func clusterQuotaUtilFromNull(cpuReq, memReq sql.NullInt64) *ClusterQuotaUtilizationPercents {
	if !cpuReq.Valid && !memReq.Valid {
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
	return out
}
