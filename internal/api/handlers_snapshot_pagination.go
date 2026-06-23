package api

import (
	"fmt"
	"strconv"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

const (
	snapshotDefaultOrderBy  = "age_days"
	snapshotDefaultOrderHow = "desc"
)

var snapshotAllowedOrderBy = map[string]string{
	"age_days":                "age_days",
	"restore_size_bytes":      "restore_size_bytes",
	"estimated_monthly_cost":  "estimated_cost_cents",
	"snapshot_name":           "snapshot_name",
	"namespace":               "namespace",
	"recommendation_type":     "recommendation_type",
}

func snapshotSortValue(r SnapshotRecommendationResponse, orderCol string) interface{} {
	switch orderCol {
	case "estimated_cost_cents":
		if r.EstimatedMonthlyCost != nil && r.EstimatedMonthlyCost.Value != "" {
			v, err := strconv.ParseFloat(r.EstimatedMonthlyCost.Value, 64)
			if err == nil {
				return money.USDToCents(v)
			}
		}
		return nil
	case "restore_size_bytes":
		return r.RestoreSizeBytes
	case "snapshot_name":
		return r.SnapshotName
	case "namespace":
		return r.Namespace
	case "recommendation_type":
		return r.RecommendationType
	default:
		return r.AgeDays
	}
}

func snapshotSeekSQL(orderCol, orderHow string, cursor SnapshotCursor, hasSort bool, argIdx int) (string, []interface{}, int, error) {
	tie := "(cluster_uuid, namespace, snapshot_name)"
	if hasSort && len(cursor.SortValue) > 0 {
		sortVal, err := decodeCursorSortValue(cursor.SortValue)
		if err != nil {
			return "", nil, argIdx, fmt.Errorf("invalid after parameter: %w", err)
		}
		clause, args := keysetSeekClause(orderCol, orderHow, tie, sortVal,
			cursor.ClusterUUID, cursor.Namespace, cursor.SnapshotName)
		clause, args, argIdx = bindSeekClause(clause, args, argIdx)
		return clause, args, argIdx, nil
	}
	clause := tie + " > ($" + strconv.Itoa(argIdx) + ", $" + strconv.Itoa(argIdx+1) + ", $" + strconv.Itoa(argIdx+2) + ")"
	args := []interface{}{cursor.ClusterUUID, cursor.Namespace, cursor.SnapshotName}
	return clause, args, argIdx + 3, nil
}

func snapshotOrderNulls(orderCol, orderDir string) string {
	if orderDir == listoptions.OrderDesc {
		return orderCol + " DESC NULLS LAST"
	}
	return orderCol + " ASC"
}

func snapshotGroupNextCursor(groupKey string) string {
	return EncodeSnapshotCursor(SnapshotCursor{GroupKey: groupKey})
}

func snapshotNextCursor(orderCol string, last SnapshotRecommendationResponse, sortValue interface{}) string {
	return EncodeSnapshotCursor(SnapshotCursor{
		ClusterUUID:  last.ClusterUUID,
		Namespace:    last.Namespace,
		SnapshotName: last.SnapshotName,
		SortValue:    model.PaginationSortValueJSON(sortValue),
		OrderBy:      orderCol,
	})
}
