package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
	"github.com/redhatinsights/ros-ocp-backend/librobne/namespace"
	"github.com/redhatinsights/ros-ocp-backend/librobne/node"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pvc"
	"github.com/redhatinsights/ros-ocp-backend/librobne/quota"
	"github.com/redhatinsights/ros-ocp-backend/librobne/snapshot"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/redhatinsights/ros-ocp-backend/librobne/vm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleNSRec() namespace.NamespaceRec {
	return namespace.NamespaceRec{
		OrgID:                "org-hidden",
		ClusterUUID:          "should-not-appear-on-row",
		Namespace:            "kube-system",
		Term:                 "short",
		Engine:               "cost",
		RecCPURequestMC:      50,
		RecCPULimitMC:        100,
		RecMemRequestKiB:     1024,
		RecMemLimitKiB:       2048,
		CurrentCPURequestMC:  200,
		CurrentMemRequestKiB: 4096,
		Category:             "underused",
	}
}

func sampleRec(savings *int64) types.ContainerRec {
	return types.ContainerRec{
		OrgID:                 "org-hidden",
		ClusterUUID:           "should-not-appear-on-row",
		Namespace:             "app",
		Workload:              "api",
		WorkloadType:          "deployment",
		ContainerName:         "api",
		Term:                  "short",
		Engine:                "cost",
		RecCPURequestMC:       50,
		RecCPULimitMC:         100,
		RecMemRequestKiB:      1024,
		RecMemLimitKiB:        2048,
		CurrentCPURequestMC:   200,
		CurrentCPULimitMC:     400,
		CurrentMemRequestKiB:  4096,
		CurrentMemLimitKiB:    8192,
		ConfidenceLevel:       0.9,
		EstimatedSavingsCents: savings,
		IdleState:             types.IdleState("active"),
		Stale:                 false,
		Category:              "underused",
		Expl:                  types.ContainerExplanationFactors{CPUCostPctMC: 999},
	}
}

func TestWriteRecs_JSONEnvelopeSnakeCase(t *testing.T) {
	var buf bytes.Buffer
	err := writeRecs(&buf, recommendResult{
		Recs:        []types.ContainerRec{sampleRec(nil)},
		ClusterID:   "cluster-a",
		Now:         time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC),
		SkippedRows: 3,
	}, "json")
	require.NoError(t, err)

	raw := buf.String()
	assert.NotContains(t, raw, "RecCPURequestMC", "JSON must not dump PascalCase ContainerRec fields")
	assert.NotContains(t, raw, "CPUCostPctMC", "JSON must not include explanation factors")
	assert.NotContains(t, raw, "org-hidden")
	assert.NotContains(t, raw, "should-not-appear-on-row")

	var env recommendJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	assert.Equal(t, 1, env.Version)
	assert.Equal(t, "cluster-a", env.ClusterID)
	assert.Equal(t, "2026-08-01T02:00:00Z", env.Now)
	assert.Equal(t, 3, env.SkippedRows)
	require.Len(t, env.Recommendations, 1)

	row := env.Recommendations[0]
	assert.Equal(t, "app", row.Namespace)
	assert.Equal(t, "api", row.Workload)
	assert.Equal(t, "deployment", row.WorkloadType)
	assert.Equal(t, "api", row.ContainerName)
	assert.Equal(t, "short", row.Term)
	assert.Equal(t, "cost", row.Engine)
	assert.Equal(t, int64(50), row.RecCPURequestMC)
	assert.Equal(t, int64(100), row.RecCPULimitMC)
	assert.Equal(t, int64(1024), row.RecMemRequestKiB)
	assert.Equal(t, int64(2048), row.RecMemLimitKiB)
	assert.Equal(t, int64(200), row.CurrentCPURequestMC)
	assert.Equal(t, int64(4096), row.CurrentMemRequestKiB)
	assert.Nil(t, row.EstimatedSavingsCents)
	assert.Equal(t, "active", row.IdleState)
	assert.False(t, row.Stale)
	assert.Equal(t, "underused", row.Category)
}

