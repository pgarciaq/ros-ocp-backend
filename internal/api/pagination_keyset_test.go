package api

import (
	"strings"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/stretchr/testify/assert"
)

func TestKeysetSeekClause_ASC_NonNilSortValue(t *testing.T) {
	clause, args := keysetSeekClause(
		"estimated_savings_cents", listoptions.OrderAsc,
		"(cluster_uuid, namespace)", int64(500),
		"c1", "ns-a",
	)

	assert.Contains(t, clause, "(estimated_savings_cents) > ?")
	assert.Contains(t, clause, "(estimated_savings_cents) IS NULL")
	assert.Contains(t, clause, "IS NOT DISTINCT FROM ?")
	assert.Contains(t, clause, "(cluster_uuid, namespace) > (?, ?)")
	assert.Equal(t, []interface{}{int64(500), int64(500), "c1", "ns-a"}, args)
}

func TestKeysetSeekClause_DESC_NonNilSortValue(t *testing.T) {
	clause, args := keysetSeekClause(
		"estimated_savings_cents", listoptions.OrderDesc,
		"(cluster_uuid, namespace)", int64(1000),
		"c2", "ns-b",
	)

	assert.Contains(t, clause, "(estimated_savings_cents) < ?")
	assert.Contains(t, clause, "(estimated_savings_cents) IS NULL")
	assert.Contains(t, clause, "IS NOT DISTINCT FROM ?")
	assert.Contains(t, clause, "(cluster_uuid, namespace) > (?, ?)")
	assert.Equal(t, []interface{}{int64(1000), int64(1000), "c2", "ns-b"}, args)
}

func TestKeysetSeekClause_ASC_NilSortValue(t *testing.T) {
	clause, args := keysetSeekClause(
		"estimated_savings_cents", listoptions.OrderAsc,
		"(cluster_uuid, namespace)", nil,
		"c3", "ns-c",
	)

	assert.Contains(t, clause, "(estimated_savings_cents) IS NULL")
	assert.Contains(t, clause, "(cluster_uuid, namespace) > (?, ?)")
	assert.NotContains(t, clause, "> ?")
	assert.NotContains(t, clause, "IS NOT DISTINCT FROM")
	assert.Equal(t, []interface{}{"c3", "ns-c"}, args)
}

func TestKeysetSeekClause_DESC_NilSortValue(t *testing.T) {
	clause, args := keysetSeekClause(
		"estimated_savings_cents", listoptions.OrderDesc,
		"(cluster_uuid, namespace)", nil,
		"c4", "ns-d",
	)

	assert.Contains(t, clause, "(estimated_savings_cents) IS NULL")
	assert.Contains(t, clause, "(cluster_uuid, namespace) > (?, ?)")
	assert.NotContains(t, clause, "< ?")
	assert.NotContains(t, clause, "IS NOT DISTINCT FROM")
	assert.Equal(t, []interface{}{"c4", "ns-d"}, args)
}

func TestKeysetSeekClause_NonNilSortValue_IncludesNullRows(t *testing.T) {
	for _, dir := range []string{listoptions.OrderAsc, listoptions.OrderDesc} {
		t.Run(dir, func(t *testing.T) {
			clause, _ := keysetSeekClause(
				"savings", dir,
				"(id)", int64(42),
				"row-1",
			)
			assert.Contains(t, clause, "(savings) IS NULL",
				"non-nil sort value clause must include IS NULL to capture NULL-valued rows (NULLS LAST)")
		})
	}
}

func TestKeysetSeekClause_NilSortValue_OnlyNullRegion(t *testing.T) {
	for _, dir := range []string{listoptions.OrderAsc, listoptions.OrderDesc} {
		t.Run(dir, func(t *testing.T) {
			clause, _ := keysetSeekClause(
				"savings", dir,
				"(id)", nil,
				"row-2",
			)
			assert.True(t, strings.HasPrefix(clause, "((savings) IS NULL AND"),
				"nil sort value clause must restrict to NULL region only")
		})
	}
}

func TestKeysetSeekClause_PlaceholderCount(t *testing.T) {
	tests := []struct {
		name      string
		sortValue interface{}
		tieArgs   []interface{}
		wantArgs  int
	}{
		{"non-nil with 3 tie args", int64(100), []interface{}{"a", "b", "c"}, 5},
		{"non-nil with 1 tie arg", int64(100), []interface{}{"a"}, 3},
		{"nil with 3 tie args", nil, []interface{}{"a", "b", "c"}, 3},
		{"nil with 1 tie arg", nil, []interface{}{"a"}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clause, args := keysetSeekClause("col", listoptions.OrderAsc, "(tie)", tt.sortValue, tt.tieArgs...)
			assert.Equal(t, tt.wantArgs, len(args))
			placeholderCount := strings.Count(clause, "?")
			assert.Equal(t, tt.wantArgs, placeholderCount,
				"number of ? placeholders must match number of args")
		})
	}
}

