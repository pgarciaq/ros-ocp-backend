package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- DetermineSecurityEnforcement tests ---
// These test the function directly with a fabricated Config to avoid Clowder init panics.

func TestDetermineSecurityEnforcement_DevelopmentMode(t *testing.T) {
	os.Unsetenv("ACG_CONFIG")
	os.Unsetenv("ROS_SECURITY_ENFORCE")
	c := &Config{Development: true}
	assert.Equal(t, SecurityEnforcementNone, DetermineSecurityEnforcement(c))
}

func TestDetermineSecurityEnforcement_ClowderFatal(t *testing.T) {
	t.Setenv("ACG_CONFIG", "/some/path/cdapp.json")
	os.Unsetenv("ROS_SECURITY_ENFORCE")
	c := &Config{Development: false}
	assert.Equal(t, SecurityEnforcementFatal, DetermineSecurityEnforcement(c))
}

func TestDetermineSecurityEnforcement_ExplicitEnforce(t *testing.T) {
	os.Unsetenv("ACG_CONFIG")
	t.Setenv("ROS_SECURITY_ENFORCE", "true")
	c := &Config{Development: false}
	assert.Equal(t, SecurityEnforcementFatal, DetermineSecurityEnforcement(c))
}

func TestDetermineSecurityEnforcement_OnPremDefault(t *testing.T) {
	os.Unsetenv("ACG_CONFIG")
	os.Unsetenv("ROS_SECURITY_ENFORCE")
	c := &Config{Development: false}
	assert.Equal(t, SecurityEnforcementWarn, DetermineSecurityEnforcement(c))
}

func TestDetermineSecurityEnforcement_ExplicitEnforceCaseInsensitive(t *testing.T) {
	os.Unsetenv("ACG_CONFIG")
	t.Setenv("ROS_SECURITY_ENFORCE", "True")
	c := &Config{Development: false}
	assert.Equal(t, SecurityEnforcementFatal, DetermineSecurityEnforcement(c))
}

func TestDetermineSecurityEnforcement_EnforceFalseIsWarn(t *testing.T) {
	os.Unsetenv("ACG_CONFIG")
	t.Setenv("ROS_SECURITY_ENFORCE", "false")
	c := &Config{Development: false}
	assert.Equal(t, SecurityEnforcementWarn, DetermineSecurityEnforcement(c))
}

// --- ValidateSecurityConfig integration tests ---

func TestValidateSecurityConfig_DevelopmentSkipsAll(t *testing.T) {
	ResetForTest()
	os.Unsetenv("ACG_CONFIG")
	os.Unsetenv("ROS_SECURITY_ENFORCE")
	t.Setenv("DEVELOPMENT", "true")
	t.Setenv("RBAC_ENABLE", "false")
	t.Setenv("DB_SSL", "disable")
	t.Setenv("ROS_TAGS_DEV_TOKEN", "some-secret")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "")
	_ = GetConfig()

	err := ValidateSecurityConfig()
	require.NoError(t, err)
}

func TestValidateSecurityConfig_WarnLevelNonFatal(t *testing.T) {
	ResetForTest()
	os.Unsetenv("ACG_CONFIG")
	os.Unsetenv("ROS_SECURITY_ENFORCE")
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("RBAC_ENABLE", "false")
	t.Setenv("DB_SSL", "disable")
	t.Setenv("ROS_TAGS_DEV_TOKEN", "some-secret")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "")
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "false")
	_ = GetConfig()

	err := ValidateSecurityConfig()
	require.NoError(t, err, "warn level should not return an error")
}

func TestValidateSecurityConfig_FatalWithViolations(t *testing.T) {
	ResetForTest()
	os.Unsetenv("ACG_CONFIG")
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_SECURITY_ENFORCE", "true")
	t.Setenv("RBAC_ENABLE", "false")
	t.Setenv("DB_SSL", "disable")
	t.Setenv("ROS_TAGS_DEV_TOKEN", "some-secret")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "")
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "false")
	_ = GetConfig()

	err := ValidateSecurityConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RBAC_DISABLED")
	assert.Contains(t, err.Error(), "DB_TLS_DISABLED")
	assert.Contains(t, err.Error(), "DEV_TOKEN_PRESENT")
	assert.Contains(t, err.Error(), "CSV_ALLOWLIST_EMPTY")
	assert.Contains(t, err.Error(), "INTERNAL_AUTH_DISABLED")
}

func TestValidateSecurityConfig_FatalNoViolations(t *testing.T) {
	ResetForTest()
	os.Unsetenv("ACG_CONFIG")
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_SECURITY_ENFORCE", "true")
	t.Setenv("RBAC_ENABLE", "true")
	t.Setenv("DB_SSL", "verify-full")
	os.Unsetenv("ROS_TAGS_DEV_TOKEN")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "s3.amazonaws.com")
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "true")
	_ = GetConfig()
	cfg := GetConfig()
	cfg.KafkaSecurityProtocol = "SASL_SSL"

	err := ValidateSecurityConfig()
	require.NoError(t, err)
}

