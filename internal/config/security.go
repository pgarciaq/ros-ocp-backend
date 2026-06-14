package config

import (
	"fmt"
	"strings"
)

// IsDevelopment reports whether DEVELOPMENT=true (local/dev deployments).
func IsDevelopment() bool {
	return GetConfig().Development
}

// ValidateSecurityConfig enforces production security requirements at startup.
func ValidateSecurityConfig() error {
	c := GetConfig()
	if c == nil {
		return nil
	}
	if err := validateCSVSecurity(c); err != nil {
		return err
	}
	if err := validateInternalTagsAuth(c); err != nil {
		return err
	}
	return nil
}

func validateInternalTagsAuth(c *Config) error {
	if c.Development || c.InternalTagsAuthRequired {
		return nil
	}
	return fmt.Errorf(
		"config: ROS_INTERNAL_TAGS_AUTH_REQUIRED must be true in non-development mode; " +
			"internal tag sync and savings recalc endpoints would be unauthenticated",
	)
}

func validateCSVSecurity(c *Config) error {
	allowed := strings.TrimSpace(c.CSVAllowedHosts)
	if allowed != "" {
		return nil
	}
	if IsDevelopment() {
		return nil
	}
	return fmt.Errorf(
		"config: ROS_CSV_ALLOWED_HOSTS is empty in non-development mode; " +
			"CSV URL fetches are blocked to prevent SSRF — set an explicit host allowlist or DEVELOPMENT=true for local use",
	)
}
