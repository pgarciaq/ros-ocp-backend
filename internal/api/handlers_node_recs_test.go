package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func TestFilterNodeRecs_NoFilters(t *testing.T) {
	recs := []model.NodeGPURecommendation{
		{NodeName: "node-1", GPUModel: "T4"},
		{NodeName: "node-2", GPUModel: "A100"},
	}
	result := filterNodeRecs(recs, "", "", "")
	assert.Len(t, result, 2)
}

func TestFilterNodeRecs_ByNodeName(t *testing.T) {
	recs := []model.NodeGPURecommendation{
		{NodeName: "gpu-worker-1", GPUModel: "T4"},
		{NodeName: "gpu-worker-2", GPUModel: "T4"},
		{NodeName: "cpu-only-node", GPUModel: "A100"},
	}
	result := filterNodeRecs(recs, "gpu-worker-1", "", "")
	assert.Len(t, result, 1)
	assert.Equal(t, "gpu-worker-1", result[0].NodeName)
}

func TestFilterNodeRecs_ByNodeNameCaseInsensitive(t *testing.T) {
	recs := []model.NodeGPURecommendation{
		{NodeName: "GPU-Worker-1", GPUModel: "T4"},
	}
	result := filterNodeRecs(recs, "gpu-worker-1", "", "")
	assert.Len(t, result, 1)
}

func TestFilterNodeRecs_ByGPUModel(t *testing.T) {
	recs := []model.NodeGPURecommendation{
		{NodeName: "node-1", GPUModel: "NVIDIA T4"},
		{NodeName: "node-2", GPUModel: "NVIDIA A100-SXM4-80GB"},
		{NodeName: "node-3", GPUModel: "NVIDIA T4"},
	}
	result := filterNodeRecs(recs, "", "A100", "")
	assert.Len(t, result, 1)
	assert.Equal(t, "node-2", result[0].NodeName)
}

func TestFilterNodeRecs_BothFilters(t *testing.T) {
	recs := []model.NodeGPURecommendation{
		{NodeName: "node-1", GPUModel: "T4"},
		{NodeName: "node-1", GPUModel: "A100"},
		{NodeName: "node-2", GPUModel: "T4"},
	}
	result := filterNodeRecs(recs, "node-1", "T4", "")
	assert.Len(t, result, 1)
	assert.Equal(t, "node-1", result[0].NodeName)
	assert.Equal(t, "T4", result[0].GPUModel)
}

func TestFilterNodeRecs_NoMatch(t *testing.T) {
	recs := []model.NodeGPURecommendation{
		{NodeName: "node-1", GPUModel: "T4"},
	}
	result := filterNodeRecs(recs, "nonexistent", "", "")
	assert.Len(t, result, 0)
}

func TestGroupByNodeAndModel(t *testing.T) {
	rec1 := &engine.GPURec{GPUModelName: "T4", Term: "medium", Classification: engine.GPUClassUnderutilized, SMActiveAvg: 0.1}
	rec2 := &engine.GPURec{GPUModelName: "T4", Term: "medium", Classification: engine.GPUClassUnderutilized, SMActiveAvg: 0.2}
	rec3 := &engine.GPURec{GPUModelName: "A100", Term: "medium", Classification: engine.GPUClassWellUtilized, SMActiveAvg: 0.7}

	gpuRecs := map[string][]*engine.GPURec{
		"ns1/wl1/c1": {rec1},
		"ns1/wl1/c2": {rec2},
		"ns2/wl2/c3": {rec3},
	}
	nodeMap := map[string]string{
		"ns1/wl1/c1": "gpu-node-1",
		"ns1/wl1/c2": "gpu-node-1",
		"ns2/wl2/c3": "gpu-node-1",
	}
	lastSeen := map[string]time.Time{
		"gpu-node-1": time.Now().UTC().AddDate(0, 0, -2),
	}

	groups := groupByNodeAndModel(gpuRecs, nodeMap, lastSeen, "cluster-1")

	assert.Len(t, groups, 2, "should have 2 groups (T4 + A100 on same node)")

	var t4Group, a100Group *engine.NodeGPUGroup
	for i := range groups {
		if groups[i].GPUModel == "T4" {
			t4Group = &groups[i]
		} else if groups[i].GPUModel == "A100" {
			a100Group = &groups[i]
		}
	}
	require.NotNil(t, t4Group, "expected T4 group to be found")
	require.NotNil(t, a100Group, "expected A100 group to be found")
	assert.Len(t, t4Group.Containers, 2)
	assert.Len(t, a100Group.Containers, 1)
	assert.Equal(t, "gpu-node-1", t4Group.NodeName)
	assert.False(t, t4Group.LastSeen.IsZero(), "LastSeen should be populated from nodeLastSeen")
}

