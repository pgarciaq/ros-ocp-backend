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
	return nil
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
