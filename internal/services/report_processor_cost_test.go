package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

func TestGetCostDataProvider_SavingsEstimatesDisabled(t *testing.T) {
	t.Setenv("KOKU_MASU_URL", "http://masu.example:5042")
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "false")
	config.ResetForTest()

	cfg := config.GetConfig()
	provider := getCostDataProvider(cfg)
	_, ok := provider.(*costdata.NilCostDataProvider)
	require.True(t, ok, "expected NilCostDataProvider when savings estimates disabled")
}

func TestNativeEngineCostFetch_SavingsEstimatesDisabled_SkipsMasu(t *testing.T) {
	var masuCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		masuCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	t.Setenv("KOKU_MASU_URL", srv.URL)
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "false")
	config.ResetForTest()

	appCfg := config.GetConfig()
	require.False(t, appCfg.SavingsEstimatesEnabled)

	var costData *costdata.ClusterCostData
	if appCfg.SavingsEstimatesEnabled {
		costProvider := getCostDataProvider(appCfg)
		var costErr error
		costData, costErr = costProvider.GetEffectiveRates(
			context.Background(), "1234567", "cluster-1", time.Now().UTC().AddDate(0, 0, -7), time.Now().UTC(),
		)
		require.NoError(t, costErr)
	}

	assert.Equal(t, int32(0), masuCalls.Load(), "Masu should not be called when savings estimates are disabled")
	assert.Nil(t, costData)

	recs := []engine.ContainerRec{{Namespace: "app", ContainerName: "web"}}
	engine.ApplySavingsEstimates(recs, costData)
	assert.Nil(t, recs[0].EstimatedSavingsCents)
	assert.Contains(t, recs[0].NotificationCodes, engine.NotifNoCostData)
}

func TestNativeEngineCostFetch_SavingsEstimatesEnabled_CallsMasu(t *testing.T) {
	var masuCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		masuCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cluster_id":"cluster-1","configured_rates":{},"namespace_aggregates":{}}`))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("KOKU_MASU_URL", srv.URL)
	t.Setenv("ROS_SAVINGS_ESTIMATES_ENABLED", "true")
	config.ResetForTest()

	appCfg := config.GetConfig()
	require.True(t, appCfg.SavingsEstimatesEnabled)

	var costData *costdata.ClusterCostData
	if appCfg.SavingsEstimatesEnabled {
		costProvider := getCostDataProvider(appCfg)
		var costErr error
		costData, costErr = costProvider.GetEffectiveRates(
			context.Background(), "1234567", "cluster-1", time.Now().UTC().AddDate(0, 0, -7), time.Now().UTC(),
		)
		require.NoError(t, costErr)
	}

	assert.Equal(t, int32(1), masuCalls.Load())
	require.NotNil(t, costData)
}