func TestPvcSeekSQL_NilSortValue(t *testing.T) {
	cursor := PVCCursor{
		ClusterUUID:         "cluster-1",
		Namespace:           "ns-1",
		PersistentVolumeClaim: "pvc-1",
		SortValue:           nil,
		OrderBy:             "estimated_savings_cents",
	}

	clause, args, nextIdx, err := pvcSeekSQL("estimated_savings_cents", "asc", cursor, true, 1)
	assert.NoError(t, err)
	assert.Contains(t, clause, "IS NULL")
	assert.NotContains(t, clause, "IS NOT DISTINCT FROM")
	assert.Len(t, args, 3)
	assert.Equal(t, 4, nextIdx)
}

func TestPvcSeekSQL_NonNilSortValue(t *testing.T) {
	cursor := PVCCursor{
		ClusterUUID:         "cluster-2",
		Namespace:           "ns-2",
		PersistentVolumeClaim: "pvc-2",
		SortValue:           []byte(`500`),
		OrderBy:             "estimated_savings_cents",
	}

	clause, args, nextIdx, err := pvcSeekSQL("estimated_savings_cents", "desc", cursor, true, 1)
	assert.NoError(t, err)
	assert.Contains(t, clause, "IS NULL")
	assert.Contains(t, clause, "IS NOT DISTINCT FROM")
	assert.Len(t, args, 5)
	assert.Equal(t, 6, nextIdx)
}

func TestPvcSeekSQL_NoSort(t *testing.T) {
	cursor := PVCCursor{
		ClusterUUID:         "cluster-3",
		Namespace:           "ns-3",
		PersistentVolumeClaim: "pvc-3",
	}

	clause, args, nextIdx, err := pvcSeekSQL("estimated_savings_cents", "asc", cursor, false, 1)
	assert.NoError(t, err)
	assert.NotContains(t, clause, "IS NULL")
	assert.Equal(t, []interface{}{"cluster-3", "ns-3", "pvc-3"}, args)
	assert.Equal(t, 4, nextIdx)
}

func TestPvcOrderNulls(t *testing.T) {
	assert.Equal(t, "savings DESC NULLS LAST", pvcOrderNulls("savings", listoptions.OrderDesc))
	assert.Equal(t, "savings ASC NULLS LAST", pvcOrderNulls("savings", listoptions.OrderAsc))
}

func TestPlaceholders(t *testing.T) {
	assert.Equal(t, "", placeholders(0))
	assert.Equal(t, "?", placeholders(1))
	assert.Equal(t, "?, ?", placeholders(2))
	assert.Equal(t, "?, ?, ?", placeholders(3))
	assert.Equal(t, "?, ?, ?, ?, ?", placeholders(5))
}

func TestBindSeekClause(t *testing.T) {
	clause, args, nextIdx := bindSeekClause(
		"((col) > ? OR (col) IS NULL OR ((col) IS NOT DISTINCT FROM ? AND (tie) > (?, ?)))",
		[]interface{}{100, 100, "a", "b"},
		5,
	)
	assert.Contains(t, clause, "$5")
	assert.Contains(t, clause, "$6")
	assert.Contains(t, clause, "$7")
	assert.Contains(t, clause, "$8")
	assert.NotContains(t, clause, "?")
	assert.Equal(t, []interface{}{100, 100, "a", "b"}, args)
	assert.Equal(t, 9, nextIdx)
}

func TestSnapshotSeekSQL_NilSortValue(t *testing.T) {
	cursor := SnapshotCursor{
		ClusterUUID:  "cluster-1",
		Namespace:    "ns-snap",
		SnapshotName: "snap-1",
		SortValue:    nil,
		OrderBy:      "estimated_cost_cents",
	}

	clause, args, nextIdx, err := snapshotSeekSQL("estimated_cost_cents", "asc", cursor, true, 1)
	assert.NoError(t, err)
	assert.Contains(t, clause, "IS NULL")
	assert.NotContains(t, clause, "IS NOT DISTINCT FROM")
	assert.Len(t, args, 3)
	assert.Equal(t, 4, nextIdx)
}

func TestSnapshotSeekSQL_NonNilSortValue(t *testing.T) {
	cursor := SnapshotCursor{
		ClusterUUID:  "cluster-2",
		Namespace:    "ns-snap2",
		SnapshotName: "snap-2",
		SortValue:    []byte(`1200`),
		OrderBy:      "estimated_cost_cents",
	}

	clause, args, nextIdx, err := snapshotSeekSQL("estimated_cost_cents", "desc", cursor, true, 1)
	assert.NoError(t, err)
	assert.Contains(t, clause, "IS NULL")
	assert.Contains(t, clause, "IS NOT DISTINCT FROM")
	assert.Len(t, args, 5)
	assert.Equal(t, 6, nextIdx)
}

func TestSnapshotOrderNulls(t *testing.T) {
	assert.Equal(t, "age_days DESC NULLS LAST", snapshotOrderNulls("age_days", listoptions.OrderDesc))
	assert.Equal(t, "age_days ASC NULLS LAST", snapshotOrderNulls("age_days", listoptions.OrderAsc))
}
