package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultIngestStrictAnalytics(t *testing.T) {
	ResetForTest()
	t.Setenv("ROS_INGEST_STRICT_ANALYTICS", "")
	cfg := GetConfig()
	assert.True(t, cfg.IngestStrictAnalytics, "strict analytics should default to true")
}

func TestDefaultRBACCacheMaxEntries(t *testing.T) {
	ResetForTest()
	t.Setenv("ROS_RBAC_CACHE_MAX_ENTRIES", "")
	cfg := GetConfig()
	assert.Equal(t, 500, cfg.RBACCacheMaxEntries)
}
