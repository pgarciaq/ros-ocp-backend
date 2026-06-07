package model_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func TestParseKokuTagFilterParams_EmptyValueRejected(t *testing.T) {
	t.Parallel()

	_, err := model.ParseKokuTagFilterParams(map[string][]string{
		"filter[tag:environment]": {""},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires at least one value")
}

func TestParseKokuTagFilterParams_EmptyKeyRejected(t *testing.T) {
	t.Parallel()

	_, err := model.ParseKokuTagFilterParams(map[string][]string{
		"filter[tag:]": {"production"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tag filter key")
}

func TestMergeTagFilters_UnionsSameKeyValues(t *testing.T) {
	t.Parallel()

	merged := model.MergeTagFilters([]model.TagFilter{
		{Key: "environment", Values: []string{"production"}},
		{Key: "environment", Values: []string{"staging", "production"}},
		{Key: "team", Values: []string{"platform"}},
	})
	require.Len(t, merged, 2)
	byKey := make(map[string][]string, len(merged))
	for _, f := range merged {
		byKey[f.Key] = f.Values
	}
	assert.Equal(t, []string{"production", "staging"}, byKey["environment"])
	assert.Equal(t, []string{"platform"}, byKey["team"])
}

func TestTagFiltersFromParams_DisabledReturnsNil(t *testing.T) {
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "false")

	params := map[string]interface{}{
		model.TagFiltersQueryKey: []model.TagFilter{{Key: "environment", Values: []string{"production"}}},
	}
	assert.Nil(t, model.TagFiltersFromParams(params))
}

func TestTagFilterExistsClause_DisabledReturnsEmpty(t *testing.T) {
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "false")

	clause, args, next := model.TagFilterExistsClause(
		"1234567", "nr.cluster_uuid", "nr.namespace", []model.TagFilter{{Key: "environment", Values: []string{"production"}}}, 3)
	assert.Empty(t, clause)
	assert.Nil(t, args)
	assert.Equal(t, 3, next)
}

func TestTagFilterExistsClause_NodeClusterScopedOmitsNamespaceColumn(t *testing.T) {
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "api")

	filters := []model.TagFilter{{Key: "environment", Values: []string{"production"}}}
	clause, args, next := model.TagFilterExistsClause("1234567", "nr.cluster_uuid", "", filters, 5)
	require.NotEmpty(t, clause)
	assert.Contains(t, clause, "nr.cluster_uuid = ock.cluster_uuid")
	assert.NotContains(t, clause, "nr.namespace = ock.namespace")
	assert.Contains(t, clause, "ock.resolved_tags @>")
	require.GreaterOrEqual(t, len(args), 2)
	assert.Equal(t, "1234567", args[0])
	assert.Greater(t, next, 5)
}

func TestTagFilterExistsClause_NamespaceScopedIncludesNamespaceColumn(t *testing.T) {
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "api")

	filters := []model.TagFilter{{Key: "environment", Values: []string{"production", "staging"}}}
	clause, args, _ := model.TagFilterExistsClause("1234567", "pvc.cluster_uuid", "pvc.namespace", filters, 2)
	require.NotEmpty(t, clause)
	assert.Contains(t, clause, "pvc.namespace = ock.namespace")
	assert.Contains(t, clause, "ock.resolved_tags->>$")
	assert.Contains(t, clause, "IN (")
	require.GreaterOrEqual(t, len(args), 4)
}

func TestTagFilterExistsClause_DBSourceBalancedParentheses(t *testing.T) {
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "db")

	filters := []model.TagFilter{
		{Key: "environment", Values: []string{"production"}},
		{Key: "team", Values: []string{"platform"}},
	}
	clause, args, next := model.TagFilterExistsClause("1234567", "nr.cluster_uuid", "nr.namespace", filters, 1)
	require.NotEmpty(t, clause)
	assert.Equal(t, strings.Count(clause, "("), strings.Count(clause, ")"), "unbalanced parentheses in DB tag EXISTS clause")
	assert.Contains(t, clause, "reporting_ocptags_values")
	assert.Contains(t, clause, "tv.key = $")
	assert.Contains(t, clause, " AND (")
	require.GreaterOrEqual(t, len(args), 4)
	assert.Greater(t, next, 1)
}

func TestTagFilterExistsClauseForCommaSeparatedNamespaces_DBSource(t *testing.T) {
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "db")

	filters := []model.TagFilter{{Key: "environment", Values: []string{"production"}}}
	clause, args, next := model.TagFilterExistsClauseForCommaSeparatedNamespaces(
		"1234567", "cluster_uuid", "namespaces", filters, 4)
	require.NotEmpty(t, clause)
	assert.Contains(t, clause, "string_to_array(COALESCE(namespaces, ''), ',')")
	assert.Contains(t, clause, "trim(both ' ' from member.ns) = ock.namespace")
	assert.Equal(t, strings.Count(clause, "("), strings.Count(clause, ")"))
	require.GreaterOrEqual(t, len(args), 3)
	assert.Greater(t, next, 4)
}
