package engine

// Notification codes matching notification_code_definitions seed data.
const (
	NotifLowConfidence      int16 = 1
	NotifStaleData          int16 = 2
	NotifOOMDetected        int16 = 3
	NotifPDBCaveat          int16 = 4
	NotifIdleWorkload       int16 = 5
	NotifRecApplied         int16 = 6
	NotifNewWorkload        int16 = 7
	// Code 8 (ABANDONED_WORKLOAD) removed — zombie classification via idle_state supersedes it.
	NotifMemoryTrendingUp   int16 = 9
	NotifGPUUnderutilized   int16 = 10
	NotifNodeUnderutilized  int16 = 11
	NotifNodeOvercommitted  int16 = 12
	NotifStrandedResources  int16 = 13
	NotifASaturated         int16 = 14
	NotifNodeIdle           int16 = 15
	NotifAFlapping          int16 = 16
	NotifARecommended       int16 = 17
	NotifVMIdle             int16 = 18
	NotifVMOversized        int16 = 19
	NotifPVCOrphaned        int16 = 20
	NotifHPASaturated       int16 = 21
	NotifHPAActive          int16 = 22
	NotifInstanceNotInCat   int16 = 23
	NotifInstanceDeprecated int16 = 24
	NotifNoCostData         int16 = 25
	NotifGPUIdle            int16 = 26
	NotifGPUMemBound        int16 = 27
	NotifGPUNoProfilingData int16 = 28
	NotifPVCOversized       int16 = 29
	NotifPVCNearFull        int16 = 30
	NotifSnapshotOrphaned   int16 = 31
	NotifSnapshotNeverUsed  int16 = 32
	NotifSnapshotRedundant  int16 = 33
	NotifSnapshotStale      int16 = 34
	NotifSnapshotManaged    int16 = 35
	// NotifNodePodSchedulingLimit indicates the node is near its max pod scheduling capacity.
	NotifNodePodSchedulingLimit int16 = 74
	// NotifNodeFleetConsolidation indicates fleet-level node removal within a MachineSet.
	NotifNodeFleetConsolidation int16 = 76
	// NotifSparseData indicates recommendation based on limited absolute data volume.
	NotifSparseData int16 = 77
	// NotifGPUMultiDevice indicates the container uses multiple GPUs (distributed training);
	// MIG downsizing is not recommended.
	NotifGPUMultiDevice int16 = 78
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
