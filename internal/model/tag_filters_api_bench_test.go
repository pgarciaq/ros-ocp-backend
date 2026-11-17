package model_test

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// BenchmarkTagFilterKeys_APIMode is #814's committed jsonb benchmark (the
// D1 spike's hot-path measurement, made permanent): the shipped both-shapes
// containment filter over a realistically mixed storage world — most rows
// hold sorted arrays (the post-#814 push), every 5th namespace holds the
// scalar legacy shape (a pre-#814 row awaiting its org's next push).
//
// The "pre-814-scalar-predicate" sub-benchmark inlines the old single-value
// predicate over the same rows for an apples-to-apples cost comparison; it
// matches only the legacy subset (correctness was the bug, not the goal
// here — the cost comparison is).
func BenchmarkTagFilterKeys_APIMode(b *testing.B) {
	const (
		namespacesPerCluster = 150
		clusters             = 2
		backgroundKeys       = 30
	)
	pool := testutil.SetupTestDB(b)
	gdb := testutil.OpenTestGORM(pool)
	seedBenchmarkTagWorld(b, pool, namespacesPerCluster, clusters, backgroundKeys)

	// The mixed storage world: arrays everywhere, scalar legacy every 5th
	// namespace (all carry environment=production). bench-ns-<g>-<n>:
	// split_part 4 is the namespace counter n.
	if _, err := pool.Exec(b.Context(), `
		UPDATE org_container_keys
		SET resolved_tags = CASE WHEN (split_part(namespace, '-', 4)::int % 5 = 0)
			THEN '{"environment":"production"}'::jsonb
			ELSE '{"environment":["dev","production","staging"]}'::jsonb END
		WHERE org_id = $1 AND namespace LIKE 'bench-ns-%'
	`, testutil.TestOrgID); err != nil {
		b.Fatal(err)
	}

	config.ResetTagsForTest()
	b.Setenv("ROS_TAGS_ENABLED", "true")
	b.Setenv("ROS_TAGS_SOURCE", "api")

	single := []model.TagFilter{{Key: "environment", Values: []string{"production"}}}
	multi := []model.TagFilter{{Key: "environment", Values: []string{"production", "staging"}}}

	runFilter := func(b *testing.B, filters []model.TagFilter, want int) {
		b.Helper()
		for i := 0; i < b.N; i++ {
			query := gdb.Table("org_container_keys ock").
				Select("ock.cluster_uuid::text, ock.namespace").
				Where("ock.org_id = ?", testutil.TestOrgID)
			query = model.ApplyTagFiltersToKeys(query, testutil.TestOrgID, filters)
			rows, err := query.Rows()
			if err != nil {
				b.Fatal(err)
			}
			count := 0
			for rows.Next() {
				count++
			}
			_ = rows.Close()
			if count != want {
				b.Fatalf("expected %d matches, got %d", want, count)
			}
		}
	}

	b.Run("both-shapes-single-value", func(b *testing.B) {
		runFilter(b, single, namespacesPerCluster*clusters)
	})
	b.Run("both-shapes-any-of-two-values", func(b *testing.B) {
		runFilter(b, multi, namespacesPerCluster*clusters)
	})
	b.Run("pre-814-scalar-predicate", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			query := gdb.Table("org_container_keys ock").
				Select("ock.cluster_uuid::text, ock.namespace").
				Where("ock.org_id = ?", testutil.TestOrgID).
				Where(`ock.resolved_tags @> ?`, `{"environment":"production"}`)
			rows, err := query.Rows()
			if err != nil {
				b.Fatal(err)
			}
			count := 0
			for rows.Next() {
				count++
			}
			_ = rows.Close()
			if count != namespacesPerCluster*clusters/5 {
				b.Fatalf("expected the legacy subset (%d), got %d", namespacesPerCluster*clusters/5, count)
			}
		}
	})
}
