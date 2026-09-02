package vm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestEnrichVMDetailWithBusinessHours_NilPoolOmits(t *testing.T) {
	t.Parallel()
	got, err := EnrichVMDetailWithBusinessHours(context.Background(), nil, "org", "cluster", "vm", "ns", "short_term", "cost")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestEnrichVMDetailWithBusinessHours_FeatureOffOmits(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "false")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)
	_ = config.GetConfig()

	got, err := EnrichVMDetailWithBusinessHours(context.Background(), nil, "org", "cluster", "vm", "ns", "short_term", "cost")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestFilterVMDigestsSince(t *testing.T) {
	t.Parallel()
	day := func(s string) time.Time {
		d, err := time.Parse("2006-01-02", s)
		require.NoError(t, err)
		return d
	}
	rows := []Digest{
		{BucketDate: day("2026-08-01")},
		{BucketDate: day("2026-09-01")},
		{BucketDate: day("2026-09-10")},
	}
	got := FilterVMDigestsSince(rows, day("2026-09-01"))
	require.Len(t, got, 2)
	assert.Equal(t, "2026-09-01", got[0].BucketDate.Format("2006-01-02"))
	assert.Equal(t, "2026-09-10", got[1].BucketDate.Format("2006-01-02"))
}