func TestWriteRecs_JSONSavingsNull(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		Recs:      []types.ContainerRec{sampleRec(nil)},
		ClusterID: "c",
		Now:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}, "json"))

	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"estimated_savings_cents":null`)
	assert.NotContains(t, compact, `"estimated_savings_cents":0`)
}

func TestWriteRecs_JSONSavingsNumber(t *testing.T) {
	cents := int64(42)
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		Recs:      []types.ContainerRec{sampleRec(&cents)},
		ClusterID: "c",
		Now:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}, "json"))

	var env recommendJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	require.NotNil(t, env.Recommendations[0].EstimatedSavingsCents)
	assert.Equal(t, int64(42), *env.Recommendations[0].EstimatedSavingsCents)
}

func TestWriteRecs_JSONEmptyRecommendationsArray(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		ClusterID: "c",
		Now:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"recommendations":[]`)
	assert.NotContains(t, compact, `"recommendations":null`)
	assert.NotContains(t, compact, `"namespace_recommendations"`)
}

func TestWriteRecs_JSONNamespaceSibling(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		NamespaceRecs: []namespace.NamespaceRec{sampleNSRec()},
		ClusterID:     "cluster-a",
		Now:           time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC),
		plugins:       []string{"namespace"},
	}, "json"))

	raw := buf.String()
	assert.NotContains(t, raw, "RecCPURequestMC")
	assert.NotContains(t, raw, "org-hidden")

	var env struct {
		Version                  int            `json:"version"`
		Recommendations          []containerOut `json:"recommendations"`
		NamespaceRecommendations []struct {
			Namespace             string `json:"namespace"`
			Term                  string `json:"term"`
			Engine                string `json:"engine"`
			RecCPURequestMC       int64  `json:"rec_cpu_request_mc"`
			RecCPULimitMC         int64  `json:"rec_cpu_limit_mc"`
			RecMemRequestKiB      int64  `json:"rec_mem_request_kib"`
			RecMemLimitKiB        int64  `json:"rec_mem_limit_kib"`
			CurrentCPURequestMC   int64  `json:"current_cpu_request_mc"`
			CurrentMemRequestKiB  int64  `json:"current_mem_request_kib"`
			EstimatedSavingsCents *int64 `json:"estimated_savings_cents"`
			Stale                 bool   `json:"stale"`
			Category              string `json:"category"`
		} `json:"namespace_recommendations"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	assert.Equal(t, 2, env.Version)
	assert.Empty(t, env.Recommendations)
	require.Len(t, env.NamespaceRecommendations, 1)
	row := env.NamespaceRecommendations[0]
	assert.Equal(t, "kube-system", row.Namespace)
	assert.Equal(t, "short", row.Term)
	assert.Equal(t, "cost", row.Engine)
	assert.Equal(t, int64(50), row.RecCPURequestMC)
	assert.Nil(t, row.EstimatedSavingsCents)
	assert.Equal(t, "underused", row.Category)
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"namespace_recommendations":[{`)
	assert.NotContains(t, compact, `"namespace_recommendations":null`)
}

func TestWriteRecs_JSONNamespaceEmptyArray(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		ClusterID: "c",
		Now:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		plugins:   []string{"namespace"},
	}, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":2`)
	assert.Contains(t, compact, `"namespace_recommendations":[]`)
	assert.NotContains(t, compact, `"namespace_recommendations":null`)
}

func TestWriteRecs_BusinessHoursJSONVersion10(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		Recs:            []types.ContainerRec{sampleRec(nil)},
		BHRecs:          []types.ContainerRec{sampleRec(nil)},
		BHNamespaceRecs: []namespace.NamespaceRec{},
		ClusterID:       "c",
		Now:             time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		plugins:         []string{"container"},
		businessHours:   true,
	}, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":10`)
	assert.Contains(t, compact, `"business_hours_recommendations":[`)
	assert.Contains(t, compact, `"business_hours_namespace_recommendations":[]`)
	assert.NotContains(t, compact, `"business_hours_recommendations":null`)
	assert.NotContains(t, compact, `"business_hours_namespace_recommendations":null`)
}

