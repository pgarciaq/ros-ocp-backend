package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// Negative Limits must never panic the pgx scans. GORM honors Limit(-1) as
// limit-cancellation and runs unbounded; other negatives flow through to the
// query layer — either way the scan capacity hint is clamped and streaming
// proceeds normally (#561). Unreachable via the API (ListAPIOptions rejects
// negatives) — only direct in-process misuse. Pre-fix, both cases panic with
// makeslice: len out of range.
func TestGetRecommendationQuality_NegativeLimitDoesNotPanic(t *testing.T) {
	testutil.SetupTestDB(t)

	for _, limit := range []int{-1, -5} {
		opts := listoptions.ListOptions{Limit: limit, Offset: 0, OrderBy: "q.measured_at", OrderHow: "desc"}
		rows, total, err := GetRecommendationQuality("test-org-no-rows", opts, map[string]interface{}{}, nil)
		require.NoError(t, err, "limit %d must not error", limit)
		assert.Empty(t, rows, "limit %d on empty tables must return no rows", limit)
		assert.Equal(t, 0, total, "limit %d on empty tables must count zero", limit)
	}
}
