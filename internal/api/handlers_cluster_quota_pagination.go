package api

import (
	"fmt"
	"strconv"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func clusterQuotaSortValue(item ClusterQuotaRecommendationListItem, orderCol string) interface{} {
	switch orderCol {
	case "estimated_savings_cents":
		if item.rawSavingsCents != nil {
			return *item.rawSavingsCents
		}
		return nil
	case `CASE risk_level WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END`:
		return item.RiskLevel
	case `GREATEST(COALESCE(utilization_cpu_request_percent,0), COALESCE(utilization_memory_request_percent,0), COALESCE(utilization_storage_request_percent,0), COALESCE(utilization_pods_percent,0))`:
		return item.rawMaxUtilPercent
	default:
		return item.ClusterQuotaName
	}
}

func clusterQuotaSeekSQL(orderCol, orderHow string, cursor ClusterQuotaCursor, hasSort bool, argIdx int) (string, []interface{}, int, error) {
	tie := "(cluster_uuid, cluster_quota_name)"
	if hasSort && len(cursor.SortValue) > 0 {
		sortVal, err := decodeCursorSortValue(cursor.SortValue)
		if err != nil {
			return "", nil, argIdx, fmt.Errorf("invalid after parameter: %w", err)
		}
		clause, args := keysetSeekClause(orderCol, orderHow, tie, sortVal,
			cursor.ClusterUUID, cursor.ClusterQuotaName)
		clause, args, argIdx = bindSeekClause(clause, args, argIdx)
		return clause, args, argIdx, nil
	}
	clause := tie + " > ($" + strconv.Itoa(argIdx) + ", $" + strconv.Itoa(argIdx+1) + ")"
	args := []interface{}{cursor.ClusterUUID, cursor.ClusterQuotaName}
	return clause, args, argIdx + 2, nil
}

func clusterQuotaGroupSeekSQL(cursor ClusterQuotaCursor, argIdx int) (string, []interface{}, int) {
	if cursor.GroupKey != "" {
		clause := "cluster_uuid::text > $" + strconv.Itoa(argIdx)
		return clause, []interface{}{cursor.GroupKey}, argIdx + 1
	}
	return "", nil, argIdx
}

func clusterQuotaOrderNulls(orderCol, orderDir string) string {
	if orderDir == listoptions.OrderDesc {
		return orderCol + " DESC NULLS LAST"
	}
	return orderCol + " ASC NULLS LAST"
}

func clusterQuotaNextCursor(orderCol string, last ClusterQuotaRecommendationListItem, sortValue interface{}) string {
	return EncodeClusterQuotaCursor(ClusterQuotaCursor{
		ClusterUUID:      last.ClusterUUID,
		ClusterQuotaName: last.ClusterQuotaName,
		SortValue:        model.PaginationSortValueJSON(sortValue),
		OrderBy:          orderCol,
	})
}

func clusterQuotaGroupNextCursor(groupKey string) string {
	return EncodeClusterQuotaCursor(ClusterQuotaCursor{GroupKey: groupKey})
}