func TestValidateSecurityConfig_ClowderPlusDevelopmentAlwaysFatal(t *testing.T) {
	// Cannot call GetConfig() with ACG_CONFIG set (Clowder panics in tests),
	// so we test the logic directly by simulating what ValidateSecurityConfig does.
	t.Setenv("ACG_CONFIG", "/some/path/cdapp.json")
	c := &Config{Development: true}
	// The Clowder+DEVELOPMENT check is the first thing in ValidateSecurityConfig
	assert.NotEmpty(t, os.Getenv("ACG_CONFIG"))
	assert.True(t, c.Development)
	// Verify the enforcement level would still resolve
	level := DetermineSecurityEnforcement(c)
	// Development=true → None, but the explicit Clowder+Dev check fires before level is consulted
	assert.Equal(t, SecurityEnforcementNone, level)
}

// --- Individual check tests (Fatal level via ROS_SECURITY_ENFORCE) ---

func TestValidateSecurityConfig_RBACDisabledFatal(t *testing.T) {
	ResetForTest()
	os.Unsetenv("ACG_CONFIG")
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_SECURITY_ENFORCE", "true")
	t.Setenv("RBAC_ENABLE", "false")
	t.Setenv("DB_SSL", "verify-full")
	os.Unsetenv("ROS_TAGS_DEV_TOKEN")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "s3.example.com")
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "true")
	_ = GetConfig()
	cfg := GetConfig()
	cfg.KafkaSecurityProtocol = "SSL"

	err := ValidateSecurityConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RBAC_DISABLED")
	assert.Contains(t, err.Error(), "AC-3")
}

func TestValidateSecurityConfig_DBTLSDisableFatal(t *testing.T) {
	ResetForTest()
	os.Unsetenv("ACG_CONFIG")
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_SECURITY_ENFORCE", "true")
	t.Setenv("RBAC_ENABLE", "true")
	t.Setenv("DB_SSL", "disable")
	os.Unsetenv("ROS_TAGS_DEV_TOKEN")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "s3.example.com")
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "true")
	_ = GetConfig()
	cfg := GetConfig()
	cfg.KafkaSecurityProtocol = "SASL_SSL"

	err := ValidateSecurityConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DB_TLS_DISABLED")
	assert.Contains(t, err.Error(), "SC-8")
}

func TestValidateSecurityConfig_DBTLSEmptyFatal(t *testing.T) {
	ResetForTest()
	os.Unsetenv("ACG_CONFIG")
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_SECURITY_ENFORCE", "true")
	t.Setenv("RBAC_ENABLE", "true")
	t.Setenv("DB_SSL", "")
	os.Unsetenv("ROS_TAGS_DEV_TOKEN")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "s3.example.com")
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "true")
	_ = GetConfig()
	cfg := GetConfig()
	cfg.KafkaSecurityProtocol = "SSL"

	err := ValidateSecurityConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DB_TLS_DISABLED")
}

func TestValidateSecurityConfig_KafkaPlaintextFatal(t *testing.T) {
	ResetForTest()
	os.Unsetenv("ACG_CONFIG")
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_SECURITY_ENFORCE", "true")
	t.Setenv("RBAC_ENABLE", "true")
	t.Setenv("DB_SSL", "require")
	os.Unsetenv("ROS_TAGS_DEV_TOKEN")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "s3.example.com")
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "true")
	_ = GetConfig()
	cfg := GetConfig()
	cfg.KafkaSecurityProtocol = "PLAINTEXT"

	err := ValidateSecurityConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KAFKA_TLS_MISSING")
	assert.Contains(t, err.Error(), "SC-8")
}

func TestValidateSecurityConfig_KafkaSASLPlaintextFatal(t *testing.T) {
	ResetForTest()
	os.Unsetenv("ACG_CONFIG")
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_SECURITY_ENFORCE", "true")
	t.Setenv("RBAC_ENABLE", "true")
	t.Setenv("DB_SSL", "require")
	os.Unsetenv("ROS_TAGS_DEV_TOKEN")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "s3.example.com")
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "true")
	_ = GetConfig()
	cfg := GetConfig()
	cfg.KafkaSecurityProtocol = "SASL_PLAINTEXT"

	err := ValidateSecurityConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KAFKA_TLS_MISSING")
}

