package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSecurityConfig_RequiresCSVAllowlist(t *testing.T) {
	ResetForTest()
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "")
	_ = GetConfig()

	err := ValidateSecurityConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ROS_CSV_ALLOWED_HOSTS")
}

func TestValidateSecurityConfig_AllowsEmptyAllowlistInDevelopment(t *testing.T) {
	ResetForTest()
	t.Setenv("DEVELOPMENT", "true")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "")
	_ = GetConfig()

	err := ValidateSecurityConfig()
	require.NoError(t, err)
}
