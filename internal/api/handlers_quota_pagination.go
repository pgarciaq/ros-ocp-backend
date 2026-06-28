package api

import (
	"fmt"
	"strconv"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func quotaSortValue(item QuotaRecommendationListItem, orderCol string) interface{} {
	switch orderCol {
	case "quota_name":
		return item.QuotaName
	case "estimated_savings_cents":
		if item.rawSavingsCents != nil {
			return *item.rawSavingsCents
		}
		return nil
	case `CASE risk_level WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END`:
		return item.RiskLevel
	case `GREATEST(COALESCE(cpu_request_utilization_bp,0), COALESCE(cpu_limit_utilization_bp,0), COALESCE(memory_request_utilization_bp,0), COALESCE(memory_limit_utilization_bp,0), COALESCE(utilization_storage_request_bp,0), COALESCE(utilization_pods_bp,0))`:
		return item.rawMaxUtilBP
	default:
		return item.Namespace
	}
}

func quotaSeekSQL(orderCol, orderHow string, cursor QuotaCursor, hasSort bool, argIdx int) (string, []interface{}, int, error) {
	tie := "(cluster_uuid, namespace, quota_name)"
	if hasSort && len(cursor.SortValue) > 0 {
		sortVal, err := decodeCursorSortValue(cursor.SortValue)
		if err != nil {
			return "", nil, argIdx, fmt.Errorf("invalid after parameter: %w", err)
		}
		clause, args := keysetSeekClause(orderCol, orderHow, tie, sortVal,
			cursor.ClusterUUID, cursor.Namespace, cursor.QuotaName)
		clause, args, argIdx = bindSeekClause(clause, args, argIdx)
		return clause, args, argIdx, nil
	}
	clause := tie + " > ($" + strconv.Itoa(argIdx) + ", $" + strconv.Itoa(argIdx+1) + ", $" + strconv.Itoa(argIdx+2) + ")"
	args := []interface{}{cursor.ClusterUUID, cursor.Namespace, cursor.QuotaName}
	return clause, args, argIdx + 3, nil
}

func quotaGroupSeekSQL(groupCol string, cursor QuotaCursor, argIdx int) (string, []interface{}, int) {
	if cursor.GroupKey != "" {
		clause := groupCol + " > $" + strconv.Itoa(argIdx)
		return clause, []interface{}{cursor.GroupKey}, argIdx + 1
	}
	return "", nil, argIdx
}

func quotaOrderNulls(orderCol, orderDir string) string {
	if orderDir == listoptions.OrderDesc {
		return orderCol + " DESC NULLS LAST"
	}
	return orderCol + " ASC NULLS LAST"
}

func quotaNextCursor(orderCol string, last QuotaRecommendationListItem, sortValue interface{}) string {
	return EncodeQuotaCursor(QuotaCursor{
		ClusterUUID: last.ClusterUUID,
		Namespace:   last.Namespace,
		QuotaName:   last.QuotaName,
		SortValue:   model.PaginationSortValueJSON(sortValue),
		OrderBy:     orderCol,
	})
}

func quotaGroupNextCursor(groupKey string) string {
	return EncodeQuotaCursor(QuotaCursor{GroupKey: groupKey})
}