func TestWriteRecs_BusinessHoursJSONVersion11(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		NodeRecs:      []node.Rec{{Node: "worker-1", Term: "short", Engine: "cost"}},
		BHNodeRecs:    []node.Rec{},
		ClusterID:     "c",
		Now:           time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		plugins:       []string{"node"},
		businessHours: true,
	}, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":11`)
	assert.Contains(t, compact, `"business_hours_node_recommendations":[]`)
	assert.NotContains(t, compact, `"business_hours_node_recommendations":null`)
	assert.NotContains(t, compact, `"business_hours_gpu_recommendations"`)
	assert.NotContains(t, compact, `"notification_codes"`)
}

func TestWriteRecs_BusinessHoursNotificationCodesOnlyOnBHRows(t *testing.T) {
	engineNode := []int16{types.NotifNodeUnderutilized, types.NotifNodeIdle}
	engineGPU := []int16{types.NotifGPUIdle}
	engineTS := []int16{types.NotifGPUTimeSharingCandidate}
	result := recommendResult{
		NodeRecs: []node.Rec{{
			Node: "worker-1", Term: "short", Engine: "cost",
			NotificationCodes: engineNode,
		}},
		BHNodeRecs: []node.Rec{{
			Node: "worker-1", Term: "short", Engine: "cost",
			NotificationCodes: engineNode,
		}},
		GPURecs: []gpuRecRow{{
			Namespace: "app", Workload: "api", ContainerName: "api",
			Rec: gpu.GPURec{Term: "short", GPUModelName: "A100", NotificationCodes: engineGPU},
		}},
		BHGPURecs: []gpuRecRow{{
			Namespace: "app", Workload: "api", ContainerName: "api",
			Rec: gpu.GPURec{Term: "short", GPUModelName: "A100", NotificationCodes: engineGPU},
		}},
		GPUTimeslicing: []gpu.TimeslicingRec{{
			NodeName: "gpu-1", GPUModel: "A100", Term: "short", RecommendedReplicas: 4,
			NotificationCodes: engineTS,
		}},
		BHGPUTimeslicing: []gpu.TimeslicingRec{{
			NodeName: "gpu-1", GPUModel: "A100", Term: "short", RecommendedReplicas: 4,
			NotificationCodes: engineTS,
		}},
		VMRecs: []vm.VMRecommendation{{
			Namespace: "production", VMName: "web-vm", Term: "short", Engine: "cost",
		}},
		BHVMRecs: []vm.VMRecommendation{{
			Namespace: "production", VMName: "web-vm", Term: "short", Engine: "cost",
		}},
		ClusterID:     "c",
		Now:           time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		plugins:       []string{"node", "gpu", "vm"},
		businessHours: true,
	}

	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	var env recommendJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	assert.Equal(t, recommendJSONVersionWithBusinessHoursPlugins, env.Version)

	require.NotNil(t, env.NodeRecommendations)
	require.Len(t, *env.NodeRecommendations, 1)
	assert.Empty(t, (*env.NodeRecommendations)[0].NotificationCodes)
	require.NotNil(t, env.BusinessHoursNodeRecommendations)
	require.Len(t, *env.BusinessHoursNodeRecommendations, 1)
	assert.Equal(t, []int16{types.NotifNodeBHNotPeakSafe}, (*env.BusinessHoursNodeRecommendations)[0].NotificationCodes)

	require.NotNil(t, env.GPURecommendations)
	require.Len(t, *env.GPURecommendations, 1)
	assert.Empty(t, (*env.GPURecommendations)[0].NotificationCodes)
	require.NotNil(t, env.BusinessHoursGPURecommendations)
	require.Len(t, *env.BusinessHoursGPURecommendations, 1)
	assert.Equal(t, []int16{types.NotifGPUBHOfficeWindow}, (*env.BusinessHoursGPURecommendations)[0].NotificationCodes)

	require.NotNil(t, env.GPUTimeslicingRecommendations)
	require.Len(t, *env.GPUTimeslicingRecommendations, 1)
	assert.Empty(t, (*env.GPUTimeslicingRecommendations)[0].NotificationCodes)
	require.NotNil(t, env.BusinessHoursGPUTimeslicingRecommendations)
	require.Len(t, *env.BusinessHoursGPUTimeslicingRecommendations, 1)
	assert.Equal(t, []int16{types.NotifGPUTSBHClusterWindow}, (*env.BusinessHoursGPUTimeslicingRecommendations)[0].NotificationCodes)

	require.NotNil(t, env.VMRecommendations)
	require.Len(t, *env.VMRecommendations, 1)
	assert.Empty(t, (*env.VMRecommendations)[0].NotificationCodes)
	require.NotNil(t, env.BusinessHoursVMRecommendations)
	require.Len(t, *env.BusinessHoursVMRecommendations, 1)
	assert.Equal(t, []int16{types.NotifVMBHOfficeWindow}, (*env.BusinessHoursVMRecommendations)[0].NotificationCodes)

	assert.Equal(t, engineNode, result.BHNodeRecs[0].NotificationCodes)
	assert.Equal(t, engineGPU, result.BHGPURecs[0].Rec.NotificationCodes)
	assert.Equal(t, engineTS, result.BHGPUTimeslicing[0].NotificationCodes)

	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"notification_codes":[79]`)
	assert.Contains(t, compact, `"notification_codes":[80]`)
	assert.Contains(t, compact, `"notification_codes":[81]`)
	assert.Contains(t, compact, `"notification_codes":[82]`)
	assert.NotContains(t, compact, `"notification_codes":[11`)
	assert.NotContains(t, compact, `"notification_codes":[15`)
	assert.NotContains(t, compact, `"notification_codes":[26`)
	assert.NotContains(t, compact, `"notification_codes":[36`)
}

func TestWriteRecs_OmitsBusinessHoursKeysWhenOff(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		Recs:      []types.ContainerRec{sampleRec(nil)},
		ClusterID: "c",
		Now:       time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		plugins:   []string{"container"},
	}, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":1`)
	assert.NotContains(t, compact, `"business_hours_recommendations"`)
	assert.NotContains(t, compact, `"business_hours_namespace_recommendations"`)
}

func TestWriteRecs_BusinessHoursCSVError(t *testing.T) {
	err := writeRecs(bytes.NewBuffer(nil), recommendResult{
		Recs:          []types.ContainerRec{sampleRec(nil)},
		plugins:       []string{"container"},
		businessHours: true,
	}, "csv")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "json")
	assert.Contains(t, err.Error(), "schedule")
}

func TestWriteRecs_CSVMixedPluginsError(t *testing.T) {
	err := writeRecs(bytes.NewBuffer(nil), recommendResult{
		Recs:          []types.ContainerRec{sampleRec(nil)},
		NamespaceRecs: []namespace.NamespaceRec{sampleNSRec()},
		plugins:       []string{"container", "namespace"},
	}, "csv")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "json")
}

func TestWriteRecs_CSVMixedNodeContainerError(t *testing.T) {
	err := writeRecs(bytes.NewBuffer(nil), recommendResult{
		Recs:     []types.ContainerRec{sampleRec(nil)},
		NodeRecs: []node.Rec{{Node: "worker-1", Term: "short", Engine: "cost"}},
		plugins:  []string{"container", "node"},
	}, "csv")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "json")
}

