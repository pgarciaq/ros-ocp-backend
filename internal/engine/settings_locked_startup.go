package engine

import (
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

// LogSettingsLockedStartup emits a warning when ROS_SETTINGS_LOCKED is enabled and lists opt-outs.
func LogSettingsLockedStartup(logf func(format string, args ...any)) {
	if logf == nil {
		return
	}
	cfg := config.GetConfig()
	if cfg == nil || !cfg.SettingsLocked {
		return
	}
	logf("ROS_SETTINGS_LOCKED=true: all tenant settings overrides will be ignored; compiled defaults enforced")
	var unlocked []string
	check := func(name string, locked bool) {
		if !locked {
			unlocked = append(unlocked, name)
		}
	}
	check("container", cfg.SettingsLockedContainer)
	check("gpu", cfg.SettingsLockedGPU)
	check("node", cfg.SettingsLockedNode)
	check("namespace", cfg.SettingsLockedNamespace)
	check("pvc", cfg.SettingsLockedPVC)
	check("vm", cfg.SettingsLockedVM)
	check("quota", cfg.SettingsLockedQuota)
	check("cluster-quota", cfg.SettingsLockedClusterQuota)
	check("idle_detection", cfg.SettingsLockedIdle)
	check("snapshot", cfg.SettingsLockedSnapshot)
	check("business_hours", cfg.SettingsLockedBusinessHours)
	check("terms", cfg.SettingsLockedTerms)
	if len(unlocked) > 0 {
		logf("ROS_SETTINGS_LOCKED: per-feature opt-out (tenant API allowed): %s", strings.Join(unlocked, ", "))
	}
}
