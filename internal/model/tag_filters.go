package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

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

// labelSummaryWindow returns the ADR-0353 month-snapped report-period window:
// periods whose report_period_start falls in [last_month_start,
// this_month_end] — the same window the api-mode push reads, so db mode and
// api mode see the same clusters. Period starts are month-aligned, so the
// half-open [lastMonthStart, nextMonthStart) is equivalent.
func labelSummaryWindow() (time.Time, time.Time) {
	now := time.Now().UTC()
	thisMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return thisMonthStart.AddDate(0, -1, 0), thisMonthStart.AddDate(0, 1, 0)
}

// tagFragmentBuilder builds SQL fragments with their bind arguments in one
// of two placeholder styles: gorm's "?" or pgx's "$N" (idx starts at the
// caller's next argument position).
type tagFragmentBuilder struct {
	args    []interface{}
	idx     int
	useGorm bool
}

func newGormTagFragmentBuilder() *tagFragmentBuilder {
	return &tagFragmentBuilder{useGorm: true}
}

// newPgxTagFragmentBuilder builds fragments for pgx's "$N" placeholders.
// nextPlaceholder is the position number the builder's FIRST placeholder
// must take (ph() pre-increments, so idx starts one below it).
func newPgxTagFragmentBuilder(nextPlaceholder int) *tagFragmentBuilder {
	return &tagFragmentBuilder{idx: nextPlaceholder - 1}
}

func (b *tagFragmentBuilder) ph() string {
	if b.useGorm {
		return "?"
	}
	b.idx++
	return fmt.Sprintf("$%d", b.idx)
}

func (b *tagFragmentBuilder) arg(v interface{}) {
	b.args = append(b.args, v)
}