func TestWriteRecs_JSONNodeSibling(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		NodeRecs:  []node.Rec{{Node: "worker-1", Term: "short", Engine: "cost", Category: "underutilized", RecommendedCPUMC: 1000}},
		ClusterID: "cluster-a",
		Now:       time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC),
		plugins:   []string{"node"},
	}, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":3`)
	assert.Contains(t, compact, `"node_recommendations":[{`)
	assert.NotContains(t, compact, `"node_recommendations":null`)
	assert.NotContains(t, compact, `"gpu_recommendations"`)
	assert.Contains(t, compact, `"estimated_savings_cents":null`)
	assert.NotContains(t, compact, "Expl")
	assert.NotContains(t, compact, `"notification_codes"`)
}

func TestWriteRecs_JSONGPUSiblingEmptyArrays(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		ClusterID: "c",
		Now:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		plugins:   []string{"gpu"},
	}, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":4`)
	assert.Contains(t, compact, `"gpu_recommendations":[]`)
	assert.Contains(t, compact, `"gpu_timeslicing_recommendations":[]`)
	assert.NotContains(t, compact, `"gpu_recommendations":null`)
	assert.NotContains(t, compact, `"node_recommendations"`)
}

func TestWriteRecs_JSONDefaultOmitsNodeGPUKeys(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		Recs:      []types.ContainerRec{sampleRec(nil)},
		ClusterID: "c",
		Now:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}, "json"))
	raw := buf.String()
	assert.NotContains(t, raw, "node_recommendations")
	assert.NotContains(t, raw, "gpu_recommendations")
	assert.NotContains(t, raw, "gpu_timeslicing_recommendations")
	assert.NotContains(t, raw, "pvc_recommendations")
	assert.NotContains(t, raw, "quota_recommendations")
}

