package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/vm"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func seedVMRecCluster(t *testing.T, orgID string) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'vm-test', 1, NOW()) ON CONFLICT DO NOTHING`,
		testutil.TestClusterUUID,
	)
	require.NoError(t, err)
}

func TestVMRecommendations_ListFilterAbandoned(t *testing.T) {
	orgID := "org-vm-abandoned-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	_ = config.GetConfig()

	seedVMRecCluster(t, orgID)

	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	now := time.Now().UTC()
	abandoned := model.VMRecommendation{
		OrgID:                orgID,
		ClusterUUID:          clusterID,
		VMName:               "abandoned-vm",
		Namespace:            "test-ns",
		GuestOS:              "linux",
		CurrentVCPU:          4,
		CurrentMemoryGiB:     8,
		RecommendedVCPU:      0,
		RecommendedMemoryGiB: 0,
		Confidence:           "high",
		Term:                 "short_term",
		Engine:               "cost",
		IsIdle:               false,
		IsAbandoned:          true,
		IsOversized:          true,
		Notifications:        []byte(`[{"code":43,"type":"critical","message":"abandoned"}]`),
		LastRecommendedAt:    now,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	active := abandoned
	active.VMName = "active-vm"
	active.IsAbandoned = false
	active.IsIdle = true
	active.RecommendedVCPU = 1
	active.RecommendedMemoryGiB = 1
	active.Notifications = []byte(`[{"code":18,"type":"warning","message":"idle"}]`)

	require.NoError(t, vm.PersistVMRecommendations(context.Background(), pool, []model.VMRecommendation{abandoned, active}, nil))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/vm", api.GetVMRecommendations)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vm?filter[is_abandoned]=true&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
		Data []struct {
			VMName   string `json:"vm_name"`
			Metadata struct {
				IsAbandoned bool `json:"is_abandoned"`
			} `json:"metadata"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "abandoned-vm", resp.Data[0].VMName)
	assert.True(t, resp.Data[0].Metadata.IsAbandoned)
}

func TestVMRecommendations_ListFilterEngine(t *testing.T) {
	orgID := "org-vm-engine-filter-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	_ = config.GetConfig()

	seedVMRecCluster(t, orgID)

	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	now := time.Now().UTC()
	costVM := model.VMRecommendation{
		OrgID:                orgID,
		ClusterUUID:          clusterID,
		VMName:               "engine-cost-vm",
		Namespace:            "vm-ns",
		GuestOS:              "linux",
		CurrentVCPU:          4,
		CurrentMemoryGiB:     8,
		RecommendedVCPU:      2,
		RecommendedMemoryGiB: 4,
		Confidence:           "high",
		Term:                 "medium_term",
		Engine:               "cost",
		LastRecommendedAt:    now,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	perfVM := costVM
	perfVM.VMName = "engine-perf-vm"
	perfVM.Engine = "performance"
	perfVM.RecommendedVCPU = 3
	perfVM.RecommendedMemoryGiB = 6

	require.NoError(t, vm.PersistVMRecommendations(context.Background(), pool, []model.VMRecommendation{costVM, perfVM}, nil))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/vm", api.GetVMRecommendations)

	for _, wantEngine := range []string{"cost", "performance"} {
		req := httptest.NewRequest(
			http.MethodGet,
			"/api/cost-management/v1/recommendations/openshift/vm?filter%5Bengine%5D="+wantEngine+"&limit=20",
			nil,
		)
		req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "engine=%s body: %s", wantEngine, rec.Body.String())

		var resp struct {
			Data []struct {
				VMName   string `json:"vm_name"`
				Metadata struct {
					Engine string `json:"engine"`
				} `json:"metadata"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.NotEmpty(t, resp.Data)
		for _, row := range resp.Data {
			assert.Equal(t, wantEngine, row.Metadata.Engine)
		}
	}
}

func TestVMList_Filter_IsNetworkBound(t *testing.T) {
	orgID := "org-vm-network-bound-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	_ = config.GetConfig()

	seedVMRecCluster(t, orgID)

	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	now := time.Now().UTC()
	networkBound := model.VMRecommendation{
		OrgID:                orgID,
		ClusterUUID:          clusterID,
		VMName:               "network-heavy-vm",
		Namespace:            "edge",
		GuestOS:              "linux",
		CurrentVCPU:          4,
		CurrentMemoryGiB:     8,
		RecommendedVCPU:      4,
		RecommendedMemoryGiB: 8,
		Confidence:           "high",
		Term:                 "medium_term",
		Engine:               "cost",
		IsNetworkBound:       true,
		Notifications:        []byte(`[{"code":55,"type":"warning","message":"network"}]`),
		LastRecommendedAt:    now,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	computeBound := networkBound
	computeBound.VMName = "compute-vm"
	computeBound.IsNetworkBound = false
	require.NoError(t, vm.PersistVMRecommendations(context.Background(), pool, []model.VMRecommendation{networkBound, computeBound}, nil))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/vm", api.GetVMRecommendations)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vm?filter[is_network_bound]=true&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
		Data []struct {
			VMName   string `json:"vm_name"`
			Metadata struct {
				IsNetworkBound bool `json:"is_network_bound"`
			} `json:"metadata"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "network-heavy-vm", resp.Data[0].VMName)
	assert.True(t, resp.Data[0].Metadata.IsNetworkBound)
}

func TestVMList_Filter_GuestOS(t *testing.T) {
	orgID := "org-vm-guest-os-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	_ = config.GetConfig()

	seedVMRecCluster(t, orgID)

	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	now := time.Now().UTC()
	windowsVM := model.VMRecommendation{
		OrgID: orgID, ClusterUUID: clusterID,
		VMName: "win-vm", Namespace: "apps",
		GuestOS:     "Microsoft Windows Server 2022",
		CurrentVCPU: 2, CurrentMemoryGiB: 4,
		RecommendedVCPU: 2, RecommendedMemoryGiB: 4,
		Confidence: "moderate", Term: "medium_term", Engine: "cost",
		Notifications:     []byte(`[]`),
		LastRecommendedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	linuxVM := windowsVM
	linuxVM.VMName = "linux-vm"
	linuxVM.GuestOS = "linux"
	require.NoError(t, vm.PersistVMRecommendations(context.Background(), pool, []model.VMRecommendation{windowsVM, linuxVM}, nil))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/vm", api.GetVMRecommendations)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vm?filter[guest_os]=windows&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
		Data []struct {
			VMName  string `json:"vm_name"`
			GuestOS string `json:"guest_os"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "win-vm", resp.Data[0].VMName)
	assert.Contains(t, strings.ToLower(resp.Data[0].GuestOS), "windows")
}

func seedVMGPUListFixtures(t *testing.T, orgID string) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	seedVMRecCluster(t, orgID)
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	now := time.Now().UTC()

	gpuRec := model.VMRecommendation{
		OrgID: orgID, ClusterUUID: clusterID,
		VMName: "gpu-idle-vm", Namespace: "inference",
		GuestOS: "linux", CurrentVCPU: 8, CurrentMemoryGiB: 32,
		RecommendedVCPU: 8, RecommendedMemoryGiB: 32,
		Confidence: "moderate", Term: "medium_term", Engine: "cost",
		GPUCount: 1, GPUModel: "A100", GPUClassification: "idle",
		Notifications:     []byte(`[]`),
		LastRecommendedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	noGPU := gpuRec
	noGPU.VMName = "cpu-only-vm"
	noGPU.Namespace = "batch"
	noGPU.GPUCount = 0
	noGPU.GPUClassification = ""
	require.NoError(t, vm.PersistVMRecommendations(ctx, pool, []model.VMRecommendation{gpuRec, noGPU}, nil))
}

func TestVMList_Filter_HasGPU(t *testing.T) {
	orgID := "org-vm-has-gpu-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	_ = config.GetConfig()

	seedVMGPUListFixtures(t, orgID)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/vm", api.GetVMRecommendations)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vm?filter[has_gpu]=true&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
		Data []struct {
			VMName string `json:"vm_name"`
			GPU    any    `json:"gpu"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "gpu-idle-vm", resp.Data[0].VMName)
	assert.NotNil(t, resp.Data[0].GPU)
}

func TestVMList_Filter_GPUClassification(t *testing.T) {
	orgID := "org-vm-gpu-class-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	_ = config.GetConfig()

	seedVMGPUListFixtures(t, orgID)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/vm", api.GetVMRecommendations)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vm?filter[gpu_classification]=idle&limit=20",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
		Data []struct {
			VMName string `json:"vm_name"`
			GPU    struct {
				GPUClassification string `json:"gpu_classification"`
			} `json:"gpu"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "gpu-idle-vm", resp.Data[0].VMName)
	assert.Equal(t, "idle", resp.Data[0].GPU.GPUClassification)
}

func TestVMDetail_Success_WithGPUDevices(t *testing.T) {
	orgID := "org-vm-detail-gpu-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	_ = config.GetConfig()

	seedVMRecCluster(t, orgID)
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	ctx := context.Background()
	bucket := time.Now().UTC().Truncate(24 * time.Hour)

	var digestID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO daily_vm_digests (
			org_id, cluster_uuid, vm_name, namespace, bucket_date,
			sample_count, gpu_count, gpu_model, has_gpu
		) VALUES ($1, $2, 'detail-gpu-vm', 'ml', $3, 4, 2, 'A100', true)
		RETURNING id`,
		orgID, clusterID, bucket,
	).Scan(&digestID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO vm_gpu_device_digests (vm_digest_id, gpu_uuid, gpu_model, util_avg_bp, util_max_bp)
		VALUES ($1, 'GPU-ONE', 'A100', 500, 800), ($1, 'GPU-TWO', 'A100', 1500, 2000)`,
		digestID,
	)
	require.NoError(t, err)

	now := time.Now().UTC()
	vmRec := model.VMRecommendation{
		OrgID: orgID, ClusterUUID: clusterID,
		VMName: "detail-gpu-vm", Namespace: "ml",
		GuestOS: "linux", CurrentVCPU: 4, CurrentMemoryGiB: 16,
		RecommendedVCPU: 4, RecommendedMemoryGiB: 16,
		Confidence: "moderate", Term: "medium_term", Engine: "cost",
		GPUCount: 2, GPUModel: "A100", GPUClassification: "well_utilized",
		Notifications:     []byte(`[]`),
		LastRecommendedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, vm.PersistVMRecommendations(ctx, pool, []model.VMRecommendation{vmRec}, nil))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/vm/detail", api.GetVMRecommendationDetail)

	url := "/api/cost-management/v1/recommendations/openshift/vm/detail" +
		"?cluster_uuid=" + testutil.TestClusterUUID +
		"&vm_name=detail-gpu-vm&namespace=ml&term=medium_term&engine=cost"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		ID  string `json:"id"`
		GPU struct {
			GPUDevices []struct {
				UUID string `json:"uuid"`
			} `json:"gpu_devices"`
		} `json:"gpu"`
		DailyDigests []struct {
			GPUDevices []struct {
				UUID string `json:"uuid"`
			} `json:"gpu_devices"`
		} `json:"daily_digests"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, model.NativeVMID(testutil.TestClusterUUID, "ml", "detail-gpu-vm"), body.ID)
	require.NotNil(t, body.GPU.GPUDevices)
	assert.Len(t, body.GPU.GPUDevices, 2)
	uuids := []string{body.GPU.GPUDevices[0].UUID, body.GPU.GPUDevices[1].UUID}
	assert.ElementsMatch(t, []string{"GPU-ONE", "GPU-TWO"}, uuids)
	require.NotEmpty(t, body.DailyDigests)
	assert.Len(t, body.DailyDigests[0].GPUDevices, 2)
}
