package container

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

func TestCrashLoopNotificationBoundary(t *testing.T) {
	th := types.NotificationThresholds{CrashLoopRestartThreshold: 3}
	rec2 := types.ContainerRec{RestartCountSum: 2, DataDays: 3}
	assert.NotContains(t, EvaluateNotificationsWithThresholds(rec2, 3, th), types.NotifUnstableContainer)
	rec3 := types.ContainerRec{RestartCountSum: 3, DataDays: 3}
	assert.Contains(t, EvaluateNotificationsWithThresholds(rec3, 3, th), types.NotifUnstableContainer)
	recZero := types.ContainerRec{RestartCountSum: 3, DataDays: 3}
	assert.Contains(t, EvaluateNotificationsWithThresholds(recZero, 3, types.NotificationThresholds{}), types.NotifUnstableContainer) // zero means 3
}

func TestCrashLoopNotification_FiresAlongsideOOM(t *testing.T) {
	th := types.NotificationThresholds{CrashLoopRestartThreshold: 3}
	rec := types.ContainerRec{RestartCountSum: 5, OOMCountSum: 2, DataDays: 7}
	codes := EvaluateNotificationsWithThresholds(rec, 3, th)
	assert.Contains(t, codes, types.NotifUnstableContainer)
	assert.Contains(t, codes, types.NotifOOMDetected)
}
