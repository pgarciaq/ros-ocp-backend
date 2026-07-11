package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func setRequiredDBEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_NAME", "test")
	t.Setenv("DB_USER", "test")
}

func TestDefaultDBMaxConns(t *testing.T) {
	ResetForTest()
	setRequiredDBEnv(t)

	cfg := GetConfig()
	assert.Equal(t, defaultDBMaxConns, cfg.DBMaxConns)
}

func TestDBPoolSizeLegacyAlias(t *testing.T) {
	ResetForTest()
	setRequiredDBEnv(t)
	t.Setenv("DB_POOL_SIZE", "8")

	cfg := GetConfig()
	assert.Equal(t, 8, cfg.DBMaxConns)
}

func TestROSDBMaxConnsOverridesDBPoolSize(t *testing.T) {
	ResetForTest()
	setRequiredDBEnv(t)
	t.Setenv("DB_POOL_SIZE", "8")
	t.Setenv("ROS_DB_MAX_CONNS", "12")

	cfg := GetConfig()
	assert.Equal(t, 12, cfg.DBMaxConns)
}

func TestInvalidDBMaxConnsFallsBackToDefault(t *testing.T) {
	ResetForTest()
	setRequiredDBEnv(t)
	t.Setenv("ROS_DB_MAX_CONNS", "0")

	cfg := GetConfig()
	assert.Equal(t, defaultDBMaxConns, cfg.DBMaxConns)
}

func TestDefaultDBMaxConnsSatisfiesPoolConstraint(t *testing.T) {
	ResetForTest()
	setRequiredDBEnv(t)

	cfg := GetConfig()
	needed := cfg.ManifestDownloadWorkers*cfg.KafkaWorkers + 2
	assert.GreaterOrEqual(t, cfg.DBMaxConns, needed,
		"defaultDBMaxConns (%d) must be >= ManifestDownloadWorkers(%d) × KafkaWorkers(%d) + 2 = %d (ADR-0320)",
		cfg.DBMaxConns, cfg.ManifestDownloadWorkers, cfg.KafkaWorkers, needed)
}
