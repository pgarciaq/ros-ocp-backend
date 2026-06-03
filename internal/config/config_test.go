package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestEnvironmentVariableConfiguration(t *testing.T) {
	tests := []struct {
		name         string
		envKey       string
		envValue     string
		viperKey     string
		defaultValue string
		expected     string
		setEnv       bool
	}{
		{
			name:         "DB_HOST environment variable overrides default",
			envKey:       "DB_HOST",
			envValue:     "custom-db-host",
			viperKey:     "DBHost",
			defaultValue: "localhost",
			expected:     "custom-db-host",
			setEnv:       true,
		},
		{
			name:         "DB_PORT uses default when environment variable not set",
			envKey:       "DB_PORT",
			envValue:     "",
			viperKey:     "DBPort",
			defaultValue: "15432",
			expected:     "15432",
			setEnv:       false,
		},
		{
			name:         "KAFKA_BOOTSTRAP_SERVERS environment variable overrides default",
			envKey:       "KAFKA_BOOTSTRAP_SERVERS",
			envValue:     "kafka:9092",
			viperKey:     "KAFKA_BOOTSTRAP_SERVERS",
			defaultValue: "localhost:29092",
			expected:     "kafka:9092",
			setEnv:       true,
		},
		{
			name:         "DB_CA_CERT environment variable overrides default",
			envKey:       "DB_CA_CERT",
			envValue:     "test-ca-cert",
			viperKey:     "DBCACert",
			defaultValue: "",
			expected:     "test-ca-cert",
			setEnv:       true,
		},
		{
			name:         "SOURCES_API_BASE_URL uses default when not set",
			envKey:       "SOURCES_API_BASE_URL",
			envValue:     "",
			viperKey:     "SOURCES_API_BASE_URL",
			defaultValue: "http://127.0.0.1:8002",
			expected:     "http://127.0.0.1:8002",
			setEnv:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()

			if tt.setEnv {
				t.Setenv(tt.envKey, tt.envValue)
			}

			viper.AutomaticEnv()

			if tt.envKey != tt.viperKey {
				_ = viper.BindEnv(tt.viperKey, tt.envKey)
			}

			viper.SetDefault(tt.viperKey, tt.defaultValue)

			result := viper.GetString(tt.viperKey)

			if result != tt.expected {
				t.Errorf("viper.GetString(%q) = %q, want %q (env %s=%q)",
					tt.viperKey, result, tt.expected, tt.envKey, tt.envValue)
			}
		})
	}
}

// TestNonClowderConfigurationLoads verifies that configuration loads correctly
// when CLOWDER_ENABLED is false and environment variables are used.
func TestNonClowderConfigurationLoads(t *testing.T) {
	t.Setenv("CLOWDER_ENABLED", "false")
	t.Setenv("DB_HOST", "test-postgres")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("KAFKA_BOOTSTRAP_SERVERS", "test-kafka:9092")
	t.Setenv("DB_CA_CERT", "test-ca-cert")

	viper.Reset()
	cfg = nil

	config := GetConfig()

	if config == nil {
		t.Fatal("GetConfig() returned nil")
		return
	}

	if config.DBHost != "test-postgres" {
		t.Errorf("DBHost = %q, want %q", config.DBHost, "test-postgres")
	}

	if config.DBPort != "5432" {
		t.Errorf("DBPort = %q, want %q", config.DBPort, "5432")
	}

	if config.KafkaBootstrapServers != "test-kafka:9092" {
		t.Errorf("KafkaBootstrapServers = %q, want %q", config.KafkaBootstrapServers, "test-kafka:9092")
	}

	if config.DBCACert != "test-ca-cert" {
		t.Errorf("DBCACert = %q, want %q", config.DBCACert, "test-ca-cert")
	}
}

func TestLegacyStaleArchiveDaysEnv(t *testing.T) {
	t.Setenv("CLOWDER_ENABLED", "false")
	t.Setenv("ROS_STALE_CLEANUP_DAYS", "")
	t.Setenv("ROS_STALE_ARCHIVE_DAYS", "45")
	t.Setenv("DB_HOST", "test-postgres")
	t.Setenv("DB_PORT", "5432")

	viper.Reset()
	cfg = nil

	config := GetConfig()
	if config == nil {
		t.Fatal("GetConfig() returned nil")
	}
	if config.StaleCleanupDays != 45 {
		t.Errorf("StaleCleanupDays = %d, want 45 (from deprecated ROS_STALE_ARCHIVE_DAYS)", config.StaleCleanupDays)
	}
}

func TestStaleCleanupDaysEnvTakesPrecedenceOverLegacy(t *testing.T) {
	t.Setenv("CLOWDER_ENABLED", "false")
	t.Setenv("ROS_STALE_CLEANUP_DAYS", "21")
	t.Setenv("ROS_STALE_ARCHIVE_DAYS", "45")
	t.Setenv("DB_HOST", "test-postgres")
	t.Setenv("DB_PORT", "5432")

	viper.Reset()
	cfg = nil

	config := GetConfig()
	if config == nil {
		t.Fatal("GetConfig() returned nil")
	}
	if config.StaleCleanupDays != 21 {
		t.Errorf("StaleCleanupDays = %d, want 21 (ROS_STALE_CLEANUP_DAYS overrides legacy)", config.StaleCleanupDays)
	}
}
