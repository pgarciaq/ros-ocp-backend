package vm

import (
	"context"
	"testing"

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
