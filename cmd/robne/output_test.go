package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/namespace"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
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

func TestWriteRecs_CSVMixedPluginsError(t *testing.T) {
	err := writeRecs(bytes.NewBuffer(nil), recommendResult{
		Recs:          []types.ContainerRec{sampleRec(nil)},
		NamespaceRecs: []namespace.NamespaceRec{sampleNSRec()},
		plugins:       []string{"container", "namespace"},
	}, "csv")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "json")
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
