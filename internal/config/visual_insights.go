package config

// VisualInsightsEnabled reports whether the ROS_VISUAL_INSIGHTS_ENABLED kill-switch
// allows the OOM timeline endpoint and cpuThrottle field in boxplot responses.
func VisualInsightsEnabled() bool {
	return GetConfig().VisualInsightsEnabled
}
