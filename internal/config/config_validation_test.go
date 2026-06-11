package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigValidationWarnings_InternalTagsAuthWithoutAllowlist(t *testing.T) {
	ResetForTest()
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "true")
	t.Setenv("ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS", "")
	cfg := GetConfig()

	warnings := ConfigValidationWarnings(cfg)
	assert.Contains(t, warnings, warnInternalTagsAuthNoAllowlist)
}

func TestConfigValidationWarnings_CORSWildcardInProduction(t *testing.T) {
	ResetForTest()
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_CORS_ALLOWED_ORIGINS", "*")
	cfg := GetConfig()

	warnings := ConfigValidationWarnings(cfg)
	assert.Contains(t, warnings, warnCORSOpenInProduction)
}

func TestConfigValidationWarnings_CORSEmptyInProduction(t *testing.T) {
	ResetForTest()
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_CORS_ALLOWED_ORIGINS", "")
	cfg := GetConfig()

	warnings := ConfigValidationWarnings(cfg)
	assert.Contains(t, warnings, warnCORSOpenInProduction)
}

func TestConfigValidationWarnings_InternalOrgAllowlistWithoutAuth(t *testing.T) {
	ResetForTest()
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "false")
	t.Setenv("ROS_INTERNAL_ALLOWED_ORGS", "1234567,7654321")
	cfg := GetConfig()

	warnings := ConfigValidationWarnings(cfg)
	assert.Contains(t, warnings, warnInternalOrgAllowlistNoAuth)
}

func TestConfigValidationWarnings_NoWarningsInHealthyProduction(t *testing.T) {
	ResetForTest()
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "true")
	t.Setenv("ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS", "cost-onprem-koku-api")
	t.Setenv("ROS_CORS_ALLOWED_ORIGINS", "https://console.example.com")
	t.Setenv("ROS_INTERNAL_ALLOWED_ORGS", "")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "s3.example.com")
	cfg := GetConfig()

	warnings := ConfigValidationWarnings(cfg)
	assert.Empty(t, warnings)
}

func TestConfigValidationWarnings_DevelopmentSkipsCORSWarning(t *testing.T) {
	ResetForTest()
	t.Setenv("DEVELOPMENT", "true")
	t.Setenv("ROS_CORS_ALLOWED_ORIGINS", "")
	cfg := GetConfig()

	warnings := ConfigValidationWarnings(cfg)
	for _, w := range warnings {
		assert.NotContains(t, w, "CORS")
	}
}
