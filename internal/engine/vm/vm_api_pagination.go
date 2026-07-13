package vm

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// VMListCursor carries keyset pagination position for VM list queries.
type VMListCursor struct {
	ClusterUUID string
	VMName      string
	Namespace   string
	Term        string
	Engine      string
	SortValue   json.RawMessage
	HasSort     bool
}

func vmKeysetSeekClause(orderCol, orderHow string, cursor VMListCursor, argIdx int) (string, []any, int, error) {
	tie := "(cluster_uuid, vm_name, namespace, term, engine)"
	if cursor.HasSort {
		var sortVal any
		if len(cursor.SortValue) > 0 {
			var err error
			sortVal, err = decodeVMCursorSortValue(cursor.SortValue)
			if err != nil {
				return "", nil, argIdx, fmt.Errorf("invalid after parameter: %w", err)
			}
		}
		clause, args := vmSeekClause(orderCol, orderHow, tie, sortVal,
			cursor.ClusterUUID, cursor.VMName, cursor.Namespace, cursor.Term, cursor.Engine)
		clause, args, argIdx = bindVMSeekClause(clause, args, argIdx)
		return clause, args, argIdx, nil
	}
	clause := tie + " > ($" + strconv.Itoa(argIdx) + ", $" + strconv.Itoa(argIdx+1) + ", $" +
		strconv.Itoa(argIdx+2) + ", $" + strconv.Itoa(argIdx+3) + ", $" + strconv.Itoa(argIdx+4) + ")"
	args := []any{cursor.ClusterUUID, cursor.VMName, cursor.Namespace, cursor.Term, cursor.Engine}
	return clause, args, argIdx + 5, nil
}

func vmSeekClause(sortCol, orderHow, tieCols string, sortValue any, tieArgs ...any) (string, []any) {
	if sortValue == nil {
		return fmt.Sprintf(
			"((%s) IS NULL AND %s > (%s))",
			sortCol, tieCols, vmPlaceholders(len(tieArgs)),
		), tieArgs
	}
	if orderHow == "DESC" {
		return fmt.Sprintf(
			"((%s) < ? OR (%s) IS NULL OR ((%s) IS NOT DISTINCT FROM ? AND %s > (%s)))",
			sortCol, sortCol, sortCol, tieCols, vmPlaceholders(len(tieArgs)),
		), append([]any{sortValue, sortValue}, tieArgs...)
	}
	return fmt.Sprintf(
		"((%s) > ? OR (%s) IS NULL OR ((%s) IS NOT DISTINCT FROM ? AND %s > (%s)))",
		sortCol, sortCol, sortCol, tieCols, vmPlaceholders(len(tieArgs)),
	), append([]any{sortValue, sortValue}, tieArgs...)
}

func vmPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := "?"
	for i := 1; i < n; i++ {
		out += ", ?"
	}
	return out
}

func bindVMSeekClause(clause string, args []any, startIdx int) (string, []any, int) {
	out := ""
	idx := startIdx
	for i := 0; i < len(clause); i++ {
		if clause[i] == '?' {
			out += "$" + strconv.Itoa(idx)
			idx++
			continue
		}
		out += string(clause[i])
	}
	return out, args, idx
}

func decodeVMCursorSortValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f, nil
	}
	var i int64
	if err := json.Unmarshal(raw, &i); err == nil {
		return i, nil
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b, nil
	}
	return nil, fmt.Errorf("unsupported cursor sort value")
}

func vmOrderNulls(orderCol, orderHow string) string {
	if orderHow == "DESC" {
		return orderCol + " DESC NULLS LAST"
	}
	return orderCol + " ASC NULLS LAST"
}