// labelSummaryTagMatch builds the #815 db-mode tag-match fragment for one
// filter, correlated on the caller's cluster and namespace SQL expressions
// (which must render text: e.g. "ock.cluster_uuid::text", "ock.namespace").
//
// Source: Koku's two label-summary tables (the Tags API's and ADR-0353's
// push source), joined through the report period for the cluster — NOT
// reporting_ocptags_values, whose cluster/namespace arrays are rebuilt per
// summarization run and hold only the last-summarized cluster for any
// shared key/value (#815). The month-snapped window mirrors the push's, so
// a cluster keeps matching while one of its periods is in the window.
//
// The enabledtagkeys EXISTS (folded per the owner's #815 comment) makes a
// disabled key stop matching immediately — today db mode keeps matching
// until the next summarization purges the label rows.
//
// A summary row's values are an array per (key, period, namespace, node);
// the namespace matches when any node's row in any window period carries a
// requested value — the same union-then-match the push performs.
func (b *tagFragmentBuilder) labelSummaryTagMatch(schema, clusterRef, nsRef string, f TagFilter) string {
	podTable := pgx.Identifier{schema, "reporting_ocpusagepodlabel_summary"}.Sanitize()
	volTable := pgx.Identifier{schema, "reporting_ocpstoragevolumelabel_summary"}.Sanitize()
	periodTable := pgx.Identifier{schema, "reporting_ocpusagereportperiod"}.Sanitize()
	etkTable := pgx.Identifier{schema, "reporting_enabledtagkeys"}.Sanitize()

	winStart, winEnd := labelSummaryWindow()

	// summaryPredicate emits its own placeholders and arguments for every
	// occurrence — gorm's "?" binds one argument per occurrence, so a
	// placeholder cannot be shared between the pod and storage EXISTS
	// (pgx's "$N" could repeat, but one style must serve both).
	summaryPredicate := func(table string) string {
		keyPH := b.ph()
		b.arg(f.Key)

		valueClause := ""
		if len(f.Values) != 1 || f.Values[0] != "*" {
			parts := make([]string, len(f.Values))
			for i, v := range f.Values {
				ph := b.ph()
				b.arg(v)
				parts[i] = fmt.Sprintf("%s = ANY(ls.values)", ph)
			}
			valueClause = "AND (" + strings.Join(parts, " OR ") + ")"
		}

		// Window bounds bind as DATE strings with explicit ::date casts —
		// comparing a bound timestamptz against the DATE column would shift
		// the boundary under a non-UTC session TimeZone (#815 review).
		winStartPH := b.ph()
		b.arg(winStart.Format("2006-01-02"))
		winEndPH := b.ph()
		b.arg(winEnd.Format("2006-01-02"))

		return fmt.Sprintf(`EXISTS (
				SELECT 1
				FROM %s ls
				JOIN %s rp ON rp.id = ls.report_period_id
				WHERE ls.key = %s
				  AND ls.namespace = %s
				  %s
				  AND rp.cluster_id = %s
				  AND rp.report_period_start >= %s::date
				  AND rp.report_period_start < %s::date
			)`, table, periodTable, keyPH, nsRef, valueClause, clusterRef, winStartPH, winEndPH)
	}

	// Build the summary predicates FIRST: the builder appends bind arguments
	// in placeholder order, and Go evaluates fmt.Sprintf's arguments (the
	// summaryPredicate calls) only when the return statement runs —
	// generating etk's placeholder before them would append its argument
	// first and scramble every bind after it.
	podPredicate := summaryPredicate(podTable)
	volPredicate := summaryPredicate(volTable)

	etkPH := b.ph()
	b.arg(f.Key)

	// provider_type scope (#815 review P2): Cost Management enables tag keys
	// per provider type — an OCP-disabled key with an AWS-enabled row of the
	// same name must not keep matching. Koku's own label-summary SQL and
	// ROS's DBTagProvider both scope the same way.
	etkTypePH := b.ph()
	b.arg("OCP")

	return fmt.Sprintf(`((%s OR %s) AND EXISTS (
			SELECT 1 FROM %s etk WHERE etk.key = %s AND etk.enabled AND etk.provider_type = %s
		))`,
		podPredicate, volPredicate, etkTable, etkPH, etkTypePH)
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

// tagValueContainmentGorm builds the #814 D2 any-of predicate for one
// filter in both storage shapes: for each requested value v of key k,
// (tags @> {"k":["v"]}) OR (tags @> {"k":"v"}) — the array arm is jsonb
// containment ("v" ∈ the stored array, the post-#814 storage); the scalar
// arm matches legacy rows (the pre-#814 storage, until each org's next
// push — the D4 transition tolerance). column is the qualified jsonb
// column ("ock.resolved_tags" / "onk.resolved_tags"). Returns "" when no
// value survived marshaling.
func tagValueContainmentGorm(column string, f TagFilter) (string, []interface{}) {
	var clauses []string
	var args []interface{}
	for _, v := range f.Values {
		arrayPayload, err := json.Marshal(map[string][]string{f.Key: {v}})
		if err != nil {
			log.Warnf("tagValueContainmentGorm: skipping value %q: %v", v, err)
			continue
		}
		scalarPayload, err := json.Marshal(map[string]string{f.Key: v})
		if err != nil {
			log.Warnf("tagValueContainmentGorm: skipping value %q: %v", v, err)
			continue
		}
		clauses = append(clauses, "("+column+" @> ? OR "+column+" @> ?)")
		args = append(args, string(arrayPayload), string(scalarPayload))
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args
}

// applyAPITagFiltersToKeys applies api-mode (resolved_tags) filters.
// #814: values are stored as sorted arrays; see tagValueContainmentGorm
// for the both-shapes match.
func applyAPITagFiltersToKeys(query *gorm.DB, filters []TagFilter) *gorm.DB {
	for _, f := range filters {
		if f.Key == "" || len(f.Values) == 0 {
			continue
		}
		if len(f.Values) == 1 && f.Values[0] == "*" {
			query = query.Where("ock.resolved_tags ? ?", f.Key)
			continue
		}
		pred, args := tagValueContainmentGorm("ock.resolved_tags", f)
		if pred == "" {
			continue
		}
		query = query.Where(pred, args...)
	}
	return query
}

func applyDBTagFiltersToKeys(query *gorm.DB, orgID string, filters []TagFilter) *gorm.DB {
	schema, err := tags.TenantSchema(orgID)
	if err != nil {
		log.Warnf("ApplyTagFiltersToKeys: invalid org_id %q: %v", orgID, err)
		return query
	}

	for _, f := range filters {
		if f.Key == "" || len(f.Values) == 0 {
			continue
		}
		b := newGormTagFragmentBuilder()
		fragment := b.labelSummaryTagMatch(schema, "ock.cluster_uuid::text", "ock.namespace", f)
		query = query.Where(fragment, b.args...)
	}
	return query
}

// ApplyTagFiltersToClusterNamespace restricts rows to cluster/namespace pairs that match tag predicates.
// clusterColumn and namespaceColumn must be safe SQL identifiers (e.g. "nr.cluster_uuid", "pvc.namespace").
func ApplyTagFiltersToClusterNamespace(query *gorm.DB, orgID string, filters []TagFilter, clusterColumn, namespaceColumn string) *gorm.DB {
	if len(filters) == 0 {
		return query
	}
	if config.TagsSource() == "api" {
		subq := database.GetDB().Table("org_container_keys ock").
			Select("1").
			Where("ock.org_id = ?", orgID).
			Where(clusterColumn + " = ock.cluster_uuid").
			Where(namespaceColumn + " = ock.namespace")
		subq = applyAPITagFiltersToKeys(subq, filters)
		return query.Where("EXISTS (?)", subq)
	}
	return applyDBTagFiltersToClusterNamespace(query, orgID, filters, clusterColumn, namespaceColumn)
}

func applyDBTagFiltersToClusterNamespace(
	query *gorm.DB,
	orgID string,
	filters []TagFilter,
	clusterColumn, namespaceColumn string,
) *gorm.DB {
	schema, err := tags.TenantSchema(orgID)
	if err != nil {
		log.Warnf("ApplyTagFiltersToClusterNamespace: invalid org_id %q: %v", orgID, err)
		return query
	}

	for _, f := range filters {
		if f.Key == "" || len(f.Values) == 0 {
			continue
		}
		b := newGormTagFragmentBuilder()
		fragment := b.labelSummaryTagMatch(schema, clusterColumn+"::text", namespaceColumn, f)
		query = query.Where(fragment, b.args...)
	}
	return query
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

// tagValueContainmentPgx is the numbered-placeholder twin of
// tagValueContainmentGorm for raw pgx sites: same #814 D2 both-shapes
// any-of predicate, with $n::jsonb placeholders starting at startIdx.
// Returns the number of args emitted (0 when no value survived
// marshaling).
func tagValueContainmentPgx(column string, f TagFilter, startIdx int) (string, []interface{}, int) {
	var clauses []string
	var args []interface{}
	idx := startIdx
	for _, v := range f.Values {
		arrayPayload, err := json.Marshal(map[string][]string{f.Key: {v}})
		if err != nil {
			log.Warnf("tagValueContainmentPgx: skipping value %q: %v", v, err)
			continue
		}
		scalarPayload, err := json.Marshal(map[string]string{f.Key: v})
		if err != nil {
			log.Warnf("tagValueContainmentPgx: skipping value %q: %v", v, err)
			continue
		}
		clauses = append(clauses, fmt.Sprintf("(%s @> $%d::jsonb OR %s @> $%d::jsonb)", column, idx, column, idx+1))
		args = append(args, string(arrayPayload), string(scalarPayload))
		idx += 2
	}
	if len(clauses) == 0 {
		return "", nil, 0
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args, len(args)
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
		pred, predArgs, n := tagValueContainmentPgx("ock.resolved_tags", f, idx)
		if n == 0 {
			continue
		}
		inner += " AND " + pred
		args = append(args, predArgs...)
		idx += n
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
	args := []interface{}{}
	idx := nextArg
	inner := fmt.Sprintf(`EXISTS (
		SELECT 1 FROM org_container_keys ock
		WHERE ock.org_id = $%d
		  AND %s = ock.cluster_uuid`, idx, clusterExpr)
	if namespaceExpr != "" {
		inner += fmt.Sprintf(" AND %s = ock.namespace", namespaceExpr)
	}
	inner += `
		  AND `
	args = append(args, orgID)
	idx++

	b := newPgxTagFragmentBuilder(idx)
	fragments := make([]string, 0, len(filters))
	for _, f := range filters {
		if f.Key == "" || len(f.Values) == 0 {
			continue
		}
		fragments = append(fragments, b.labelSummaryTagMatch(schema, "ock.cluster_uuid::text", "ock.namespace", f))
	}
	if len(fragments) == 0 {
		return "", nil, nextArg
	}
	args = append(args, b.args...)
	inner += strings.Join(fragments, "\n		  AND ") + ")"
	return inner, args, b.idx + 1
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
		pred, predArgs, n := tagValueContainmentPgx("ock.resolved_tags", f, idx)
		if n == 0 {
			continue
		}
		inner += " AND " + pred
		args = append(args, predArgs...)
		idx += n
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
	args := []interface{}{orgID}
	idx := nextArg + 1
	inner := fmt.Sprintf(`EXISTS (
		SELECT 1
		FROM unnest(string_to_array(COALESCE(%s, ''), ',')) AS member(ns),
		     org_container_keys ock
		WHERE ock.org_id = $%d
		  AND %s = ock.cluster_uuid
		  AND trim(both ' ' from member.ns) = ock.namespace
		  AND `, namespacesExpr, nextArg, clusterExpr)

	b := newPgxTagFragmentBuilder(idx)
	fragments := make([]string, 0, len(filters))
	for _, f := range filters {
		if f.Key == "" || len(f.Values) == 0 {
			continue
		}
		fragments = append(fragments, b.labelSummaryTagMatch(schema, "ock.cluster_uuid::text", "ock.namespace", f))
	}
	if len(fragments) == 0 {
		return "", nil, nextArg
	}
	args = append(args, b.args...)
	inner += strings.Join(fragments, "\n		  AND ") + ")"
	return inner, args, b.idx + 1
}
