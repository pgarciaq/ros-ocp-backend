package config

import (
	"strings"

	"github.com/spf13/viper"
)

var tagsFeatureDisabled bool

// TagsFeatureEnabled reports whether ROS_TAGS_ENABLED allows tag sync and list filtering.
// May be turned off at runtime when DB tag health check fails (ROS_TAGS_SOURCE=db).
func TagsFeatureEnabled() bool {
	if tagsFeatureDisabled {
		return false
	}
	return GetConfig().TagsEnabled
}

// DisableTagsFeature turns off tag filtering for the remainder of the process.
func DisableTagsFeature() {
	tagsFeatureDisabled = true
}

// ResetTagsRuntimeForTest clears runtime tag overrides (for tests).
func ResetTagsRuntimeForTest() {
	tagsFeatureDisabled = false
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
	ResetTagsRuntimeForTest()
	cfgMu.Lock()
	defer cfgMu.Unlock()
	viper.Reset()
	cfg = nil
}
