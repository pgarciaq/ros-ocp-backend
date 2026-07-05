package health

import (
	"context"
	"errors"
	"net"
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

// --- validateS3Endpoint tests ---

func TestValidateS3Endpoint_HTTPSAllowedInProduction(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "false")
	// google.com resolves to public IPs — should pass
	err := validateS3Endpoint("https://s3.amazonaws.com")
	assert.NoError(t, err)
}

func TestValidateS3Endpoint_HTTPBlockedInProduction(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "false")
	err := validateS3Endpoint("http://s3.amazonaws.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http:// is not allowed in production")
}

func TestValidateS3Endpoint_HTTPAllowedInDevelopment(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "true")
	// In dev mode, http is allowed for local MinIO/LocalStack
	err := validateS3Endpoint("http://minio.example.com")
	// May fail on DNS, but should NOT fail on scheme
	if err != nil {
		assert.NotContains(t, err.Error(), "http:// is not allowed")
	}
}

func TestValidateS3Endpoint_InvalidScheme(t *testing.T) {
	config.ResetForTest()
	err := validateS3Endpoint("ftp://bucket.example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheme must be http or https")
}

func TestValidateS3Endpoint_EmptyHost(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "true")
	err := validateS3Endpoint("http://")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "URL must include a host")
}

func TestValidateS3Endpoint_LoopbackIP(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "true")
	cases := []string{
		"http://127.0.0.1:9000",
		"http://127.0.0.2:9000",
		"http://[::1]:9000",
	}
	for _, endpoint := range cases {
		err := validateS3Endpoint(endpoint)
		require.Error(t, err, "expected error for %s", endpoint)
		assert.Contains(t, err.Error(), "restricted address")
	}
}

func TestValidateS3Endpoint_PrivateRFC1918(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "true")
	cases := []string{
		"http://10.0.0.1:9000",
		"http://10.255.255.255:9000",
		"http://172.16.0.1:9000",
		"http://172.31.255.255:9000",
		"http://192.168.0.1:9000",
		"http://192.168.255.255:9000",
	}
	for _, endpoint := range cases {
		err := validateS3Endpoint(endpoint)
		require.Error(t, err, "expected error for %s", endpoint)
		assert.Contains(t, err.Error(), "restricted address")
	}
}

func TestValidateS3Endpoint_IPv6Private(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "true")
	cases := []string{
		"http://[fd12:3456:789a::1]:9000",
		"http://[fe80::1]:9000",
	}
	for _, endpoint := range cases {
		err := validateS3Endpoint(endpoint)
		require.Error(t, err, "expected error for %s", endpoint)
		assert.Contains(t, err.Error(), "restricted address")
	}
}

func TestValidateS3Endpoint_MetadataIP(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "true")
	// 169.254.169.254 is link-local
	err := validateS3Endpoint("http://169.254.169.254")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restricted address")
}

func TestValidateS3Endpoint_PublicIPAllowed(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "true")
	// 8.8.8.8 is a public IP
	err := validateS3Endpoint("http://8.8.8.8:9000")
	assert.NoError(t, err)
}

func TestIsRestrictedS3IP(t *testing.T) {
	cases := []struct {
		ip       string
		expected bool
	}{
		{"127.0.0.1", true},
		{"127.0.0.2", true},
		{"::1", true},
		{"10.0.0.1", true},
		{"10.255.0.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"172.15.255.255", false},
		{"172.32.0.0", false},
		{"192.168.0.1", true},
		{"192.168.255.255", true},
		{"169.254.169.254", true},
		{"fe80::1", true},
		{"fd00::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"2001:4860:4860::8888", false},
	}
	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		require.NotNil(t, ip, "failed to parse %s", tc.ip)
		assert.Equal(t, tc.expected, isRestrictedS3IP(ip), "IP: %s", tc.ip)
	}
}

func TestValidateS3Endpoint_DNSRebinding_Localhost(t *testing.T) {
	config.ResetForTest()
	t.Setenv("DEVELOPMENT", "true")
	// "localhost" as hostname resolves to 127.0.0.1 — should be caught by DNS resolution
	err := validateS3Endpoint("http://localhost:9000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restricted address")
}
