package snapshot

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/stretchr/testify/assert"
)

func TestSnapshotCostCentsConversion(t *testing.T) {
	// 10 GiB at $0.05/GiB/month = $0.50/month = 50 cents
	restoreSizeBytes := int64(10 * 1024 * 1024 * 1024)
	settings := SnapshotSettings{CostPerGiBMonth: 0.05}

	gib := float64(restoreSizeBytes) / (1024 * 1024 * 1024)
	assert.InDelta(t, 10.0, gib, 0.001)
	assert.Equal(t, int64(50), money.USDToCents(gib*settings.CostPerGiBMonth))
}
