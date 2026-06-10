package tags

import (
	"fmt"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// ValidateTagAuthConfig enforces tag sync authentication settings at startup.
func ValidateTagAuthConfig() error {
	cfg := config.GetConfig()
	devToken := strings.TrimSpace(cfg.TagsDevToken)
	if devToken != "" {
		if config.IsDevelopment() {
			logging.GetLogger().Warn(
				"ROS_TAGS_DEV_TOKEN is set — authentication bypassed for tag sync endpoints (development mode only)",
			)
		} else {
			return fmt.Errorf(
				"ROS_TAGS_DEV_TOKEN is set outside development mode; remove it or set DEVELOPMENT=true for local use only",
			)
		}
	}

	if config.TagsUsePushSync() {
		allowed := strings.TrimSpace(cfg.TagsAllowedServiceAccounts)
		if allowed == "" {
			if config.IsDevelopment() {
				logging.GetLogger().Warn(
					"ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS is empty — any authenticated service account may call tag sync (development mode only)",
				)
			} else {
				return fmt.Errorf(
					"ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS is empty with ROS_TAGS_SOURCE=api; " +
						"set an explicit allowlist of service accounts permitted to push tags",
				)
			}
		}
	}
	return nil
}