func TestWriteRecs_UnknownFormat(t *testing.T) {
	err := writeRecs(bytes.NewBuffer(nil), recommendResult{}, "xml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown --format")
}

func TestContainerOutCSVHeadersMatchJSONTags(t *testing.T) {
	rt := reflect.TypeOf(containerOut{})
	var tags []string
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		require.NotEmpty(t, name)
		tags = append(tags, name)
	}
	assert.Equal(t, containerOutCSVHeader, tags)
}

func TestNamespaceOutCSVHeadersMatchJSONTags(t *testing.T) {
	rt := reflect.TypeOf(namespaceOut{})
	var tags []string
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		require.NotEmpty(t, name)
		tags = append(tags, name)
	}
	assert.Equal(t, namespaceOutCSVHeader, tags)
}

func TestContainerRecHasNoJSONTags(t *testing.T) {
	rt := reflect.TypeOf(types.ContainerRec{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		assert.Empty(t, f.Tag.Get("json"), "do not tag ContainerRec.%s; CLI owns JSON via containerOut", f.Name)
	}
}

func TestNamespaceRecHasNoJSONTags(t *testing.T) {
	rt := reflect.TypeOf(namespace.NamespaceRec{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		assert.Empty(t, f.Tag.Get("json"), "do not tag NamespaceRec.%s; CLI owns JSON via namespaceOut", f.Name)
	}
}

func TestNodeRecHasNoJSONTags(t *testing.T) {
	rt := reflect.TypeOf(node.Rec{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		assert.Empty(t, f.Tag.Get("json"), "do not tag node.Rec.%s; CLI owns JSON via nodeOut", f.Name)
	}
}

func TestGPURecHasNoJSONTags(t *testing.T) {
	rt := reflect.TypeOf(gpu.GPURec{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		assert.Empty(t, f.Tag.Get("json"), "do not tag GPURec.%s; CLI owns JSON via gpuOut", f.Name)
	}
}

func jsonTagNamesSkipping(t *testing.T, rt reflect.Type, skip map[string]struct{}) []string {
	t.Helper()
	var tags []string
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		require.NotEmpty(t, name)
		if _, ok := skip[name]; ok {
			continue
		}
		tags = append(tags, name)
	}
	return tags
}

func TestNodeOutCSVHeadersMatchJSONTags(t *testing.T) {
	// notification_codes is JSON-only on BH sibling rows; csv/table error when BH is on.
	assert.Equal(t, nodeOutCSVHeader, jsonTagNamesSkipping(t, reflect.TypeOf(nodeOut{}), map[string]struct{}{"notification_codes": {}}))
}

func TestGPUOutCSVHeadersMatchJSONTags(t *testing.T) {
	assert.Equal(t, gpuOutCSVHeader, jsonTagNamesSkipping(t, reflect.TypeOf(gpuOut{}), map[string]struct{}{"notification_codes": {}}))
}

func TestWriteRecs_JSONPVCSibling(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		PVCRecs: []pvc.PVCRec{{
			Namespace: "production", PVC: "data-pvc", Term: "short",
			RecommendationType: pvc.PVCRecTypeOversized, CapacityBytes: 10 << 30,
		}},
		ClusterID: "cluster-a",
		Now:       time.Date(2026, 5, 3, 2, 0, 0, 0, time.UTC),
		plugins:   []string{"pvc"},
	}, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":5`)
	assert.Contains(t, compact, `"pvc_recommendations":[{`)
	assert.NotContains(t, compact, `"pvc_recommendations":null`)
	assert.Contains(t, compact, `"estimated_savings_cents":null`)
	assert.NotContains(t, compact, `"node_recommendations"`)
}

func TestWriteRecs_JSONPVCSiblingEmptyArray(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		ClusterID: "c",
		Now:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		plugins:   []string{"pvc"},
	}, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":5`)
	assert.Contains(t, compact, `"pvc_recommendations":[]`)
	assert.NotContains(t, compact, `"pvc_recommendations":null`)
}

func TestPVCOutCSVHeadersMatchJSONTags(t *testing.T) {
	rt := reflect.TypeOf(pvcOut{})
	var tags []string
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		require.NotEmpty(t, name)
		tags = append(tags, name)
	}
	assert.Equal(t, pvcOutCSVHeader, tags)
}

