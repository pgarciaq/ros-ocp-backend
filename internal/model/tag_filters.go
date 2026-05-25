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

// TagFilter represents one ?tag=key:value predicate (value "*" means key exists).
type TagFilter struct {
	Key   string
	Value string
}

// ParseTagFilters parses repeated ?tag= query values. Multiple filters use AND logic.
func ParseTagFilters(raw []string) ([]TagFilter, error) {
	filters := make([]TagFilter, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key, value, ok := parseTagFilter(item)
		if !ok {
			return nil, fmt.Errorf("invalid tag filter %q: expected key:value", item)
		}
		filters = append(filters, TagFilter{Key: key, Value: value})
	}
	return filters, nil
}

func parseTagFilter(raw string) (key, value string, ok bool) {
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
		if f.Key == "" {
			continue
		}
		if f.Value == "*" {
			query = query.Where("ock.resolved_tags ? ?", f.Key)
			continue
		}
		payload, err := json.Marshal(map[string]string{f.Key: f.Value})
		if err != nil {
			log.Warnf("ApplyTagFiltersToKeys: skipping filter %q: %v", f.Key, err)
			continue
		}
		query = query.Where("ock.resolved_tags @> ?", string(payload))
	}
	return query
}
