package config

import (
	"strings"
)

const (
	warnInternalTagsAuthNoAllowlist = "internal tags auth enabled but no SAs allowlisted (ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS); all internal calls will be rejected"
	warnCORSOpenInProduction        = "CORS allows all origins in production; consider restricting ROS_CORS_ALLOWED_ORIGINS"
	warnInternalOrgAllowlistNoAuth  = "org allowlist set (ROS_INTERNAL_ALLOWED_ORGS) but internal auth is disabled (ROS_INTERNAL_TAGS_AUTH_REQUIRED=false); allowlist has no effect"
)

// ConfigValidationWarnings returns non-fatal misconfiguration messages for startup logging.
func ConfigValidationWarnings(c *Config) []string {
	if c == nil {
		return nil
	}
	var warnings []string
	if c.InternalTagsAuthRequired && strings.TrimSpace(c.TagsAllowedServiceAccounts) == "" {
		warnings = append(warnings, warnInternalTagsAuthNoAllowlist)
	}
	if !c.Development && corsMisconfiguredInProduction(c) {
		warnings = append(warnings, warnCORSOpenInProduction)
	}
	if strings.TrimSpace(c.InternalAllowedOrgs) != "" && !c.InternalTagsAuthRequired {
		warnings = append(warnings, warnInternalOrgAllowlistNoAuth)
	}
	return warnings
}

func corsMisconfiguredInProduction(c *Config) bool {
	raw := strings.TrimSpace(c.CORSAllowedOrigins)
	if raw == "" {
		return true
	}
	for _, part := range strings.Split(raw, ",") {
		if strings.TrimSpace(part) == "*" {
			return true
		}
	}
	return false
}

// ValidateConfig returns non-fatal configuration warnings for startup logging.
func ValidateConfig() []string {
	return ConfigValidationWarnings(GetConfig())
}
