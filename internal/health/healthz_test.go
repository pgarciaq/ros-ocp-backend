package health

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

func TestRunHealthzChecks_NormalConditions_ReturnsOK(t *testing.T) {
	config.ResetForTest()
	result := RunHealthzChecks(context.Background())
	require.True(t, result.OK)
	assert.Equal(t, "ok", result.Checks["goroutines_status"])
	assert.Equal(t, "ok", result.Checks["gc_status"])
	assert.Equal(t, "ok", result.Checks["scheduler"])
	assert.NotEmpty(t, result.Checks["goroutines"])
	assert.Contains(t, result.Checks, "heap_alloc_mb")
	assert.Contains(t, result.Checks, "heap_sys_mb")
	assert.Contains(t, result.Checks, "gc_cycles")
}

func TestRunHealthzChecks_GoroutineThresholdExceeded_ReturnsNotOK(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_HEALTHZ_MAX_GOROUTINES", "1")

	result := RunHealthzChecks(context.Background())
	require.False(t, result.OK)
	assert.Contains(t, result.Checks["goroutines_status"], "warning: count exceeds threshold 1")
}

func TestRunHealthzChecks_DeadlockCanarySucceeds(t *testing.T) {
	config.ResetForTest()
	result := RunHealthzChecks(context.Background())
	assert.Equal(t, "ok", result.Checks["scheduler"])
}

func TestRunHealthzChecks_ContextCancelled_ReturnsNotOK(t *testing.T) {
	config.ResetForTest()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := RunHealthzChecks(ctx)
	require.False(t, result.OK)
	assert.Contains(t, result.Checks["scheduler"], "warning: scheduler unresponsive")
}