func TestGroupByNodeAndModel_SkipsMissingNode(t *testing.T) {
	gpuRecs := map[string][]*engine.GPURec{
		"ns1/wl1/c1": {{GPUModelName: "T4", Term: "medium"}},
	}
	nodeMap := map[string]string{}
	lastSeen := map[string]time.Time{}

	groups := groupByNodeAndModel(gpuRecs, nodeMap, lastSeen, "cluster-1")
	assert.Len(t, groups, 0, "containers with no node mapping should be skipped")
}

// --- Sorting, pagination, and links unit tests ---

func TestSortNodeRecs_ByNodeNameAsc(t *testing.T) {
	recs := []model.NodeGPURecommendation{
		{NodeName: "z-node"}, {NodeName: "a-node"}, {NodeName: "m-node"},
	}
	sortNodeRecs(recs, "node_name", listoptions.OrderAsc)
	assert.Equal(t, "a-node", recs[0].NodeName)
	assert.Equal(t, "m-node", recs[1].NodeName)
	assert.Equal(t, "z-node", recs[2].NodeName)
}

func TestSortNodeRecs_ByNodeNameDesc(t *testing.T) {
	recs := []model.NodeGPURecommendation{
		{NodeName: "a-node"}, {NodeName: "z-node"}, {NodeName: "m-node"},
	}
	sortNodeRecs(recs, "node_name", listoptions.OrderDesc)
	assert.Equal(t, "z-node", recs[0].NodeName)
	assert.Equal(t, "m-node", recs[1].NodeName)
	assert.Equal(t, "a-node", recs[2].NodeName)
}

func TestSortNodeRecs_ByConfidenceDesc(t *testing.T) {
	recs := []model.NodeGPURecommendation{
		{NodeName: "a", Confidence: 0.5},
		{NodeName: "b", Confidence: 0.9},
		{NodeName: "c", Confidence: 0.2},
	}
	sortNodeRecs(recs, "confidence", listoptions.OrderDesc)
	assert.Equal(t, "b", recs[0].NodeName)
	assert.Equal(t, "a", recs[1].NodeName)
	assert.Equal(t, "c", recs[2].NodeName)
}

func TestSortNodeRecs_ByTotalSavingsNilSafe(t *testing.T) {
	s1 := float32(100)
	s2 := float32(300)
	recs := []model.NodeGPURecommendation{
		{NodeName: "a", TotalNodeSavingsUSD: &s1},
		{NodeName: "b", TotalNodeSavingsUSD: nil},
		{NodeName: "c", TotalNodeSavingsUSD: &s2},
	}
	sortNodeRecs(recs, "total_node_savings_usd", listoptions.OrderDesc)
	assert.Equal(t, "c", recs[0].NodeName)
	assert.Equal(t, "a", recs[1].NodeName)
	assert.Equal(t, "b", recs[2].NodeName)
}

func TestSortNodeRecs_ByRecommendedReplicas(t *testing.T) {
	recs := []model.NodeGPURecommendation{
		{NodeName: "a", RecommendedReplicas: 4},
		{NodeName: "b", RecommendedReplicas: 2},
		{NodeName: "c", RecommendedReplicas: 8},
	}
	sortNodeRecs(recs, "recommended_replicas", listoptions.OrderAsc)
	assert.Equal(t, "b", recs[0].NodeName)
	assert.Equal(t, "a", recs[1].NodeName)
	assert.Equal(t, "c", recs[2].NodeName)
}

func TestSortNodeRecs_SingleElement(t *testing.T) {
	recs := []model.NodeGPURecommendation{{NodeName: "only"}}
	sortNodeRecs(recs, "node_name", listoptions.OrderAsc)
	assert.Equal(t, "only", recs[0].NodeName)
}

