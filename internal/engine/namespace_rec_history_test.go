package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestListNamespaceRecommendationHistory_Roundtrip(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedNamespaceDigestSeries(t, pool, "ns-hist-api", 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)

	results, err := RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	require.NoError(t, WriteNamespaceRecommendationHistory(ctx, pool, results))

	rows, err := ListNamespaceRecommendationHistory(
		ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, "ns-hist-api", nil, nil, 30,
	)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(rows), 12, "expect at least 6 snapshots x 2 resources")

	cpuRows := 0
	for _, row := range rows {
		assert.Contains(t, []string{"cpu", "memory"}, row.Resource)
		assert.Contains(t, []string{"cost", "performance"}, row.RecommendationType)
		assert.NotEmpty(t, row.Term)
		assert.False(t, row.RecordedAt.IsZero())
		if row.Resource == "cpu" {
			cpuRows++
			assert.NotNil(t, row.Recommended.RequestMillicores)
		}
	}
	assert.Greater(t, cpuRows, 0)
}

func TestListNamespaceRecommendationHistory_FilterTermEngine(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedNamespaceDigestSeries(t, pool, "ns-hist-filter", 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)

	results, err := RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NoError(t, WriteNamespaceRecommendationHistory(ctx, pool, results))

	rows, err := ListNamespaceRecommendationHistory(
		ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, "ns-hist-filter",
		[]string{"short_term"}, []string{"cost"}, 30,
	)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	for _, row := range rows {
		assert.Equal(t, "short_term", row.Term)
		assert.Equal(t, "cost", row.RecommendationType)
	}
}

func TestParseNamespaceHistoryLimit(t *testing.T) {
	limit, err := ParseNamespaceHistoryLimit("")
	assert.NoError(t, err)
	assert.Equal(t, 30, limit)

	limit, err = ParseNamespaceHistoryLimit("10")
	assert.NoError(t, err)
	assert.Equal(t, 10, limit)

	_, err = ParseNamespaceHistoryLimit("nope")
	assert.Error(t, err)
}