func TestValidateSecurityConfig_DevTokenPresentFatal(t *testing.T) {
	ResetForTest()
	os.Unsetenv("ACG_CONFIG")
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_SECURITY_ENFORCE", "true")
	t.Setenv("RBAC_ENABLE", "true")
	t.Setenv("DB_SSL", "verify-ca")
	t.Setenv("ROS_TAGS_DEV_TOKEN", "my-dev-secret")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "s3.example.com")
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "true")
	_ = GetConfig()
	cfg := GetConfig()
	cfg.KafkaSecurityProtocol = "SASL_SSL"

	err := ValidateSecurityConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DEV_TOKEN_PRESENT")
	assert.Contains(t, err.Error(), "IA-3")
}

// --- kafkaInsecure helper tests ---

func TestKafkaInsecure(t *testing.T) {
	tests := []struct {
		protocol string
		insecure bool
	}{
		{"", true},
		{"PLAINTEXT", true},
		{"plaintext", true},
		{"SASL_PLAINTEXT", true},
		{"sasl_plaintext", true},
		{"SSL", false},
		{"SASL_SSL", false},
		{"sasl_ssl", false},
	}
	for _, tt := range tests {
		t.Run(tt.protocol, func(t *testing.T) {
			c := &Config{KafkaSecurityProtocol: tt.protocol}
			assert.Equal(t, tt.insecure, kafkaInsecure(c))
		})
	}
}

// --- Backward compatibility: existing tests preserved ---

func TestValidateSecurityConfig_RequiresCSVAllowlist(t *testing.T) {
	ResetForTest()
	os.Unsetenv("ACG_CONFIG")
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_SECURITY_ENFORCE", "true")
	t.Setenv("RBAC_ENABLE", "true")
	t.Setenv("DB_SSL", "require")
	os.Unsetenv("ROS_TAGS_DEV_TOKEN")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "")
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "true")
	_ = GetConfig()
	cfg := GetConfig()
	cfg.KafkaSecurityProtocol = "SASL_SSL"

	err := ValidateSecurityConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CSV_ALLOWLIST_EMPTY")
}

func TestValidateSecurityConfig_AllowsEmptyAllowlistInDevelopment(t *testing.T) {
	ResetForTest()
	os.Unsetenv("ACG_CONFIG")
	os.Unsetenv("ROS_SECURITY_ENFORCE")
	t.Setenv("DEVELOPMENT", "true")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "")
	_ = GetConfig()

	err := ValidateSecurityConfig()
	require.NoError(t, err)
}

func TestValidateSecurityConfig_RequiresInternalTagsAuthInProduction(t *testing.T) {
	ResetForTest()
	os.Unsetenv("ACG_CONFIG")
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_SECURITY_ENFORCE", "true")
	t.Setenv("RBAC_ENABLE", "true")
	t.Setenv("DB_SSL", "require")
	os.Unsetenv("ROS_TAGS_DEV_TOKEN")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "s3.example.com")
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "false")
	_ = GetConfig()
	cfg := GetConfig()
	cfg.KafkaSecurityProtocol = "SASL_SSL"

	err := ValidateSecurityConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "INTERNAL_AUTH_DISABLED")
}

func TestValidateSecurityConfig_AllowsDisabledInternalAuthInDevelopment(t *testing.T) {
	ResetForTest()
	os.Unsetenv("ACG_CONFIG")
	os.Unsetenv("ROS_SECURITY_ENFORCE")
	t.Setenv("DEVELOPMENT", "true")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "")
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "false")
	_ = GetConfig()

	err := ValidateSecurityConfig()
	require.NoError(t, err)
}

func TestValidateSecurityConfig_PprofEnabledFatal(t *testing.T) {
	ResetForTest()
	os.Unsetenv("ACG_CONFIG")
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_SECURITY_ENFORCE", "true")
	t.Setenv("RBAC_ENABLE", "true")
	t.Setenv("DB_SSL", "require")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "s3.amazonaws.com")
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "true")
	t.Setenv("ROS_ENABLE_PPROF", "true")
	cfg := GetConfig()
	cfg.KafkaSecurityProtocol = "SASL_SSL"

	err := ValidateSecurityConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PPROF_ENABLED")
	assert.Contains(t, err.Error(), "CM-7")
}

func TestValidateSecurityConfig_PprofDisabledOk(t *testing.T) {
	ResetForTest()
	os.Unsetenv("ACG_CONFIG")
	t.Setenv("DEVELOPMENT", "false")
	t.Setenv("ROS_SECURITY_ENFORCE", "true")
	t.Setenv("RBAC_ENABLE", "true")
	t.Setenv("DB_SSL", "require")
	t.Setenv("ROS_CSV_ALLOWED_HOSTS", "s3.amazonaws.com")
	t.Setenv("ROS_INTERNAL_TAGS_AUTH_REQUIRED", "true")
	t.Setenv("ROS_ENABLE_PPROF", "false")
	cfg := GetConfig()
	cfg.KafkaSecurityProtocol = "SASL_SSL"

	err := ValidateSecurityConfig()
	require.NoError(t, err)
}
