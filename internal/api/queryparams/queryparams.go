// Package queryparams parses HTTP query parameters using Koku-aligned bracket
// notation (filter[field], order_by[field]).
package queryparams

import (
	"fmt"
	"strings"

	"github.com/labstack/echo/v4"
)

const (
	// FilterPrefix is the Koku filter bracket prefix, e.g. filter[project].
	FilterPrefix = "filter["
	// FilterExactPrefix is the exact-match filter prefix, e.g. filter[exact:project].
	FilterExactPrefix = "filter[exact:"
	// ExcludePrefix is the exclude filter prefix, e.g. exclude[project].
	ExcludePrefix = "exclude["
	// OrderByPrefix is the Koku order bracket prefix, e.g. order_by[cost].
	OrderByPrefix = "order_by["
	// TagFilterPrefix is the Koku tag filter prefix, e.g. filter[tag:team].
	TagFilterPrefix = "filter[tag:"
)

// SplitCommaValues expands repeated query values and comma-separated lists.
// Koku clients often send ?filter[project]=a,b as a single value.
func SplitCommaValues(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// BracketKey returns the Koku filter key for name, e.g. filter[cluster].
func BracketKey(name string) string {
	return FilterPrefix + name + "]"
}

// IncludeValues returns include-filter values from Koku bracket syntax (?filter[field]=a,b).
func IncludeValues(c echo.Context, name string) []string {
	return SplitCommaValues(c.QueryParams()[BracketKey(name)])
}

// ExcludeValues returns exclude-filter values from exclude[name] bracket syntax.
func ExcludeValues(c echo.Context, name string) []string {
	key := ExcludePrefix + name + "]"
	return SplitCommaValues(c.QueryParams()[key])
}

// ExactValues returns exact-match values from filter[exact:name] syntax.
func ExactValues(c echo.Context, name string) []string {
	key := FilterExactPrefix + name + "]"
	return SplitCommaValues(c.QueryParams()[key])
}

// AllFilterValues merges include, exact, and exclude values for validation (e.g. workload_type enum).
func AllFilterValues(c echo.Context, name string) []string {
	return append(append(IncludeValues(c, name), ExactValues(c, name)...), ExcludeValues(c, name)...)
}

// FirstValue returns the first non-empty value from IncludeValues for the given filter names.
func FirstValue(c echo.Context, names ...string) string {
	for _, name := range names {
		if vals := IncludeValues(c, name); len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}

// FirstFilter is shorthand for FirstValue with a single Koku filter name.
func FirstFilter(c echo.Context, name string) string {
	return FirstValue(c, name)
}

// ParseOrderBy resolves ordering from Koku order_by[field]=asc|desc syntax.
func ParseOrderBy(c echo.Context, allowedFields map[string]string, defaultField, defaultDirection string) (dbColumn, direction string, err error) {
	if dbCol, dir, bracketErr := bracketOrderBy(c, allowedFields); bracketErr != nil {
		return "", "", bracketErr
	} else if dbCol != "" {
		return dbCol, dir, nil
	}

	if defaultField == "" {
		return "", defaultDirection, nil
	}
	dbCol, ok := allowedFields[defaultField]
	if !ok {
		return "", "", fmt.Errorf("invalid default order_by: %s", defaultField)
	}
	return dbCol, defaultDirection, nil
}

// bracketOrderBy returns dbColumn when bracket order_by is present; empty dbColumn when absent.
func bracketOrderBy(c echo.Context, allowedFields map[string]string) (dbColumn, direction string, err error) {
	for key, values := range c.QueryParams() {
		if !strings.HasPrefix(key, OrderByPrefix) || !strings.HasSuffix(key, "]") {
			continue
		}
		field := strings.TrimSpace(key[len(OrderByPrefix) : len(key)-1])
		if field == "" {
			return "", "", fmt.Errorf("invalid order_by bracket key: %q", key)
		}
		dbCol, allowed := allowedFields[field]
		if !allowed {
			return "", "", fmt.Errorf("invalid order_by value: %s", field)
		}
		dir := "desc"
		if len(values) > 0 {
			d := strings.ToLower(strings.TrimSpace(values[0]))
			switch d {
			case "asc", "desc":
				dir = d
			case "":
			default:
				return "", "", fmt.Errorf("invalid order direction for %s: %s", field, values[0])
			}
		}
		return dbCol, dir, nil
	}
	return "", "", nil
}

// ParseOrderByAPIKey is like ParseOrderBy but returns the API field name (not DB column).
func ParseOrderByAPIKey(c echo.Context, allowedFields map[string]string, defaultField, defaultDirection string) (apiField, direction string, err error) {
	if field, dir, bracketErr := bracketOrderByAPIKey(c, allowedFields); bracketErr != nil {
		return "", "", bracketErr
	} else if field != "" {
		return field, dir, nil
	}

	if defaultField == "" {
		return defaultField, defaultDirection, nil
	}
	if _, ok := allowedFields[defaultField]; !ok {
		return "", "", fmt.Errorf("invalid default order_by: %s", defaultField)
	}
	return defaultField, defaultDirection, nil
}

func bracketOrderByAPIKey(c echo.Context, allowedFields map[string]string) (apiField, direction string, err error) {
	for key, values := range c.QueryParams() {
		if !strings.HasPrefix(key, OrderByPrefix) || !strings.HasSuffix(key, "]") {
			continue
		}
		field := strings.TrimSpace(key[len(OrderByPrefix) : len(key)-1])
		if field == "" {
			return "", "", fmt.Errorf("invalid order_by bracket key: %q", key)
		}
		if _, allowed := allowedFields[field]; !allowed {
			return "", "", fmt.Errorf("invalid order_by value: %s", field)
		}
		dir := "desc"
		if len(values) > 0 {
			d := strings.ToLower(strings.TrimSpace(values[0]))
			switch d {
			case "asc", "desc":
				dir = d
			case "":
			default:
				return "", "", fmt.Errorf("invalid order direction for %s: %s", field, values[0])
			}
		}
		return field, dir, nil
	}
	return "", "", nil
}
