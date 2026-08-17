package types

// Notification codes matching notification_code_definitions seed data.
const (
	NotifLowConfidence           int16 = 1
	NotifStaleData               int16 = 2
	NotifOOMDetected             int16 = 3
	NotifPDBCaveat               int16 = 4
	NotifIdleWorkload            int16 = 5
	NotifRecApplied              int16 = 6
	NotifNewWorkload             int16 = 7
	NotifMemoryTrendingUp        int16 = 9
	NotifGPUUnderutilized        int16 = 10
	NotifNodeUnderutilized       int16 = 11
	NotifNodeOvercommitted       int16 = 12
	NotifStrandedResources       int16 = 13
	NotifASaturated              int16 = 14
	NotifNodeIdle                int16 = 15
	NotifAFlapping               int16 = 16
	NotifARecommended            int16 = 17
	NotifVMIdle                  int16 = 18
	NotifVMOversized             int16 = 19
	NotifPVCOrphaned             int16 = 20
	NotifHPASaturated            int16 = 21
	NotifHPAActive               int16 = 22
	NotifInstanceNotInCat        int16 = 23
	NotifInstanceDeprecated      int16 = 24
	NotifNoCostData              int16 = 25
	NotifGPUIdle                 int16 = 26
	NotifGPUMemBound             int16 = 27
	NotifGPUNoProfilingData      int16 = 28
	NotifPVCOversized            int16 = 29
	NotifPVCNearFull             int16 = 30
	NotifSnapshotOrphaned        int16 = 31
	NotifSnapshotNeverUsed       int16 = 32
	NotifSnapshotRedundant       int16 = 33
	NotifSnapshotStale           int16 = 34
	NotifSnapshotManaged         int16 = 35
	NotifGPUTimeSharingCandidate int16 = 36
	NotifQuotaNearCapacity       int16 = 70
	NotifQuotaOversized          int16 = 71
	NotifQuotaBlocking           int16 = 72
	NotifClusterQuotaAtCapacity  int16 = 73
	NotifNodePodSchedulingLimit  int16 = 74
	NotifNodeFleetConsolidation  int16 = 76
	NotifSparseData              int16 = 77
	NotifGPUMultiDevice          int16 = 78
	NotifNodeBHNotPeakSafe       int16 = 79
)

// NotificationThresholds holds notification evaluation thresholds derived from sizing settings.
type NotificationThresholds struct {
	LowConfidenceThreshold float32
	SparseDataThreshold    int
	MemTrendSlopeThreshold float64
}

// NotificationThresholdsFromSizing extracts notification thresholds from sizing settings.
func NotificationThresholdsFromSizing(th SizingThresholdSettings) NotificationThresholds {
	return NotificationThresholds{
		LowConfidenceThreshold: th.LowConfidenceThreshold,
		SparseDataThreshold:    th.SparseDataThreshold,
		MemTrendSlopeThreshold: th.MemTrendSlopeThreshold,
	}
}
