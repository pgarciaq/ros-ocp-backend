package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// keysetSeekClause builds a tuple-comparison WHERE fragment for keyset pagination.
// sortCol must be a trusted SQL identifier/expression from an allowlist.
//
// When sortValue is nil the cursor is in the NULL region (all non-NULL rows
// have already been returned). The clause advances only by tie-breaker within
// the NULL tail. When sortValue is non-nil, the clause also accepts rows whose
// sort value IS NULL (they sort after all non-NULLs under NULLS LAST).
func keysetSeekClause(sortCol, orderHow, tieCols string, sortValue interface{}, tieArgs ...interface{}) (string, []interface{}) {
	if sortValue == nil {
		return fmt.Sprintf(
			"((%s) IS NULL AND %s > (%s))",
			sortCol, tieCols, placeholders(len(tieArgs)),
		), tieArgs
	}
	if orderHow == listoptions.OrderDesc {
		return fmt.Sprintf(
			"((%s) < ? OR (%s) IS NULL OR ((%s) IS NOT DISTINCT FROM ? AND %s > (%s)))",
			sortCol, sortCol, sortCol, tieCols, placeholders(len(tieArgs)),
		), append([]interface{}{sortValue, sortValue}, tieArgs...)
	}
	return fmt.Sprintf(
		"((%s) > ? OR (%s) IS NULL OR ((%s) IS NOT DISTINCT FROM ? AND %s > (%s)))",
		sortCol, sortCol, sortCol, tieCols, placeholders(len(tieArgs)),
	), append([]interface{}{sortValue, sortValue}, tieArgs...)
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := "?"
	for i := 1; i < n; i++ {
		out += ", ?"
	}
	return out
}

// buildKeysetListMeta returns standard pagination metadata with has_next and next_cursor.
func buildKeysetListMeta(req *http.Request, count, limit, offset int, hasNext bool, nextCursor string) map[string]interface{} {
	meta := map[string]interface{}{
		"count": count,
		"limit": limit,
	}
	if offset > 0 || nextCursor == "" {
		meta["offset"] = offset
	}
	if hasNext {
		meta["has_next"] = true
		if nextCursor != "" {
			meta["next_cursor"] = nextCursor
		}
	} else {
		meta["has_next"] = false
	}
	return meta
}

// applyKeysetNextLink updates links.next to use the after cursor when present.
func applyKeysetNextLink(links *Links, req *http.Request, limit int, hasNext bool, nextCursor string) {
	if !hasNext || nextCursor == "" {
		return
	}
	links.Next = keysetNextURL(req, limit, nextCursor)
}

// applyModelKeysetNextLink updates model.PaginationLinks.next for keyset pagination.
func applyModelKeysetNextLink(links *model.PaginationLinks, req *http.Request, limit int, hasNext bool, nextCursor string) {
	if !hasNext || nextCursor == "" {
		return
	}
	links.Next = keysetNextURL(req, limit, nextCursor)
}

func keysetNextURL(req *http.Request, limit int, nextCursor string) string {
	q := req.URL.Query()
	q.Set("limit", strconv.Itoa(limit))
	q.Del("offset")
	q.Set("after", nextCursor)
	return fmt.Sprintf("%s?%s", req.URL.Path, q.Encode())
}

func decodeCursorSortValue(raw []byte) (interface{}, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	return model.DecodePaginationSortValue(raw)
}