func TestPVCRecHasNoJSONTags(t *testing.T) {
	rt := reflect.TypeOf(pvc.PVCRec{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		assert.Empty(t, f.Tag.Get("json"), "do not tag PVCRec.%s; CLI owns JSON via pvcOut", f.Name)
	}
}

func TestWriteRecs_JSONVMSibling(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		ClusterID: "c",
		Now:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		plugins:   []string{"vm"},
		VMRecs: []vm.VMRecommendation{{
			Namespace:            "production",
			VMName:               "web-vm",
			Term:                 "short_term",
			Engine:               "cost",
			Category:             "optimized",
			CurrentVCPU:          2,
			CurrentMemoryGiB:     4,
			RecommendedVCPU:      2,
			RecommendedMemoryGiB: 4,
			GuestOS:              "linux",
		}},
	}, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":6`)
	assert.Contains(t, compact, `"vm_recommendations":[{`)
	assert.NotContains(t, compact, `"vm_recommendations":null`)
	assert.Contains(t, compact, `"estimated_savings_cents":null`)
	assert.NotContains(t, compact, `"pvc_recommendations"`)
}

func TestWriteRecs_JSONVMSiblingEmptyArray(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		ClusterID: "c",
		Now:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		plugins:   []string{"vm"},
	}, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":6`)
	assert.Contains(t, compact, `"vm_recommendations":[]`)
	assert.NotContains(t, compact, `"vm_recommendations":null`)
}

func TestVMOutCSVHeadersMatchJSONTags(t *testing.T) {
	assert.Equal(t, vmOutCSVHeader, jsonTagNamesSkipping(t, reflect.TypeOf(vmOut{}), map[string]struct{}{"notification_codes": {}}))
}

