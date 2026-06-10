package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/health"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockReadyzDB struct {
	pingErr error
}

func (m *mockReadyzDB) Ping(ctx context.Context) error {
	return m.pingErr
}

func TestGetReadyz_PoolNil_Returns503(t *testing.T) {
	db.ReadyzPoolProvider = func() db.ReadyzDB { return nil }
	t.Cleanup(func() { db.ReadyzPoolProvider = nil })

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := GetReadyz(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "pool_uninitialized")
}

func TestGetReadyz_DBPingOK_Returns200(t *testing.T) {
	db.ReadyzPoolProvider = func() db.ReadyzDB {
		return &mockReadyzDB{pingErr: nil}
	}
	t.Cleanup(func() { db.ReadyzPoolProvider = nil })

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := GetReadyz(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"database":"ok"`)
}

func TestGetReadyz_DBPingFails_Returns503(t *testing.T) {
	db.ReadyzPoolProvider = func() db.ReadyzDB {
		return &mockReadyzDB{pingErr: errors.New("connection refused")}
	}
	t.Cleanup(func() { db.ReadyzPoolProvider = nil })

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := GetReadyz(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "unavailable")
}

func TestGetReadyz_KafkaCheckFails_Returns503(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_READINESS_CHECK_KAFKA", "true")
	health.KafkaCheckFn = func(ctx context.Context) error { return errors.New("kafka down") }
	t.Cleanup(func() {
		health.KafkaCheckFn = nil
		db.ReadyzPoolProvider = nil
	})

	db.ReadyzPoolProvider = func() db.ReadyzDB {
		return &mockReadyzDB{pingErr: nil}
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := GetReadyz(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), `"kafka":"unavailable"`)
}
