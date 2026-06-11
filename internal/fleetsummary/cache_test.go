package fleetsummary

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

func TestCache_GetPutInvalidate(t *testing.T) {
	config.ResetForTest()
	ResetForTest()
	t.Setenv("ROS_FLEET_SUMMARY_CACHE_TTL", "300")

	orgID := "1234567"
	summary := CachedSummary{
		TotalContainers:     10,
		Currency:            "USD",
		TotalMonthlySavings: money.FormatUSDToAmount(0, "USD"),
	}

	Put(orgID, false, nil, summary)
	got, ok := Get(orgID, false, nil)
	assert.True(t, ok)
	assert.Equal(t, 10, got.TotalContainers)

	InvalidateOrg(orgID)
	_, ok = Get(orgID, false, nil)
	assert.False(t, ok)
}
