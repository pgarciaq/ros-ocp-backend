package config

// VisualInsightsEnabled reports whether the ROS_VISUAL_INSIGHTS_ENABLED kill-switch
// allows the OOM timeline endpoint and cpuThrottle field in boxplot responses.
func VisualInsightsEnabled() bool {
	return GetConfig().VisualInsightsEnabled
}

// HourlyVMDigestsEnabled reports whether hourly VM activity heatmap ingestion is active.
func HourlyVMDigestsEnabled() bool {
	return GetConfig().HourlyVMDigestsEnabled
}

// HourlyVMDigestsRetentionDays returns the configured retention days for hourly VM digests.
func HourlyVMDigestsRetentionDays() int {
	days := GetConfig().HourlyVMDigestsRetentionDays
	if days <= 0 {
		return 90
	}
	return days
}
