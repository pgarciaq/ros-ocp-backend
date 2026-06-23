package api

import (
	"fmt"
	"strconv"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

func pvcSortValue(r PVCRecommendationResponse, orderCol string) interface{} {
	switch orderCol {
	case "estimated_monthly_savings_usd": // deprecated alias
		if r.EstimatedMonthlySavings != nil {
			return r.EstimatedMonthlySavings.Value
		}
		return nil
	case "persistentvolumeclaim":
		return r.PersistentVolumeClaim
	case "capacity_bytes":
		return r.CapacityBytes
	default:
		return r.UsageRatio
	}
}

func pvcSeekSQL(orderCol, orderHow string, cursor PVCCursor, hasSort bool, argIdx int) (string, []interface{}, int, error) {
	tie := "(cluster_uuid, namespace, persistentvolumeclaim)"
	if hasSort && len(cursor.SortValue) > 0 {
		sortVal, err := decodeCursorSortValue(cursor.SortValue)
		if err != nil {
			return "", nil, argIdx, fmt.Errorf("invalid after parameter: %w", err)
		}
		clause, args := keysetSeekClause(orderCol, orderHow, tie, sortVal,
			cursor.ClusterUUID, cursor.Namespace, cursor.PersistentVolumeClaim)
		clause, args, argIdx = bindSeekClause(clause, args, argIdx)
		return clause, args, argIdx, nil
	}
	clause := tie + " > ($" + strconv.Itoa(argIdx) + ", $" + strconv.Itoa(argIdx+1) + ", $" + strconv.Itoa(argIdx+2) + ")"
	args := []interface{}{cursor.ClusterUUID, cursor.Namespace, cursor.PersistentVolumeClaim}
	return clause, args, argIdx + 3, nil
}

// bindSeekClause rewrites ? placeholders in a seek clause to PostgreSQL $N bind positions.
func bindSeekClause(clause string, args []interface{}, startIdx int) (string, []interface{}, int) {
	out := ""
	idx := startIdx
	argPos := 0
	for i := 0; i < len(clause); i++ {
		if clause[i] == '?' {
			out += "$" + strconv.Itoa(idx)
			idx++
			argPos++
			continue
		}
		out += string(clause[i])
	}
	return out, args, idx
}

func pvcGroupNextCursor(groupKey string) string {
	return EncodePVCCursor(PVCCursor{GroupKey: groupKey})
}

func pvcConfidenceForRow(dataDays int, term string, terms []engine.TermConfig) float32 {
	minDataDays := 14
	for _, tc := range terms {
		if tc.Name == term {
			minDataDays = tc.MinDataDays
			break
		}
	}
	return engine.PVCConfidenceLevel(dataDays, minDataDays)
}

func pvcOrderNulls(orderCol, orderDir string) string {
	if orderDir == listoptions.OrderDesc {
		return orderCol + " DESC NULLS LAST"
	}
	return orderCol + " ASC"
}