func TestApplyNodePagination_Basic(t *testing.T) {
	recs := make([]model.NodeGPURecommendation, 25)
	for i := range recs {
		recs[i].NodeName = fmt.Sprintf("node-%02d", i)
	}

	page := applyNodePagination(recs, 0, 10)
	assert.Len(t, page, 10)
	assert.Equal(t, "node-00", page[0].NodeName)
	assert.Equal(t, "node-09", page[9].NodeName)

	page = applyNodePagination(recs, 20, 10)
	assert.Len(t, page, 5)
	assert.Equal(t, "node-20", page[0].NodeName)

	page = applyNodePagination(recs, 25, 10)
	assert.Len(t, page, 0)
}

func TestApplyNodePagination_NoLimit(t *testing.T) {
	recs := make([]model.NodeGPURecommendation, 5)
	page := applyNodePagination(recs, 0, -1)
	assert.Len(t, page, 5)

	page = applyNodePagination(recs, 2, -1)
	assert.Len(t, page, 3)
}

func TestBuildNodeLinks_FirstPage(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", nil)
	links := buildNodeLinks(req, 25, 10, 0)
	assert.NotEmpty(t, links.First)
	assert.NotEmpty(t, links.Next)
	assert.Empty(t, links.Previous)
}

func TestBuildNodeLinks_MiddlePage(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", nil)
	links := buildNodeLinks(req, 50, 10, 20)
	assert.NotEmpty(t, links.First)
	assert.NotEmpty(t, links.Next)
	assert.NotEmpty(t, links.Previous)
}

func TestBuildNodeLinks_LastPage(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", nil)
	links := buildNodeLinks(req, 25, 10, 20)
	assert.NotEmpty(t, links.First)
	assert.Empty(t, links.Next)
}

func TestBuildNodeLinks_UnlimitedNoSpuriousLinks(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing", nil)
	links := buildNodeLinks(req, 50, -1, 0)
	assert.NotEmpty(t, links.First)
	assert.Empty(t, links.Previous, "unlimited should not have previous link")
	assert.Empty(t, links.Next, "unlimited should not have next link")
	assert.Empty(t, links.Last, "unlimited should not have last link")
}

// --- RBAC filtering unit tests ---

func withRBACEnabled(t *testing.T, fn func()) {
	t.Helper()
	cfg := config.GetConfig()
	orig := cfg.RBACEnabled
	cfg.RBACEnabled = true
	defer func() { cfg.RBACEnabled = orig }()
	fn()
}

func TestFilterClustersByRBAC_Disabled(t *testing.T) {
	cfg := config.GetConfig()
	orig := cfg.RBACEnabled
	cfg.RBACEnabled = false
	defer func() { cfg.RBACEnabled = orig }()

	clusters := []string{"c1", "c2"}
	perms := map[string][]string{"openshift.cluster": {"c1"}}
	result := filterClustersByRBAC(clusters, perms)
	assert.Equal(t, clusters, result, "RBAC disabled should return all clusters")
}

func TestFilterClustersByRBAC_GlobalWildcard(t *testing.T) {
	withRBACEnabled(t, func() {
		clusters := []string{"c1", "c2"}
		perms := map[string][]string{"*": {}}
		result := filterClustersByRBAC(clusters, perms)
		assert.Equal(t, clusters, result)
	})
}

func TestFilterClustersByRBAC_ClusterWildcard(t *testing.T) {
	withRBACEnabled(t, func() {
		clusters := []string{"c1", "c2"}
		perms := map[string][]string{"openshift.cluster": {"*"}}
		result := filterClustersByRBAC(clusters, perms)
		assert.Equal(t, clusters, result)
	})
}

func TestFilterClustersByRBAC_SpecificClusters(t *testing.T) {
	withRBACEnabled(t, func() {
		clusters := []string{"c1", "c2", "c3"}
		perms := map[string][]string{"openshift.cluster": {"c1", "c3"}}
		result := filterClustersByRBAC(clusters, perms)
		assert.ElementsMatch(t, []string{"c1", "c3"}, result)
	})
}

