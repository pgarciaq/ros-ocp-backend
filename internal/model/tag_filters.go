package model

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
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

// ApplyTagFiltersToKeys adds tag predicates on org_container_keys.
// API source filters resolved_tags (push sync); DB source joins Koku tag summary tables at query time.
func ApplyTagFiltersToKeys(query *gorm.DB, orgID string, filters []TagFilter) *gorm.DB {
	if config.TagsSource() == "api" {
		return applyAPITagFiltersToKeys(query, filters)
	}
	return applyDBTagFiltersToKeys(query, orgID, filters)
}

func applyAPITagFiltersToKeys(query *gorm.DB, filters []TagFilter) *gorm.DB {
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

func applyDBTagFiltersToKeys(query *gorm.DB, orgID string, filters []TagFilter) *gorm.DB {
	schema, err := tags.TenantSchema(orgID)
	if err != nil {
		log.Warnf("ApplyTagFiltersToKeys: invalid org_id %q: %v", orgID, err)
		return query
	}
	tagValuesTable := pgx.Identifier{schema, "reporting_ocptags_values"}.Sanitize()

	for _, f := range filters {
		if f.Key == "" || len(f.Values) == 0 {
			continue
		}
		var matchClause string
		args := []interface{}{f.Key}
		if len(f.Values) == 1 && f.Values[0] == "*" {
			matchClause = "tv.key = ?"
		} else {
			matchClause = "tv.key = ? AND tv.value IN ?"
			args = append(args, f.Values)
		}
		existsSQL := fmt.Sprintf(`EXISTS (
			SELECT 1
			FROM %s tv,
			     unnest(tv.cluster_ids, tv.namespaces) AS t(cluster_id, namespace)
			WHERE %s
			  AND t.cluster_id = ock.cluster_uuid::text
			  AND t.namespace = ock.namespace
		)`, tagValuesTable, matchClause)
		query = query.Where(existsSQL, args...)
	}
	return query
}

// ApplyTagFiltersToClusterNamespace restricts rows to cluster/namespace pairs that match tag predicates.
// clusterColumn and namespaceColumn must be safe SQL identifiers (e.g. "nr.cluster_uuid", "pvc.namespace").
func ApplyTagFiltersToClusterNamespace(query *gorm.DB, orgID string, filters []TagFilter, clusterColumn, namespaceColumn string) *gorm.DB {
	if len(filters) == 0 {
		return query
	}
	subq := database.GetDB().Table("org_container_keys ock").
		Select("1").
		Where("ock.org_id = ?", orgID).
		Where(clusterColumn + " = ock.cluster_uuid").
		Where(namespaceColumn + " = ock.namespace")
	if config.TagsSource() == "api" {
		subq = applyAPITagFiltersToKeys(subq, filters)
	} else {
		subq = applyDBTagFiltersToKeys(subq, orgID, filters)
	}
	return query.Where("EXISTS (?)", subq)
}

// TagFilterExistsClause builds a parameterized EXISTS subquery for pgx handlers.
// nextArg is the next $N index (1-based). Returns empty clause when filters are disabled or empty.
func TagFilterExistsClause(orgID, clusterExpr, namespaceExpr string, filters []TagFilter, nextArg int) (clause string, args []interface{}, next int) {
	if len(filters) == 0 || !config.TagsFeatureEnabled() {
		return "", nil, nextArg
	}
	if config.TagsSource() == "api" {
		return tagFilterAPIExistsClause(orgID, clusterExpr, namespaceExpr, filters, nextArg)
	}
	return tagFilterDBExistsClause(orgID, clusterExpr, namespaceExpr, filters, nextArg)
}

func tagFilterAPIExistsClause(orgID, clusterExpr, namespaceExpr string, filters []TagFilter, nextArg int) (string, []interface{}, int) {
	args := []interface{}{orgID}
	idx := nextArg + 1
	inner := fmt.Sprintf(`EXISTS (
		SELECT 1 FROM org_container_keys ock
		WHERE ock.org_id = $%d
		  AND %s = ock.cluster_uuid`, nextArg, clusterExpr)
	if namespaceExpr != "" {
		inner += fmt.Sprintf(" AND %s = ock.namespace", namespaceExpr)
	}
	for _, f := range filters {
		if f.Key == "" || len(f.Values) == 0 {
			continue
		}
		if len(f.Values) == 1 && f.Values[0] == "*" {
			inner += fmt.Sprintf(" AND ock.resolved_tags ? $%d", idx)
			args = append(args, f.Key)
			idx++
			continue
		}
		if len(f.Values) == 1 {
			payload, err := json.Marshal(map[string]string{f.Key: f.Values[0]})
			if err != nil {
				continue
			}
			inner += fmt.Sprintf(" AND ock.resolved_tags @> $%d::jsonb", idx)
			args = append(args, string(payload))
			idx++
			continue
		}
		inner += fmt.Sprintf(" AND ock.resolved_tags->>$%d IN (", idx)
		args = append(args, f.Key)
		idx++
		placeholders := make([]string, len(f.Values))
		for i := range f.Values {
			placeholders[i] = fmt.Sprintf("$%d", idx)
			args = append(args, f.Values[i])
			idx++
		}
		inner += strings.Join(placeholders, ", ") + ")"
	}
	inner += ")"
	return inner, args, idx
}

func tagFilterDBExistsClause(orgID, clusterExpr, namespaceExpr string, filters []TagFilter, nextArg int) (string, []interface{}, int) {
	schema, err := tags.TenantSchema(orgID)
	if err != nil {
		log.Warnf("TagFilterExistsClause: invalid org_id %q: %v", orgID, err)
		return "", nil, nextArg
	}
	tagValuesTable := pgx.Identifier{schema, "reporting_ocptags_values"}.Sanitize()
	args := []interface{}{}
	idx := nextArg
	inner := fmt.Sprintf(`EXISTS (
		SELECT 1 FROM org_container_keys ock
		WHERE ock.org_id = $%d
		  AND %s = ock.cluster_uuid`, idx, clusterExpr)
	if namespaceExpr != "" {
		inner += fmt.Sprintf(" AND %s = ock.namespace", namespaceExpr)
	}
	inner += fmt.Sprintf(`
		  AND EXISTS (
			SELECT 1 FROM %s tv,
			     unnest(tv.cluster_ids, tv.namespaces) AS t(cluster_id, namespace)
			WHERE t.cluster_id = ock.cluster_uuid::text
			  AND t.namespace = ock.namespace`, tagValuesTable)
	args = append(args, orgID)
	idx++

	tagPredicates := make([]string, 0, len(filters))
	for _, f := range filters {
		if f.Key == "" || len(f.Values) == 0 {
			continue
		}
		if len(f.Values) == 1 && f.Values[0] == "*" {
			tagPredicates = append(tagPredicates, fmt.Sprintf("tv.key = $%d", idx))
			args = append(args, f.Key)
			idx++
			continue
		}
		tagPredicates = append(tagPredicates, fmt.Sprintf("tv.key = $%d AND tv.value IN (", idx))
		args = append(args, f.Key)
		idx++
		placeholders := make([]string, len(f.Values))
		for i := range f.Values {
			placeholders[i] = fmt.Sprintf("$%d", idx)
			args = append(args, f.Values[i])
			idx++
		}
		tagPredicates[len(tagPredicates)-1] += strings.Join(placeholders, ", ") + ")"
	}
	if len(tagPredicates) == 0 {
		return "", nil, nextArg
	}
	inner += " AND (" + strings.Join(tagPredicates, " AND ") + ")"
	inner += "))"
	return inner, args, idx
}

// TagFilterExistsClauseForCommaSeparatedNamespaces matches rows when any namespace listed in a
// comma-separated namespaces column matches the tag predicates (cluster-quota recommendations).
func TagFilterExistsClauseForCommaSeparatedNamespaces(
	orgID, clusterExpr, namespacesExpr string,
	filters []TagFilter,
	nextArg int,
) (clause string, args []interface{}, next int) {
	if len(filters) == 0 || !config.TagsFeatureEnabled() {
		return "", nil, nextArg
	}
	if config.TagsSource() == "api" {
		return tagFilterAPIExistsClauseCommaNamespaces(orgID, clusterExpr, namespacesExpr, filters, nextArg)
	}
	return tagFilterDBExistsClauseCommaNamespaces(orgID, clusterExpr, namespacesExpr, filters, nextArg)
}

func tagFilterAPIExistsClauseCommaNamespaces(
	orgID, clusterExpr, namespacesExpr string,
	filters []TagFilter,
	nextArg int,
) (string, []interface{}, int) {
	args := []interface{}{orgID}
	idx := nextArg + 1
	inner := fmt.Sprintf(`EXISTS (
		SELECT 1
		FROM unnest(string_to_array(COALESCE(%s, ''), ',')) AS member(ns),
		     org_container_keys ock
		WHERE ock.org_id = $%d
		  AND %s = ock.cluster_uuid
		  AND trim(both ' ' from member.ns) = ock.namespace`, namespacesExpr, nextArg, clusterExpr)
	for _, f := range filters {
		if f.Key == "" || len(f.Values) == 0 {
			continue
		}
		if len(f.Values) == 1 && f.Values[0] == "*" {
			inner += fmt.Sprintf(" AND ock.resolved_tags ? $%d", idx)
			args = append(args, f.Key)
			idx++
			continue
		}
		if len(f.Values) == 1 {
			payload, err := json.Marshal(map[string]string{f.Key: f.Values[0]})
			if err != nil {
				continue
			}
			inner += fmt.Sprintf(" AND ock.resolved_tags @> $%d::jsonb", idx)
			args = append(args, string(payload))
			idx++
			continue
		}
		inner += fmt.Sprintf(" AND ock.resolved_tags->>$%d IN (", idx)
		args = append(args, f.Key)
		idx++
		placeholders := make([]string, len(f.Values))
		for i := range f.Values {
			placeholders[i] = fmt.Sprintf("$%d", idx)
			args = append(args, f.Values[i])
			idx++
		}
		inner += strings.Join(placeholders, ", ") + ")"
	}
	inner += ")"
	return inner, args, idx
}

func tagFilterDBExistsClauseCommaNamespaces(
	orgID, clusterExpr, namespacesExpr string,
	filters []TagFilter,
	nextArg int,
) (string, []interface{}, int) {
	schema, err := tags.TenantSchema(orgID)
	if err != nil {
		log.Warnf("TagFilterExistsClauseForCommaSeparatedNamespaces: invalid org_id %q: %v", orgID, err)
		return "", nil, nextArg
	}
	tagValuesTable := pgx.Identifier{schema, "reporting_ocptags_values"}.Sanitize()
	args := []interface{}{orgID}
	idx := nextArg + 1
	inner := fmt.Sprintf(`EXISTS (
		SELECT 1
		FROM unnest(string_to_array(COALESCE(%s, ''), ',')) AS member(ns),
		     org_container_keys ock
		WHERE ock.org_id = $%d
		  AND %s = ock.cluster_uuid
		  AND trim(both ' ' from member.ns) = ock.namespace
		  AND EXISTS (
			SELECT 1 FROM %s tv,
			     unnest(tv.cluster_ids, tv.namespaces) AS t(cluster_id, namespace)
			WHERE t.cluster_id = ock.cluster_uuid::text
			  AND t.namespace = ock.namespace`, namespacesExpr, nextArg, clusterExpr, tagValuesTable)

	tagPredicates := make([]string, 0, len(filters))
	for _, f := range filters {
		if f.Key == "" || len(f.Values) == 0 {
			continue
		}
		if len(f.Values) == 1 && f.Values[0] == "*" {
			tagPredicates = append(tagPredicates, fmt.Sprintf("tv.key = $%d", idx))
			args = append(args, f.Key)
			idx++
			continue
		}
		tagPredicates = append(tagPredicates, fmt.Sprintf("tv.key = $%d AND tv.value IN (", idx))
		args = append(args, f.Key)
		idx++
		placeholders := make([]string, len(f.Values))
		for i := range f.Values {
			placeholders[i] = fmt.Sprintf("$%d", idx)
			args = append(args, f.Values[i])
			idx++
		}
		tagPredicates[len(tagPredicates)-1] += strings.Join(placeholders, ", ") + ")"
	}
	if len(tagPredicates) == 0 {
		return "", nil, nextArg
	}
	inner += " AND (" + strings.Join(tagPredicates, " AND ") + ")"
	inner += "))"
	return inner, args, idx
}
