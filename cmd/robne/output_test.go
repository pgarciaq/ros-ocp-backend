package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestContainerRecHasNoJSONTags(t *testing.T) {
	rt := reflect.TypeOf(types.ContainerRec{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		assert.Empty(t, f.Tag.Get("json"), "do not tag ContainerRec.%s; CLI owns JSON via containerOut", f.Name)
	}
}