func TestFilterClustersByRBAC_NoClusterPerms(t *testing.T) {
	withRBACEnabled(t, func() {
		clusters := []string{"c1", "c2"}
		perms := map[string][]string{"openshift.node": {"node-a"}}
		result := filterClustersByRBAC(clusters, perms)
		assert.Equal(t, clusters, result, "no cluster perms means no cluster filtering")
	})
}

func TestFilterNodeRecsByRBAC_Disabled(t *testing.T) {
	cfg := config.GetConfig()
	orig := cfg.RBACEnabled
	cfg.RBACEnabled = false
	defer func() { cfg.RBACEnabled = orig }()

	recs := []model.NodeGPURecommendation{
		{NodeName: "node-1"}, {NodeName: "node-2"},
	}
	perms := map[string][]string{"openshift.node": {"node-1"}}
	result := filterNodeRecsByRBAC(recs, perms)
	assert.Len(t, result, 2, "RBAC disabled should return all recs")
}

func TestFilterNodeRecsByRBAC_GlobalWildcard(t *testing.T) {
	withRBACEnabled(t, func() {
		recs := []model.NodeGPURecommendation{
			{NodeName: "node-1"}, {NodeName: "node-2"},
		}
		perms := map[string][]string{"*": {}}
		result := filterNodeRecsByRBAC(recs, perms)
		assert.Len(t, result, 2)
	})
}

func TestFilterNodeRecsByRBAC_NodeWildcard(t *testing.T) {
	withRBACEnabled(t, func() {
		recs := []model.NodeGPURecommendation{
			{NodeName: "node-1"}, {NodeName: "node-2"},
		}
		perms := map[string][]string{"openshift.node": {"*"}}
		result := filterNodeRecsByRBAC(recs, perms)
		assert.Len(t, result, 2)
	})
}

func TestFilterNodeRecsByRBAC_SpecificNodes(t *testing.T) {
	withRBACEnabled(t, func() {
		recs := []model.NodeGPURecommendation{
			{NodeName: "node-1"}, {NodeName: "node-2"}, {NodeName: "node-3"},
		}
		perms := map[string][]string{"openshift.node": {"node-1", "node-3"}}
		result := filterNodeRecsByRBAC(recs, perms)
		assert.Len(t, result, 2)
		names := []string{result[0].NodeName, result[1].NodeName}
		assert.ElementsMatch(t, []string{"node-1", "node-3"}, names)
	})
}

func TestFilterNodeRecsByRBAC_NoNodePerms(t *testing.T) {
	withRBACEnabled(t, func() {
		recs := []model.NodeGPURecommendation{
			{NodeName: "node-1"}, {NodeName: "node-2"},
		}
		perms := map[string][]string{"openshift.cluster": {"c1"}}
		result := filterNodeRecsByRBAC(recs, perms)
		assert.Len(t, result, 2, "no node perms means no node filtering")
	})
}

func TestFilterNodeRecsByRBAC_EmptyPerms(t *testing.T) {
	withRBACEnabled(t, func() {
		recs := []model.NodeGPURecommendation{
			{NodeName: "node-1"},
		}
		perms := map[string][]string{}
		result := filterNodeRecsByRBAC(recs, perms)
		assert.Len(t, result, 1, "empty perms with RBAC enabled but no node key = no filtering")
	})
}

func TestFilterClusterAndNodeRBAC_Combined(t *testing.T) {
	withRBACEnabled(t, func() {
		clusters := []string{"c1", "c2", "c3"}
		perms := map[string][]string{
			"openshift.cluster": {"c1", "c2"},
			"openshift.node":    {"gpu-node-1"},
		}
		filteredClusters := filterClustersByRBAC(clusters, perms)
		assert.ElementsMatch(t, []string{"c1", "c2"}, filteredClusters)

		recs := []model.NodeGPURecommendation{
			{NodeName: "gpu-node-1", ClusterUUID: "c1"},
			{NodeName: "gpu-node-2", ClusterUUID: "c1"},
			{NodeName: "gpu-node-1", ClusterUUID: "c2"},
		}
		filteredRecs := filterNodeRecsByRBAC(recs, perms)
		assert.Len(t, filteredRecs, 2)
		for _, r := range filteredRecs {
			assert.Equal(t, "gpu-node-1", r.NodeName)
		}
	})
}