func TestWriteRecs_JSONQuotaSibling(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		ClusterID: "c",
		Now:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		plugins:   []string{"quota"},
		QuotaRecs: []quota.QuotaRec{{
			OrgID:              "org-hidden",
			ClusterUUID:        "should-not-appear-on-row",
			Namespace:          "app",
			QuotaName:          "compute-resources",
			RecommendationType: quota.QuotaRecTypeOptimal,
			RiskLevel:          quota.QuotaRiskLow,
			Snapshot: quota.NamespaceQuotaSnapshot{
				CPURequestHardMC: 2000,
			},
			Recommended: quota.QuotaResourceBundle{
				CPURequestMillicores: 1100,
			},
			Expl: types.QuotaExplanationFactors{RecommendationReason: "hidden"},
		}},
	}, "json"))
	raw := buf.String()
	assert.NotContains(t, raw, "org-hidden")
	assert.NotContains(t, raw, "should-not-appear-on-row")
	assert.NotContains(t, raw, "RecommendationReason")
	assert.NotContains(t, raw, `"snapshot"`)
	compact := strings.ReplaceAll(strings.ReplaceAll(raw, " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":7`)
	assert.Contains(t, compact, `"quota_recommendations":[{`)
	assert.NotContains(t, compact, `"quota_recommendations":null`)
	assert.Contains(t, compact, `"estimated_savings_cents":null`)
	assert.NotContains(t, compact, `"vm_recommendations"`)
}

func TestWriteRecs_JSONQuotaSiblingEmptyArray(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		ClusterID: "c",
		Now:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		plugins:   []string{"quota"},
	}, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":7`)
	assert.Contains(t, compact, `"quota_recommendations":[]`)
	assert.NotContains(t, compact, `"quota_recommendations":null`)
}

func TestQuotaOutCSVHeadersMatchJSONTags(t *testing.T) {
	rt := reflect.TypeOf(quotaOut{})
	var tags []string
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		require.NotEmpty(t, name)
		tags = append(tags, name)
	}
	assert.Equal(t, quotaOutCSVHeader, tags)
}

func TestQuotaRecHasNoJSONTags(t *testing.T) {
	rt := reflect.TypeOf(quota.QuotaRec{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		assert.Empty(t, f.Tag.Get("json"), "do not tag QuotaRec.%s; CLI owns JSON via quotaOut", f.Name)
	}
}

func TestWriteRecs_CSVMixedQuotaContainerError(t *testing.T) {
	err := writeRecs(bytes.NewBuffer(nil), recommendResult{
		Recs:      []types.ContainerRec{sampleRec(nil)},
		QuotaRecs: []quota.QuotaRec{{Namespace: "app", QuotaName: "compute"}},
		plugins:   []string{"container", "quota"},
	}, "csv")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "json")
}

func TestWriteRecs_JSONClusterQuotaSibling(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		ClusterID: "c",
		Now:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		plugins:   []string{"cluster_quota"},
		ClusterQuotaRecs: []quota.ClusterQuotaRec{{
			OrgID:              "org-hidden",
			ClusterUUID:        "should-not-appear-on-row",
			ClusterQuotaName:   "team-a",
			Namespaces:         "app, prod",
			RecommendationType: quota.QuotaRecTypeOptimal,
			RiskLevel:          quota.QuotaRiskLow,
			Snapshot: quota.ClusterQuotaSnapshot{
				CPURequestHardMC:       10000,
				MemoryRequestHardBytes: 1073741824,
			},
			Recommended: quota.QuotaResourceBundle{
				CPURequestMillicores: 3300,
				MemoryRequestBytes:   590558003,
			},
			StorageRecommendedBytes: 0,
			PodsRecommended:         0,
			Expl:                    types.ClusterQuotaExplanationFactors{RecommendationReason: "hidden"},
		}},
	}, "json"))
	raw := buf.String()
	assert.NotContains(t, raw, "org-hidden")
	assert.NotContains(t, raw, "should-not-appear-on-row")
	assert.NotContains(t, raw, "RecommendationReason")
	assert.NotContains(t, raw, `"snapshot"`)
	assert.NotContains(t, raw, `"quota_used"`)
	assert.NotContains(t, raw, "cpu_request_used")
	compact := strings.ReplaceAll(strings.ReplaceAll(raw, " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":8`)
	assert.Contains(t, compact, `"cluster_quota_recommendations":[{`)
	assert.NotContains(t, compact, `"cluster_quota_recommendations":null`)
	assert.Contains(t, compact, `"estimated_savings_cents":null`)
	assert.Contains(t, compact, `"cluster_quota_name":"team-a"`)
	assert.Contains(t, compact, `"memory_request_hard_bytes":1073741824`)
	assert.NotContains(t, compact, `"quota_recommendations"`)
}

func TestWriteRecs_JSONClusterQuotaSiblingEmptyArray(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		ClusterID: "c",
		Now:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		plugins:   []string{"cluster_quota"},
	}, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":8`)
	assert.Contains(t, compact, `"cluster_quota_recommendations":[]`)
	assert.NotContains(t, compact, `"cluster_quota_recommendations":null`)
}

func TestClusterQuotaOutCSVHeadersMatchJSONTags(t *testing.T) {
	rt := reflect.TypeOf(clusterQuotaOut{})
	var tags []string
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		require.NotEmpty(t, name)
		tags = append(tags, name)
	}
	assert.Equal(t, clusterQuotaOutCSVHeader, tags)
}

func TestClusterQuotaRecHasNoJSONTags(t *testing.T) {
	rt := reflect.TypeOf(quota.ClusterQuotaRec{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		assert.Empty(t, f.Tag.Get("json"), "do not tag ClusterQuotaRec.%s; CLI owns JSON via clusterQuotaOut", f.Name)
	}
}

func TestWriteRecs_CSVMixedClusterQuotaContainerError(t *testing.T) {
	err := writeRecs(bytes.NewBuffer(nil), recommendResult{
		Recs:             []types.ContainerRec{sampleRec(nil)},
		ClusterQuotaRecs: []quota.ClusterQuotaRec{{ClusterQuotaName: "team-a"}},
		plugins:          []string{"container", "cluster_quota"},
	}, "csv")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "json")
}

func TestWriteRecs_JSONSnapshotSibling(t *testing.T) {
	cents := int64(5)
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		ClusterID: "c",
		Now:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		plugins:   []string{"snapshot"},
		SnapshotRecs: []snapshot.SnapshotRec{{
			OrgID:              "org-hidden",
			ClusterUUID:        "should-not-appear-on-row",
			Namespace:          "app",
			SnapshotName:       "snap-a",
			RecommendationType: "never_restored",
			AgeDays:            31,
			EstimatedCostCents: &cents,
			NotificationCodes:  nil,
			Expl:               types.SnapshotExplanationFactors{ClassificationRule: "hidden"},
		}},
	}, "json"))
	raw := buf.String()
	assert.NotContains(t, raw, "org-hidden")
	assert.NotContains(t, raw, "should-not-appear-on-row")
	assert.NotContains(t, raw, "ClassificationRule")
	assert.NotContains(t, raw, "hidden")
	compact := strings.ReplaceAll(strings.ReplaceAll(raw, " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":9`)
	assert.Contains(t, compact, `"snapshot_recommendations":[{`)
	assert.NotContains(t, compact, `"snapshot_recommendations":null`)
	assert.Contains(t, compact, `"notification_codes":[]`)
	assert.Contains(t, compact, `"snapshot_name":"snap-a"`)
	assert.NotContains(t, compact, `"cluster_quota_recommendations"`)
}

func TestWriteRecs_JSONSnapshotSiblingEmptyArray(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, recommendResult{
		ClusterID: "c",
		Now:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		plugins:   []string{"snapshot"},
	}, "json"))
	compact := strings.ReplaceAll(strings.ReplaceAll(buf.String(), " ", ""), "\n", "")
	assert.Contains(t, compact, `"version":9`)
	assert.Contains(t, compact, `"snapshot_recommendations":[]`)
	assert.NotContains(t, compact, `"snapshot_recommendations":null`)
}

func TestSnapshotOutCSVHeadersMatchJSONTags(t *testing.T) {
	rt := reflect.TypeOf(snapshotOut{})
	var tags []string
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		require.NotEmpty(t, name)
		tags = append(tags, name)
	}
	assert.Equal(t, snapshotOutCSVHeader, tags)
}

func TestSnapshotRecHasNoJSONTags(t *testing.T) {
	rt := reflect.TypeOf(snapshot.SnapshotRec{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		assert.Empty(t, f.Tag.Get("json"), "do not tag SnapshotRec.%s; CLI owns JSON via snapshotOut", f.Name)
	}
}

func TestWriteRecs_CSVMixedSnapshotContainerError(t *testing.T) {
	err := writeRecs(bytes.NewBuffer(nil), recommendResult{
		Recs:         []types.ContainerRec{sampleRec(nil)},
		SnapshotRecs: []snapshot.SnapshotRec{{SnapshotName: "snap-a"}},
		plugins:      []string{"container", "snapshot"},
	}, "csv")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "json")
}
