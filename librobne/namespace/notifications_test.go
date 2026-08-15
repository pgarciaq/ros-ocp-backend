package namespace

import (
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/stretchr/testify/assert"
)

func TestEvaluateNamespaceNotifications_NewWorkloadAndNoOOM(t *testing.T) {
	codes := EvaluateNamespaceNotifications(NamespaceRec{DataDays: 0, ConfidenceLevel: 0})
	assert.Contains(t, codes, types.NotifNewWorkload)
	assert.NotContains(t, codes, types.NotifOOMDetected)
	assert.NotContains(t, codes, types.NotifIdleWorkload)
}

func TestEvaluateNamespaceNotifications_MemoryTrendingUp(t *testing.T) {
	codes := EvaluateNamespaceNotifications(NamespaceRec{
		DataDays:        10,
		ConfidenceLevel: 0.8,
		MemTrendSlope:   600,
	})
	assert.Contains(t, codes, types.NotifMemoryTrendingUp)
}
