package engine

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

// AllFieldsLockedMarker is returned in locked_fields when ROS_SETTINGS_LOCKED freezes tenant overrides.
const AllFieldsLockedMarker = "*"

// IsSettingsLocked reports whether tenant Settings API writes and DB overrides are disabled
// for the given recommendation or settings feature type. When ROS_SETTINGS_LOCKED is false,
// this always returns false. Per-feature ROS_SETTINGS_LOCKED_* env vars opt out when the global lock is on.
func IsSettingsLocked(recType string) bool {
	cfg := config.GetConfig()
	if cfg == nil || !cfg.SettingsLocked {
		return false
	}
	switch recType {
	case "container":
		return cfg.SettingsLockedContainer
	case "gpu":
		return cfg.SettingsLockedGPU
	case "node":
		return cfg.SettingsLockedNode
	case "namespace":
		return cfg.SettingsLockedNamespace
	case "pvc":
		return cfg.SettingsLockedPVC
	case "vm":
		return cfg.SettingsLockedVM
	case "quota":
		return cfg.SettingsLockedQuota
	case "cluster-quota":
		return cfg.SettingsLockedClusterQuota
	case "idle_detection":
		return cfg.SettingsLockedIdle
	case "snapshot":
		return cfg.SettingsLockedSnapshot
	case "business_hours":
		return cfg.SettingsLockedBusinessHours
	case "terms":
		return cfg.SettingsLockedTerms
	default:
		return true
	}
}

// ShouldSkipTermTenantOverrides returns true when generic /settings/terms tenant rows must not be applied.
func ShouldSkipTermTenantOverrides(recommendationType string) bool {
	_ = recommendationType
	return IsSettingsLocked("terms")
}

// LockedFieldsForAPI merges global settings lock with per-field env locks for GET responses.
func LockedFieldsForAPI(recType string, envLocked []string) []string {
	if IsSettingsLocked(recType) {
		return []string{AllFieldsLockedMarker}
	}
	return envLocked
}
