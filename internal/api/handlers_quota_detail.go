package api

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/api/queryparams"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// QuotaRecommendationDetailResponse is the full quota recommendation for one namespace.
type QuotaRecommendationDetailResponse struct {
	ClusterUUID        string                                      `json:"cluster_uuid"`
	Namespace          string                                      `json:"namespace"`
	QuotaName          string                                      `json:"quota_name,omitempty"`
	RecommendationType string                                      `json:"recommendation_type"`
	RiskLevel          string                                      `json:"risk_level"`
	HeadroomBasisPoints int                                        `json:"headroom_basis_points"`
	QuotaHard          *QuotaResourceValues                        `json:"quota_hard,omitempty"`
	QuotaUsed          *QuotaResourceValues                        `json:"quota_used,omitempty"`
	QuotaRecommended   *QuotaResourceValues                        `json:"quota_recommended,omitempty"`
	Utilization        *QuotaUtilizationPercents                   `json:"utilization,omitempty"`
	CapacityFreed      *QuotaCapacityFreedResponse                 `json:"capacity_freed,omitempty"`
	EstimatedSavings   *money.SavingsObject                        `json:"estimated_savings,omitempty"`
	Notifications      map[string]notifications.NotificationEntry  `json:"notifications,omitempty"`
	LastObservedAt     string                                      `json:"last_observed_at,omitempty"`
	History            []engine.QuotaRecommendationHistoryRow      `json:"history,omitempty"`
}

// ClusterQuotaRecommendationDetailResponse is the full CRQ recommendation for one object.
type ClusterQuotaRecommendationDetailResponse struct {
	ClusterUUID          string                                      `json:"cluster_uuid"`
	ClusterQuotaName     string                                      `json:"cluster_quota_name"`
	RecommendationType   string                                      `json:"recommendation_type"`
	RiskLevel            string                                      `json:"risk_level"`
	QuotaHard            *ClusterQuotaResourceValues                 `json:"quota_hard,omitempty"`
	QuotaUsed            *ClusterQuotaResourceValues                 `json:"quota_used,omitempty"`
	QuotaRecommended     *ClusterQuotaResourceValues                 `json:"quota_recommended,omitempty"`
	Utilization          *ClusterQuotaUtilizationPercents            `json:"utilization,omitempty"`
	CapacityFreed        *ClusterQuotaCapacityFreedResponse          `json:"capacity_freed,omitempty"`
	EstimatedSavings     *ClusterQuotaSavingsMonthly                 `json:"estimated_savings,omitempty"`
	Notifications        map[string]notifications.NotificationEntry  `json:"notifications,omitempty"`
	Namespaces           []string                                      `json:"namespaces,omitempty"`
	History              []engine.ClusterQuotaRecommendationHistoryRow `json:"history,omitempty"`
}

type quotaDetailIdentity struct {
	clusterUUID string
	namespace   string
	quotaName   string
}

type clusterQuotaDetailIdentity struct {
	clusterUUID      string
	clusterQuotaName string
}

func parseQuotaDetailIdentity(c echo.Context) quotaDetailIdentity {
	cluster := strings.TrimSpace(c.QueryParam("cluster_uuid"))
	if cluster == "" {
		cluster = queryparams.FirstFilter(c, "cluster")
	}
	namespace := strings.TrimSpace(c.QueryParam("namespace"))
	if namespace == "" {
		namespace = queryparams.FirstFilter(c, "project")
	}
	quotaName := strings.TrimSpace(c.QueryParam("quota_name"))
	if quotaName == "" {
		quotaName = queryparams.FirstFilter(c, "quota_name")
	}
	if quotaName == "" {
		quotaName = queryparams.FirstFilter(c, "resource_quota_name")
	}
	return quotaDetailIdentity{clusterUUID: cluster, namespace: namespace, quotaName: quotaName}
}

func parseClusterQuotaDetailIdentity(c echo.Context) clusterQuotaDetailIdentity {
	cluster := strings.TrimSpace(c.QueryParam("cluster_uuid"))
	if cluster == "" {
		cluster = queryparams.FirstFilter(c, "cluster")
	}
	name := strings.TrimSpace(c.QueryParam("cluster_quota_name"))
	if name == "" {
		name = queryparams.FirstFilter(c, "cluster_quota_name")
	}
	if name == "" {
		name = queryparams.FirstFilter(c, "cluster_resource_quota")
	}
	if name == "" {
		name = queryparams.FirstFilter(c, "crq")
	}
	return clusterQuotaDetailIdentity{clusterUUID: cluster, clusterQuotaName: name}
}

