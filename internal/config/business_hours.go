package config

import "github.com/spf13/viper"

// BusinessHoursFeatureEnabled reports whether the ROS_BUSINESS_HOURS_ENABLED kill-switch
// allows business-hours API surface and ingestion (schedules remain in DB when false).
func BusinessHoursFeatureEnabled() bool {
	return GetConfig().BusinessHoursEnabled
}

// ResetForTest clears cached configuration so the next GetConfig() reloads from the environment.
// Intended for tests in other packages that mutate ROS_BUSINESS_HOURS_ENABLED.
func ResetForTest() {
	viper.Reset()
	cfg = nil
}
