package api

import (
	"fmt"
	"strconv"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

func machineSetSortCents(rec model.MachineSetRecommendation) int64 {
	if rec.TotalMonthlySavings == nil || rec.TotalMonthlySavings.Value == "" {
		return 0
	}
	v, err := strconv.ParseFloat(rec.TotalMonthlySavings.Value, 64)
	if err != nil {
		return 0
	}
	return money.USDToCents(v)
}

func machineSetSortValue(rec model.MachineSetRecommendation, totalCents int64) interface{} {
	if totalCents > 0 {
		return totalCents
	}
	if rec.TotalMonthlySavings != nil {
		return rec.TotalMonthlySavings.Value
	}
	return nil
}

func machineSetSeekSQL(cursor MachineSetCursor, hasSort bool, argIdx int) (string, []interface{}, int, error) {
	orderCol := "total_savings_cents"
	orderHow := "desc"
	tie := "(machineset_name, cluster_uuid)"
	if hasSort {
		var sortVal interface{}
		if len(cursor.SortValue) > 0 {
			var err error
			sortVal, err = decodeCursorSortValue(cursor.SortValue)
			if err != nil {
				return "", nil, argIdx, fmt.Errorf("invalid after parameter: %w", err)
			}
		}
		clause, args := keysetSeekClause(orderCol, orderHow, tie, sortVal,
			cursor.MachineSetName, cursor.ClusterUUID)
		clause, args, argIdx = bindSeekClause(clause, args, argIdx)
		return clause, args, argIdx, nil
	}
	clause := tie + " > ($" + strconv.Itoa(argIdx) + ", $" + strconv.Itoa(argIdx+1) + ")"
	args := []interface{}{cursor.MachineSetName, cursor.ClusterUUID}
	return clause, args, argIdx + 2, nil
}

func machineSetNextCursor(last model.MachineSetRecommendation, totalCents int64) string {
	return EncodeMachineSetCursor(MachineSetCursor{
		MachineSetName: last.MachineSetName,
		ClusterUUID:    last.ClusterUUID,
		SortValue:      model.PaginationSortValueJSON(machineSetSortValue(last, totalCents)),
		OrderBy:        "total_savings_cents",
	})
}
