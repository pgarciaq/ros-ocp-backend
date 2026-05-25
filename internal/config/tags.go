package config

import "github.com/spf13/viper"

// TagsFeatureEnabled reports whether ROS_TAGS_ENABLED allows tag sync and list filtering.
func TagsFeatureEnabled() bool {
	return GetConfig().TagsEnabled
}

// ResetTagsForTest clears cached configuration so the next GetConfig() reloads tag env vars.
func ResetTagsForTest() {
	viper.Reset()
	cfg = nil
}
