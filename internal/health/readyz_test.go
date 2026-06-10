package health

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

type mockReadyzDB struct {
	pingErr error
}

func (m *mockReadyzDB) Ping(ctx context.Context) error {
	return m.pingErr
}

func TestRunReadyzChecks_DatabaseOK(t *testing.T) {
	config.ResetForTest()
	result := RunReadyzChecks(context.Background(), &mockReadyzDB{})
	require.True(t, result.OK)
	assert.Equal(t, "ok", result.Checks["database"])
}

func TestRunReadyzChecks_DatabaseUnavailable(t *testing.T) {
	config.ResetForTest()
	result := RunReadyzChecks(context.Background(), &mockReadyzDB{pingErr: errors.New("connection refused")})
	require.False(t, result.OK)
	assert.Equal(t, "unavailable", result.Checks["database"])
}

func TestRunReadyzChecks_PoolNil(t *testing.T) {
	config.ResetForTest()
	result := RunReadyzChecks(context.Background(), nil)
	require.False(t, result.OK)
	assert.Equal(t, "pool_uninitialized", result.Checks["database"])
}

func TestRunReadyzChecks_KafkaEnabled_Failure(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_READINESS_CHECK_KAFKA", "true")
	KafkaCheckFn = func(ctx context.Context) error { return errors.New("broker down") }
	t.Cleanup(func() { KafkaCheckFn = nil })

	result := RunReadyzChecks(context.Background(), &mockReadyzDB{})
	require.False(t, result.OK)
	assert.Equal(t, "ok", result.Checks["database"])
	assert.Equal(t, "unavailable", result.Checks["kafka"])
}

func TestRunReadyzChecks_S3Enabled_Success(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_READINESS_CHECK_S3", "true")
	S3CheckFn = func(ctx context.Context) error { return nil }
	t.Cleanup(func() { S3CheckFn = nil })

	result := RunReadyzChecks(context.Background(), &mockReadyzDB{})
	require.True(t, result.OK)
	assert.Equal(t, "ok", result.Checks["s3"])
}
