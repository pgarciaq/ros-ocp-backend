package model

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"gorm.io/gorm"
)

// TagFiltersQueryKey stores parsed tag filters in the native list queryParams map.
const TagFiltersQueryKey = "__tag_filters__"

// TagFilter represents one tag key predicate. Multiple values use OR; multiple filters use AND.
type TagFilter struct {
	Key    string
	Values []string
}

// ParseTagFilters parses repeated legacy ?tag= query values (key:value, key:*).
func ParseTagFilters(raw []string) ([]TagFilter, error) {
	filters := make([]TagFilter, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key, value, ok := parseLegacyTagFilter(item)
		if !ok {
			return nil, fmt.Errorf("invalid tag filter %q: expected key:value", item)
		}
		filters = append(filters, TagFilter{Key: key, Values: []string{value}})
	}
	return filters, nil
}

// ParseKokuTagFilterParams parses ?filter[tag:key]=value1,value2 query params.
func ParseKokuTagFilterParams(queryParams map[string][]string) ([]TagFilter, error) {
	const prefix = "filter[tag:"
	filters := make([]TagFilter, 0)
	for key, values := range queryParams {
		if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, "]") {
			continue
		}
		tagKey := strings.TrimSpace(key[len(prefix) : len(key)-1])
		if tagKey == "" {
			return nil, fmt.Errorf("invalid tag filter key %q", key)
		}
		var tagValues []string
		for _, rawValue := range values {
			for _, part := range strings.Split(rawValue, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					tagValues = append(tagValues, part)
				}
			}
		}
		if len(tagValues) == 0 {
			return nil, fmt.Errorf("tag filter %q requires at least one value", key)
		}
		filters = append(filters, TagFilter{Key: tagKey, Values: tagValues})
	}
	return filters, nil
}

// MergeTagFilters combines filters with the same key by unioning values.
func MergeTagFilters(filters []TagFilter) []TagFilter {
	if len(filters) == 0 {
		return nil
	}
	merged := make(map[string][]string, len(filters))
	order := make([]string, 0, len(filters))
	for _, filter := range filters {
		if filter.Key == "" || len(filter.Values) == 0 {
			continue
		}
		if _, ok := merged[filter.Key]; !ok {
			order = append(order, filter.Key)
		}
		merged[filter.Key] = appendUnique(merged[filter.Key], filter.Values...)
	}
	out := make([]TagFilter, 0, len(order))
	for _, key := range order {
		out = append(out, TagFilter{Key: key, Values: merged[key]})
	}
	return out
}

func appendUnique(existing []string, values ...string) []string {
	seen := make(map[string]struct{}, len(existing))
	for _, value := range existing {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		existing = append(existing, value)
	}
	return existing
}

func parseLegacyTagFilter(raw string) (key, value string, ok bool) {
	idx := strings.Index(raw, ":")
	if idx <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(raw[:idx])
	value = strings.TrimSpace(raw[idx+1:])
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

// TagFiltersFromParams returns tag filters when the feature is enabled; otherwise nil.
func TagFiltersFromParams(queryParams map[string]interface{}) []TagFilter {
	if !config.TagsFeatureEnabled() {
		return nil
	}
	raw, ok := queryParams[TagFiltersQueryKey]
	if !ok {
		return nil
	}
	filters, ok := raw.([]TagFilter)
	if !ok {
		return nil
	}
	return filters
}

// ApplyTagFiltersToKeys adds JSONB predicates on org_container_keys.resolved_tags.
func ApplyTagFiltersToKeys(query *gorm.DB, filters []TagFilter) *gorm.DB {
	for _, f := range filters {
		if f.Key == "" || len(f.Values) == 0 {
			continue
		}
		if len(f.Values) == 1 && f.Values[0] == "*" {
			query = query.Where("ock.resolved_tags ? ?", f.Key)
			continue
		}
		if len(f.Values) == 1 {
			payload, err := json.Marshal(map[string]string{f.Key: f.Values[0]})
			if err != nil {
				log.Warnf("ApplyTagFiltersToKeys: skipping filter %q: %v", f.Key, err)
				continue
			}
			query = query.Where("ock.resolved_tags @> ?", string(payload))
			continue
		}
		query = query.Where("ock.resolved_tags->>? IN ?", f.Key, f.Values)
	}
	return query
}