// GetQuotaRecommendationDetail handles GET /recommendations/openshift/quota/detail.
func GetQuotaRecommendationDetail(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	id := parseQuotaDetailIdentity(c)
	if id.clusterUUID == "" || id.namespace == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": "cluster_uuid and namespace are required",
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

	query := `
		SELECT cluster_uuid, namespace, quota_name, recommendation_type, risk_level, headroom_basis_points,
			cpu_request_hard_millicores, cpu_limit_hard_millicores,
			memory_request_hard_bytes, memory_limit_hard_bytes,
			cpu_request_used_millicores, cpu_limit_used_millicores,
			memory_request_used_bytes, memory_limit_used_bytes,
			storage_request_hard_bytes, storage_request_used_bytes, storage_request_recommended_bytes,
			pods_hard, pods_used, pods_recommended,
			cpu_request_recommended_millicores, cpu_limit_recommended_millicores,
			memory_request_recommended_bytes, memory_limit_recommended_bytes,
			cpu_request_utilization_bp, cpu_limit_utilization_bp,
			memory_request_utilization_bp, memory_limit_utilization_bp,
			utilization_storage_request_bp, utilization_pods_bp,
			cpu_freed_millicores, memory_freed_bytes,
			estimated_savings_cents, currency, notification_codes, last_observed_at
		FROM quota_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace = $3`
	args := []any{orgID, id.clusterUUID, id.namespace}
	if id.quotaName != "" {
		query += ` AND quota_name = $4`
		args = append(args, id.quotaName)
	} else {
		query += ` ORDER BY quota_name LIMIT 1`
	}

	row := pool.QueryRow(ctx, query, args...)
	item, codes, headroomBP, err := scanQuotaDetailRow(row)
	if err == sql.ErrNoRows {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "error",
			"message": "quota recommendation not found",
		})
	}
	if err != nil {
		hlog.Errorf("quota detail query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch quota recommendation detail",
		})
	}

	quotaName := item.QuotaName
	if id.quotaName != "" {
		quotaName = id.quotaName
	}
	history, histErr := engine.ListQuotaRecommendationHistory(ctx, pool, orgID, id.clusterUUID, id.namespace, quotaName, 30)
	if histErr != nil {
		hlog.Errorf("quota detail history failed: %v", histErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch quota recommendation history",
		})
	}

	detail := QuotaRecommendationDetailResponse{
		ClusterUUID:         item.ClusterUUID,
		Namespace:           item.Namespace,
		QuotaName:           item.QuotaName,
		RecommendationType:  item.RecommendationType,
		RiskLevel:           item.RiskLevel,
		HeadroomBasisPoints: headroomBP,
		QuotaHard:           item.QuotaHard,
		QuotaUsed:           item.QuotaUsed,
		QuotaRecommended:    item.QuotaRecommended,
		Utilization:         item.Utilization,
		CapacityFreed:       item.CapacityFreed,
		EstimatedSavings:    item.EstimatedSavings,
		Notifications:       notifications.MapToKruizeFormat(codes),
		LastObservedAt:      item.LastObservedAt,
		History:             history,
	}
	if detail.History == nil {
		detail.History = []engine.QuotaRecommendationHistoryRow{}
	}
	return c.JSON(http.StatusOK, detail)
}

