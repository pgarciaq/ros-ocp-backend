package model_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
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
	// #814 D2: both-shapes containment — no ->> extraction (broken on arrays).
	assert.NotContains(t, clause, "->>")
	assert.Contains(t, clause, "ock.resolved_tags @> $3::jsonb")
	assert.Contains(t, clause, "ock.resolved_tags @> $6::jsonb")
	// orgID ($2) + two values × two arms (array + scalar) = five args.
	require.Len(t, args, 5)
	assert.Equal(t, "1234567", args[0])
	assert.Equal(t, `{"environment":["production"]}`, args[1])
	assert.Equal(t, `{"environment":"production"}`, args[2])
	assert.Equal(t, `{"environment":["staging"]}`, args[3])
	assert.Equal(t, `{"environment":"staging"}`, args[4])
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
	assert.Contains(t, clause, "reporting_ocpusagepodlabel_summary")
	assert.Contains(t, clause, "reporting_ocpstoragevolumelabel_summary")
	assert.Contains(t, clause, "reporting_enabledtagkeys")
	assert.Contains(t, clause, "ls.key = $")
	assert.Contains(t, clause, " = ANY(ls.values)")
	require.GreaterOrEqual(t, len(args), 4)
	assert.Greater(t, next, 1)
}

func TestApplyDBTagFiltersToClusterNamespace_DirectTagTableJoin(t *testing.T) {
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "db")

	filters := []model.TagFilter{{Key: "environment", Values: []string{"production"}}}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DryRun: true})
	require.NoError(t, err)

	query := db.Table("namespace_recommendation_sets ns")
	query = model.ApplyTagFiltersToClusterNamespace(query, "1234567", filters, "ns.cluster_uuid", "ns.namespace_name")

	sql := query.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return tx.Find(nil)
	})
	assert.Contains(t, sql, "reporting_ocpusagepodlabel_summary")
	assert.Contains(t, sql, "reporting_ocpstoragevolumelabel_summary")
	assert.Contains(t, sql, "ns.cluster_uuid")
	assert.Contains(t, sql, "ns.namespace_name")
	assert.NotContains(t, sql, "org_container_keys")
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

// TestAPITagFilters_WildcardRendersExecutableSQL pins the api-mode wildcard
// predicate's SQL shape (#872). gorm consumes every "?" in a predicate string
// as a bind placeholder, so the jsonb key-existence operator cannot be spelled
// directly: writing "column ? ?" makes gorm bind the operator's left operand
// and renders "column $2 ?" — a statement Postgres rejects, which the list
// endpoints surfaced as 503.
//
// The postgres dialector is deliberate: sqlite renders "?" placeholders, which
// would hide exactly the collision this test exists to catch.
func TestAPITagFilters_WildcardRendersExecutableSQL(t *testing.T) {
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "api")
	require.True(t, config.TagsFeatureEnabled())
	t.Cleanup(config.ResetTagsForTest)

	// DryRun with no reachable server: the statement is built, never sent.
	gdb, err := gorm.Open(
		postgres.New(postgres.Config{DSN: "host=127.0.0.1 user=none dbname=none"}),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true},
	)
	require.NoError(t, err)

	filters := []model.TagFilter{{Key: "environment", Values: []string{"*"}}}

	for _, tc := range []struct {
		name   string
		table  string
		column string
		apply  func(*gorm.DB) *gorm.DB
	}{
		{
			name:   "container keys",
			table:  "org_container_keys ock",
			column: "ock.resolved_tags",
			apply: func(q *gorm.DB) *gorm.DB {
				return model.ApplyTagFiltersToKeys(q, testutil.TestOrgID, filters)
			},
		},
		{
			name:   "namespace keys",
			table:  "org_namespace_keys onk",
			column: "onk.resolved_tags",
			apply: func(q *gorm.DB) *gorm.DB {
				return model.ApplyTagFiltersToNSKeys(q, testutil.TestOrgID, filters)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out []map[string]interface{}
			tx := tc.apply(
				gdb.Table(tc.table).Select(tc.column).Where("org_id = ?", testutil.TestOrgID),
			).Find(&out)
			require.NoError(t, tx.Error)
			sql := tx.Statement.SQL.String()

			// Pre-fix this renders "ock.resolved_tags $2 ?": the operator position
			// is consumed as a bind, so Postgres never sees a valid predicate —
			// the wildcard cannot execute at all.
			require.Contains(t, sql, "jsonb_exists("+tc.column+", $",
				"wildcard must use the function form; rendered SQL: %s", sql)

			// A surviving "?" is the operator gorm failed to place, i.e. the
			// malformed statement itself.
			require.NotContains(t, sql, "?",
				"rendered SQL must carry no stray '?'; got: %s", sql)

			// The key travels as a bind, never as SQL text.
			require.NotEmpty(t, tx.Statement.Vars)
			assert.Equal(t, "environment", tx.Statement.Vars[len(tx.Statement.Vars)-1],
				"the tag key must stay a bind parameter; vars: %v", tx.Statement.Vars)
		})
	}
}
