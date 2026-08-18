package engine

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	libcontainer "github.com/redhatinsights/ros-ocp-backend/librobne/container"
)

// Notification codes — canonical definitions live in core; re-exported here for backward compat.
const (
	NotifLowConfidence          = core.NotifLowConfidence
	NotifStaleData              = core.NotifStaleData
	NotifOOMDetected            = core.NotifOOMDetected
	NotifPDBCaveat              = core.NotifPDBCaveat
	NotifIdleWorkload           = core.NotifIdleWorkload
	NotifRecApplied             = core.NotifRecApplied
	NotifNewWorkload            = core.NotifNewWorkload
	NotifMemoryTrendingUp       = core.NotifMemoryTrendingUp
	NotifGPUUnderutilized       = core.NotifGPUUnderutilized
	NotifNodeUnderutilized      = core.NotifNodeUnderutilized
	NotifNodeOvercommitted      = core.NotifNodeOvercommitted
	NotifStrandedResources      = core.NotifStrandedResources
	NotifASaturated             = core.NotifASaturated
	NotifNodeIdle               = core.NotifNodeIdle
	NotifAFlapping              = core.NotifAFlapping
	NotifARecommended           = core.NotifARecommended
	NotifVMIdle                 = core.NotifVMIdle
	NotifVMOversized            = core.NotifVMOversized
	NotifPVCOrphaned            = core.NotifPVCOrphaned
	NotifHPASaturated           = core.NotifHPASaturated
	NotifHPAActive              = core.NotifHPAActive
	NotifInstanceNotInCat       = core.NotifInstanceNotInCat
	NotifInstanceDeprecated     = core.NotifInstanceDeprecated
	NotifNoCostData             = core.NotifNoCostData
	NotifGPUIdle                = core.NotifGPUIdle
	NotifGPUMemBound            = core.NotifGPUMemBound
	NotifGPUNoProfilingData     = core.NotifGPUNoProfilingData
	NotifPVCOversized           = core.NotifPVCOversized
	NotifPVCNearFull            = core.NotifPVCNearFull
	NotifSnapshotOrphaned       = core.NotifSnapshotOrphaned
	NotifSnapshotNeverUsed      = core.NotifSnapshotNeverUsed
	NotifSnapshotRedundant      = core.NotifSnapshotRedundant
	NotifSnapshotStale          = core.NotifSnapshotStale
	NotifSnapshotManaged        = core.NotifSnapshotManaged
	NotifNodePodSchedulingLimit = core.NotifNodePodSchedulingLimit
	NotifNodeFleetConsolidation = core.NotifNodeFleetConsolidation
	NotifSparseData             = core.NotifSparseData
	NotifGPUMultiDevice         = core.NotifGPUMultiDevice
	NotifNodeBHNotPeakSafe      = core.NotifNodeBHNotPeakSafe
	NotifGPUBHOfficeWindow      = core.NotifGPUBHOfficeWindow
	NotifGPUTSBHClusterWindow   = core.NotifGPUTSBHClusterWindow
)

// EvaluateNotifications produces notification codes for a recommendation
// using the default container sizing thresholds from the settings cache.
func EvaluateNotifications(rec ContainerRec, minDataDays int) []int16 {
	return libcontainer.EvaluateNotificationsWithThresholds(rec, minDataDays, core.NotificationThresholdsFromSizing(defaultContainerSizingThresholds))
}

// EvaluateNotificationsWithThresholds delegates to librobne/container.
var EvaluateNotificationsWithThresholds = libcontainer.EvaluateNotificationsWithThresholds