// GetClusterQuotaRecommendationDetail handles GET /recommendations/openshift/cluster-quota/detail.
func GetClusterQuotaRecommendationDetail(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	id := parseClusterQuotaDetailIdentity(c)
	if id.clusterUUID == "" || id.clusterQuotaName == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": "cluster_uuid and cluster_quota_name are required",
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
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND cluster_quota_name = $3`

	row := pool.QueryRow(ctx, query, orgID, id.clusterUUID, id.clusterQuotaName)
	item, codes, err := scanClusterQuotaDetailRow(row)
	if err == sql.ErrNoRows {
		return c.JSON(http.StatusNotFound, echo.Map{
			"status":  "error",
			"message": "cluster-quota recommendation not found",
		})
	}
	if err != nil {
		hlog.Errorf("cluster-quota detail query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch cluster-quota recommendation detail",
		})
	}

	history, histErr := engine.ListClusterQuotaRecommendationHistory(ctx, pool, orgID, id.clusterUUID, id.clusterQuotaName, 30)
	if histErr != nil {
		hlog.Errorf("cluster-quota detail history failed: %v", histErr)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch cluster-quota recommendation history",
		})
	}

	detail := ClusterQuotaRecommendationDetailResponse{
		ClusterUUID:        item.ClusterUUID,
		ClusterQuotaName:   item.ClusterQuotaName,
		RecommendationType: item.RecommendationType,
		RiskLevel:          item.RiskLevel,
		QuotaHard:          item.QuotaHard,
		QuotaUsed:          item.QuotaUsed,
		QuotaRecommended:   item.QuotaRecommended,
		Utilization:        item.Utilization,
		CapacityFreed:      item.CapacityFreed,
		EstimatedSavings:   item.EstimatedSavings,
		Notifications:      notifications.MapToKruizeFormat(codes),
		Namespaces:         item.Namespaces,
		History:            history,
	}
	if detail.History == nil {
		detail.History = []engine.ClusterQuotaRecommendationHistoryRow{}
	}
	return c.JSON(http.StatusOK, detail)
}

type quotaDetailRowScanner interface {
	Scan(dest ...any) error
}

func scanQuotaDetailRow(rows quotaDetailRowScanner) (QuotaRecommendationListItem, []int16, int, error) {
	var item QuotaRecommendationListItem
	var headroomBP int
	var codes []int16
	var cpuReqHard, cpuLimHard, memReqHard, memLimHard sql.NullInt64
	var cpuReqUsed, cpuLimUsed, memReqUsed, memLimUsed sql.NullInt64
	var storageHard, storageUsed, storageRec sql.NullInt64
	var podsHard, podsUsed, podsRec sql.NullInt64
	var cpuReqRec, cpuLimRec, memReqRec, memLimRec sql.NullInt64
	var cpuReqUtil, cpuLimUtil, memReqUtil, memLimUtil sql.NullInt64
	var storageUtil, podsUtil sql.NullInt64
	var cpuFreed, memFreed sql.NullInt64
	var savings sql.NullInt64
	var currency string
	var lastObserved sql.NullTime

	err := rows.Scan(
		&item.ClusterUUID, &item.Namespace, &item.QuotaName, &item.RecommendationType, &item.RiskLevel, &headroomBP,
		&cpuReqHard, &cpuLimHard, &memReqHard, &memLimHard,
		&cpuReqUsed, &cpuLimUsed, &memReqUsed, &memLimUsed,
		&storageHard, &storageUsed, &storageRec,
		&podsHard, &podsUsed, &podsRec,
		&cpuReqRec, &cpuLimRec, &memReqRec, &memLimRec,
		&cpuReqUtil, &cpuLimUtil, &memReqUtil, &memLimUtil,
		&storageUtil, &podsUtil,
		&cpuFreed, &memFreed,
		&savings, &currency, &codes, &lastObserved,
	)
	if err != nil {
		return item, nil, 0, err
	}

	item.QuotaHard = quotaValuesFromNullExtended(cpuReqHard, cpuLimHard, memReqHard, memLimHard, storageHard, podsHard)
	item.QuotaUsed = quotaValuesFromNullExtended(cpuReqUsed, cpuLimUsed, memReqUsed, memLimUsed, storageUsed, podsUsed)
	item.QuotaRecommended = quotaValuesFromNullExtended(cpuReqRec, cpuLimRec, memReqRec, memLimRec, storageRec, podsRec)
	item.Utilization = quotaUtilFromNullBP(cpuReqUtil, cpuLimUtil, memReqUtil, memLimUtil, storageUtil, podsUtil)
	item.CapacityFreed = &QuotaCapacityFreedResponse{
		CPUMillicores: nullInt64Val(cpuFreed),
		MemoryBytes:   nullInt64Val(memFreed),
	}
	if savings.Valid {
		item.EstimatedSavings = money.FormatCentsToSavingsPtr(&savings.Int64, currency)
	}
	if lastObserved.Valid {
		item.LastObservedAt = lastObserved.Time.UTC().Format(time.RFC3339)
	}
	return item, codes, headroomBP, nil
}

func scanClusterQuotaDetailRow(rows clusterQuotaRowScanner) (ClusterQuotaRecommendationListItem, []int16, error) {
	var item ClusterQuotaRecommendationListItem
	var codes []int16
	var cpuReqHard, cpuLimHard, memReqHard, memLimHard sql.NullInt64
	var cpuReqUsed, cpuLimUsed, memReqUsed, memLimUsed sql.NullInt64
	var cpuReqRec, cpuLimRec, memReqRec, memLimRec sql.NullInt64
	var storageHard, storageUsed, storageRec sql.NullInt64
	var podsHard, podsUsed, podsRec sql.NullInt64
	var cpuReqUtil, memReqUtil, storageUtil, podsUtil sql.NullInt64
	var cpuCoresFreed, memFreed, storageFreed, podsFreed sql.NullInt64
	var savings sql.NullInt64
	var namespacesRaw sql.NullString

	err := rows.Scan(
		&item.ClusterUUID, &item.ClusterQuotaName, &item.RecommendationType, &item.RiskLevel,
		&cpuReqHard, &cpuLimHard, &memReqHard, &memLimHard,
		&cpuReqUsed, &cpuLimUsed, &memReqUsed, &memLimUsed,
		&cpuReqRec, &cpuLimRec, &memReqRec, &memLimRec,
		&storageHard, &storageUsed, &storageRec,
		&podsHard, &podsUsed, &podsRec,
		&cpuReqUtil, &memReqUtil, &storageUtil, &podsUtil,
		&cpuCoresFreed, &memFreed, &storageFreed, &podsFreed, &savings, &codes, &namespacesRaw,
	)
	if err != nil {
		return item, nil, err
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
	item.Namespaces = clusterQuotaNamespacesFromDB(namespacesRaw)
	return item, codes, nil
}
