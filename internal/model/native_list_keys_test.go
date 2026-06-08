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
	_, hasStaleKeys := keys["rs.stale = ?"]
	assert.False(t, hasStaleKeys, "detail-only keys must not appear in keysParams")
	assert.Equal(t, []string{"deployment"}, detail["LOWER(rs.workload_type) = ?"])
	assert.Equal(t, false, detail["rs.stale = ?"])
	_, hasTagFilters := keys[TagFiltersQueryKey]
	assert.False(t, hasTagFilters)
	_, hasTagFiltersDetail := detail[TagFiltersQueryKey]
	assert.False(t, hasTagFiltersDetail)
}
