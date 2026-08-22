package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/vm"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestGetVMRecommendationDetail_BusinessHoursThinNestEmitsCode82(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := "org-vm-bh-thin-" + uuid.New().String()[:8]
	seedVMRecCluster(t, orgID)
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	ctx := context.Background()
	vmName := "office-vm"
	namespace := "prod"

	now := time.Now().UTC()
	for i := 0; i < 8; i++ {
		bucket := now.AddDate(0, 0, -i).Truncate(24 * time.Hour)
		_, err := pool.Exec(ctx, `
			INSERT INTO daily_vm_digests (
				org_id, cluster_uuid, vm_name, namespace, guest_os, bucket_date,
				cpu_usage_p95_mc, cpu_request_mc, mem_usage_p95_kib, mem_request_kib,
				sample_count, schedule_type
			) VALUES
				($1, $2, $3, $4, 'linux', $5, 3500, 4000, 4194304, 8388608, 96, 'all_hours'),
				($1, $2, $3, $4, 'linux', $5, 500, 4000, 1048576, 8388608, 40, 'business_hours')`,
			orgID, clusterID, vmName, namespace, bucket)
		require.NoError(t, err)
	}

	rec := model.VMRecommendation{
		OrgID: orgID, ClusterUUID: clusterID,
		VMName: vmName, Namespace: namespace, GuestOS: "linux",
		CurrentVCPU: 4, CurrentMemoryGiB: 8,
		RecommendedVCPU: 4, RecommendedMemoryGiB: 8,
		Confidence: "high", Term: "short_term", Engine: "cost",
		Notifications:     []byte(`[]`),
		LastRecommendedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, vm.PersistVMRecommendations(ctx, pool, []model.VMRecommendation{rec}, nil))

	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: testutil.TestClusterUUID, Namespace: "",
		Timezone: "UTC", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "09:00", EndTime: "17:00", Enabled: true,
	}))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/vm", api.GetVMRecommendations)
	v1.GET("/recommendations/openshift/vm/detail", api.GetVMRecommendationDetail)

	detailURL := "/api/cost-management/v1/recommendations/openshift/vm/detail" +
		"?cluster_uuid=" + testutil.TestClusterUUID +
		"&vm_name=" + vmName + "&namespace=" + namespace +
		"&term=short_term&engine=cost"
	req := httptest.NewRequest(http.MethodGet, detailURL, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	recw := httptest.NewRecorder()
	app.ServeHTTP(recw, req)
	require.Equal(t, http.StatusOK, recw.Code, recw.Body.String())

	var item api.VMRecommendationItem
	require.NoError(t, json.Unmarshal(recw.Body.Bytes(), &item))
	require.NotNil(t, item.BusinessHours)
	require.NotNil(t, item.BusinessHours.RecommendedVCPU)
	assert.Less(t, *item.BusinessHours.RecommendedVCPU, item.Recommended.VCPU)
	require.Contains(t, item.BusinessHours.Notifications, "82")
	assert.Equal(t, int16(82), item.BusinessHours.Notifications["82"].Code)
	assert.Empty(t, item.BusinessHours.Reason)
	bhJSON, err := json.Marshal(item.BusinessHours)
	require.NoError(t, err)
	assert.NotContains(t, string(bhJSON), "estimated")
	assert.NotContains(t, string(bhJSON), "instance_type")
	assert.NotContains(t, string(bhJSON), "gpu")
	assert.NotContains(t, recw.Body.String(), `"code":64`)

	listReq := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vm?filter%5Bvm_name%5D="+vmName, nil)
	listReq.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	listRec := httptest.NewRecorder()
	app.ServeHTTP(listRec, listReq)
	require.Equal(t, http.StatusOK, listRec.Code, listRec.Body.String())
	assert.NotContains(t, listRec.Body.String(), `"business_hours"`)
	assert.NotContains(t, listRec.Body.String(), "VM_BH_OFFICE_WINDOW")
}

