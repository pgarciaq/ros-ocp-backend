package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitNativeListQueryParams_DuplicatesFilterAtomsToDetail(t *testing.T) {
	t.Parallel()

	queryParams := map[string]interface{}{
		"rs.stale = ?":                  false,
		"LOWER(rs.workload_type) = ?":   []string{"deployment"},
		TagFiltersQueryKey:              []TagFilter{{Key: "environment", Values: []string{"production"}}},
	}
	keys, detail := splitNativeListQueryParams(queryParams)

	require.Equal(t, []string{"deployment"}, keys["LOWER(rs.workload_type) = ?"])
	assert.Equal(t, false, keys["rs.stale = ?"], "stale filter must appear in keysParams for ock.is_stale")
	assert.Equal(t, []string{"deployment"}, detail["LOWER(rs.workload_type) = ?"])
	assert.Equal(t, false, detail["rs.stale = ?"], "stale filter must also appear in detailParams for rs.stale")
	_, hasTagFilters := keys[TagFiltersQueryKey]
	assert.False(t, hasTagFilters)
	_, hasTagFiltersDetail := detail[TagFiltersQueryKey]
	assert.False(t, hasTagFiltersDetail)
}

func TestRemapNativeKeysQueryKey_WorkloadType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input, expected string
	}{
		{"rs.workload_type = ?", "ock.workload_type = ?"},
		{"rs.workload_type != ?", "ock.workload_type != ?"},
		{"rs.workload_type ILIKE ? ESCAPE '\\'", "ock.workload_type ILIKE ? ESCAPE '\\'"},
		{"LOWER(rs.workload_type) = ?", "LOWER(ock.workload_type) = ?"},
		{"LOWER(rs.workload_type) != ?", "LOWER(ock.workload_type) != ?"},
	}
	for _, tc := range cases {
		mapped, ok := remapNativeKeysQueryKey(tc.input)
		assert.True(t, ok, "remapNativeKeysQueryKey(%q) should succeed", tc.input)
		assert.Equal(t, tc.expected, mapped, "remapNativeKeysQueryKey(%q)", tc.input)
	}
}
