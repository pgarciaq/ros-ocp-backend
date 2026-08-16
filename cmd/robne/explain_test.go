package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToContainerExplainOut_OmitsPascalCaseExpl(t *testing.T) {
	rec := sampleRec(nil)
	rec.Expl.DataDays = 1
	rec.Expl.OOMBumpApplied = true
	out := toContainerExplainOut(rec, scheduleAllHours)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "CPUCostPctMC")
	assert.NotContains(t, string(raw), "OOMBumpApplied")
	assert.Contains(t, string(raw), `"data_days"`)
	assert.Contains(t, string(raw), `"oom_bump_applied"`)
	assert.Equal(t, 1, out.DataDays)
	assert.True(t, out.OOMBumpApplied)
	assert.Equal(t, scheduleAllHours, out.Schedule)
}

func TestRunExplain_OneDayFixture(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	var buf bytes.Buffer
	err := runExplain(&buf, explainFlags{
		commonFlags: commonFlags{
			input:        csvPath,
			noUserConfig: true,
			now:          "2026-08-01T02:00:00Z",
		},
		namespace: "app",
		workload:  "api",
		container: "api",
		term:      "short",
		engine:    "cost",
	})
	require.NoError(t, err)
	var out containerExplainOut
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, "app", out.Namespace)
	assert.Equal(t, "api", out.Workload)
	assert.Equal(t, "deployment", out.WorkloadType)
	assert.Equal(t, "api", out.ContainerName)
	assert.Equal(t, "short", out.Term)
	assert.Equal(t, "cost", out.Engine)
	assert.Equal(t, scheduleAllHours, out.Schedule)
	assert.Equal(t, int64(58), out.RecCPURequestMC)
	assert.Equal(t, int64(58880), out.RecMemRequestKiB)
	assert.Equal(t, 1, out.DataDays)
	assert.False(t, out.OOMBumpApplied)
	assert.Greater(t, out.CPUCostPctMC, int64(0))
	assert.Greater(t, out.MemCostPctKiB, int64(0))
	assert.NotContains(t, buf.String(), "CPUCostPctMC")
}

func TestRunExplain_NoMatch(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	err := runExplain(&bytes.Buffer{}, explainFlags{
		commonFlags: commonFlags{input: csvPath, noUserConfig: true, now: "2026-08-01T02:00:00Z"},
		namespace:   "missing",
		workload:    "api",
		container:   "api",
		term:        "short",
		engine:      "cost",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no matching container")
}

func TestRunExplain_AmbiguousWorkloadType(t *testing.T) {
	matches := []types.ContainerRec{
		{Namespace: "app", Workload: "api", WorkloadType: "deployment", ContainerName: "api", Term: "short", Engine: "cost"},
		{Namespace: "app", Workload: "api", WorkloadType: "daemonset", ContainerName: "api", Term: "short", Engine: "cost"},
	}
	_, err := selectContainerRec(matches, explainFlags{
		namespace: "app", workload: "api", container: "api", term: "short", engine: "cost",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--workload-type")

	got, err := selectContainerRec(matches, explainFlags{
		namespace: "app", workload: "api", workloadType: "daemonset", container: "api", term: "short", engine: "cost",
	})
	require.NoError(t, err)
	assert.Equal(t, "daemonset", got.WorkloadType)
}

func TestRunExplain_RejectsRecommendEnvelope(t *testing.T) {
	dir := t.TempDir()
	path := writeJSONFile(t, dir, "env.json", baseEnvelope())
	err := runExplain(&bytes.Buffer{}, explainFlags{
		commonFlags: commonFlags{input: path, noUserConfig: true},
		namespace:   "app",
		workload:    "api",
		container:   "api",
		term:        "short",
		engine:      "cost",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not read recommend JSON envelopes")
}

func TestRunExplain_ExplicitOtherPluginError(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	err := runExplain(&bytes.Buffer{}, explainFlags{
		commonFlags: commonFlags{
			input:        csvPath,
			plugins:      "namespace",
			noUserConfig: true,
			now:          "2026-08-01T02:00:00Z",
		},
		namespace: "app",
		workload:  "api",
		container: "api",
		term:      "short",
		engine:    "cost",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "container-only")
	assert.Contains(t, err.Error(), "#490")
}

func TestRunExplain_BusinessHoursScheduleRequiresYAML(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	err := runExplain(&bytes.Buffer{}, explainFlags{
		commonFlags: commonFlags{input: csvPath, noUserConfig: true, now: "2026-08-01T02:00:00Z"},
		namespace:   "app",
		workload:    "api",
		container:   "api",
		term:        "short",
		engine:      "cost",
		schedule:    scheduleBusinessHours,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "business_hours.enabled")
}

func TestRunExplain_BusinessHoursWeekendNoMatch(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(oneDayCSV("app", "api", "cluster-a")), 0o600))
	writeBusinessHoursYAML(t, cwd)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	err := runExplain(&bytes.Buffer{}, explainFlags{
		commonFlags: commonFlags{input: csvPath, noUserConfig: true, now: "2026-08-01T02:00:00Z"},
		namespace:   "app",
		workload:    "api",
		container:   "api",
		term:        "short",
		engine:      "cost",
		schedule:    scheduleBusinessHours,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no matching container")
}

func TestRunExplain_BusinessHoursWeekday(t *testing.T) {
	cwd := t.TempDir()
	csvPath := filepath.Join(cwd, "ocp_ros_usage.csv")
	monday := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	require.NoError(t, os.WriteFile(csvPath, []byte(weekdayCSV("app", "api", "cluster-a", monday)), 0o600))
	writeBusinessHoursYAML(t, cwd)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	t.Chdir(cwd)

	var buf bytes.Buffer
	err := runExplain(&buf, explainFlags{
		commonFlags: commonFlags{input: csvPath, noUserConfig: true, now: "2026-08-04T00:00:00Z"},
		namespace:   "app",
		workload:    "api",
		container:   "api",
		term:        "short",
		engine:      "cost",
		schedule:    scheduleBusinessHours,
	})
	require.NoError(t, err)
	var out containerExplainOut
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, scheduleBusinessHours, out.Schedule)
	assert.Equal(t, 1, out.DataDays)
}
