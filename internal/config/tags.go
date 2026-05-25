package config

import (
	"strings"

	"github.com/spf13/viper"
)

// TagsFeatureEnabled reports whether ROS_TAGS_ENABLED allows tag sync and list filtering.
func TagsFeatureEnabled() bool {
	return GetConfig().TagsEnabled
}

// TagsSource returns the tag data source: "db" (direct Koku PostgreSQL reads) or "api" (push sync).
func TagsSource() string {
	source := strings.ToLower(strings.TrimSpace(GetConfig().TagsSource))
	if source == "" {
		return "db"
	}
	return source
}

// TagsUsePushSync reports whether Koku should push tags via HTTP (api source).
func TagsUsePushSync() bool {
	return TagsSource() == "api"
}

// ResetTagsForTest clears cached configuration so the next GetConfig() reloads tag env vars.
func ResetTagsForTest() {
	viper.Reset()
	cfg = nil
}