func TestGetVMRecommendationDetail_InsufficientBHDaysReasonOnly(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := "org-vm-bh-reason-" + uuid.New().String()[:8]
	seedVMRecCluster(t, orgID)
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	ctx := context.Background()
	vmName := "sparse-vm"
	namespace := "prod"
	now := time.Now().UTC()
	bucket := now.Truncate(24 * time.Hour)
	_, err := pool.Exec(ctx, `
		INSERT INTO daily_vm_digests (
			org_id, cluster_uuid, vm_name, namespace, guest_os, bucket_date,
			cpu_usage_p95_mc, cpu_request_mc, mem_usage_p95_kib, mem_request_kib,
			sample_count, schedule_type
		) VALUES
			($1, $2, $3, $4, 'linux', $5, 3500, 4000, 4194304, 8388608, 96, 'all_hours'),
			($1, $2, $3, $4, 'linux', $5, 500, 4000, 1048576, 8388608, 40, 'business_hours')`,
		orgID, clusterID, vmName, namespace, bucket)
	require.NoError(t, err)

	rec := model.VMRecommendation{
		OrgID: orgID, ClusterUUID: clusterID,
		VMName: vmName, Namespace: namespace, GuestOS: "linux",
		CurrentVCPU: 4, CurrentMemoryGiB: 8,
		RecommendedVCPU: 4, RecommendedMemoryGiB: 8,
		Confidence: "low", Term: "short_term", Engine: "cost",
		Notifications:     []byte(`[]`),
		LastRecommendedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, vm.PersistVMRecommendations(ctx, pool, []model.VMRecommendation{rec}, nil))
	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: testutil.TestClusterUUID, Namespace: "",
		Timezone: "UTC", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "09:00", EndTime: "17:00", Enabled: true,
	}))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/vm/detail", api.GetVMRecommendationDetail)
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vm/detail"+
			"?cluster_uuid="+testutil.TestClusterUUID+
			"&vm_name="+vmName+"&namespace="+namespace+
			"&term=short_term&engine=cost", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	recw := httptest.NewRecorder()
	app.ServeHTTP(recw, req)
	require.Equal(t, http.StatusOK, recw.Code, recw.Body.String())

	var item api.VMRecommendationItem
	require.NoError(t, json.Unmarshal(recw.Body.Bytes(), &item))
	require.NotNil(t, item.BusinessHours)
	assert.Contains(t, item.BusinessHours.Reason, "insufficient business hours data")
	assert.Nil(t, item.BusinessHours.RecommendedVCPU)
	assert.NotContains(t, item.BusinessHours.Notifications, "82")
}

func TestGetVMRecommendationDetail_DisabledScheduleOmitsBusinessHours(t *testing.T) {
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	config.ResetForTest()
	t.Cleanup(config.ResetForTest)
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := "org-vm-bh-off-" + uuid.New().String()[:8]
	seedVMRecCluster(t, orgID)
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	ctx := context.Background()
	vmName := "off-vm"
	namespace := "prod"
	now := time.Now().UTC()
	for i := 0; i < 8; i++ {
		bucket := now.AddDate(0, 0, -i).Truncate(24 * time.Hour)
		_, err := pool.Exec(ctx, `
			INSERT INTO daily_vm_digests (
				org_id, cluster_uuid, vm_name, namespace, guest_os, bucket_date,
				cpu_usage_p95_mc, cpu_request_mc, mem_usage_p95_kib, mem_request_kib,
				sample_count, schedule_type
			) VALUES
				($1, $2, $3, $4, 'linux', $5, 3500, 4000, 4194304, 8388608, 96, 'all_hours')`,
			orgID, clusterID, vmName, namespace, bucket)
		require.NoError(t, err)
	}
	rec := model.VMRecommendation{
		OrgID: orgID, ClusterUUID: clusterID,
		VMName: vmName, Namespace: namespace, GuestOS: "linux",
		CurrentVCPU: 4, CurrentMemoryGiB: 8,
		RecommendedVCPU: 4, RecommendedMemoryGiB: 8,
		Confidence: "high", Term: "short_term", Engine: "cost",
		Notifications:     []byte(`[]`),
		LastRecommendedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, vm.PersistVMRecommendations(ctx, pool, []model.VMRecommendation{rec}, nil))
	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: testutil.TestClusterUUID, Namespace: "",
		Timezone: "UTC", Days: []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		StartTime: "09:00", EndTime: "17:00", Enabled: false,
	}))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
	})

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/vm/detail", api.GetVMRecommendationDetail)
	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vm/detail"+
			"?cluster_uuid="+testutil.TestClusterUUID+
			"&vm_name="+vmName+"&namespace="+namespace+
			"&term=short_term&engine=cost", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	recw := httptest.NewRecorder()
	app.ServeHTTP(recw, req)
	require.Equal(t, http.StatusOK, recw.Code, recw.Body.String())

	var item api.VMRecommendationItem
	require.NoError(t, json.Unmarshal(recw.Body.Bytes(), &item))
	assert.Nil(t, item.BusinessHours)
	assert.NotContains(t, recw.Body.String(), `"business_hours"`)
}
