package engine

import "github.com/redhatinsights/ros-ocp-backend/internal/engine/core"

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
)

const (
	defaultMemTrendSlopeThreshold         = 100.0
	defaultLowConfidenceThreshold float32 = 0.5
	defaultSparseDataThreshold    int     = 2
)

// EvaluateNotifications produces notification codes for a recommendation.
// minDataDays is the minimum data days for the term to be considered reliable.
func EvaluateNotifications(rec ContainerRec, minDataDays int) []int16 {
	return EvaluateNotificationsWithThresholds(rec, minDataDays, NotificationThresholdsFromSizing(defaultContainerSizingThresholds))
}

// EvaluateNotificationsWithThresholds produces notification codes using explicit thresholds.
func EvaluateNotificationsWithThresholds(rec ContainerRec, minDataDays int, th NotificationThresholds) []int16 {
	codes := []int16{}

	if rec.DataDays < 1 {
		codes = append(codes, NotifNewWorkload)
	}
	if rec.ConfidenceLevel < th.LowConfidenceThreshold && rec.DataDays > 0 {
		codes = append(codes, NotifLowConfidence)
	}
	if rec.DataDays > 0 && rec.DataDays <= th.SparseDataThreshold {
		codes = append(codes, NotifSparseData)
	}
	if rec.OOMCountSum > 0 {
		codes = append(codes, NotifOOMDetected)
	}
	if rec.IsIdle || rec.IdleState == IdleStateIdle || rec.IdleState == IdleStateZombie {
		codes = append(codes, NotifIdleWorkload)
	}
	if rec.Stale {
		codes = append(codes, NotifStaleData)
	}
	if rec.MemTrendSlope > th.MemTrendSlopeThreshold {
		codes = append(codes, NotifMemoryTrendingUp)
	}

	return codes
}
