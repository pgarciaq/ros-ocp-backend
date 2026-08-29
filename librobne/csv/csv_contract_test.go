package csv

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// OperatorRosContainerCSVHeader is the exact header produced by
// koku-metrics-operator's rosContainerRow.csvHeader() method.
//
// Source of truth: koku-metrics-operator/internal/collector/types.go
// (rosContainerRow).csvHeader()
//
// If the operator changes its CSV header, this test will need updating —
// and that's the point: it makes the cross-repo contract explicit.
var OperatorRosContainerCSVHeader = []string{
	"report_period_start",
	"report_period_end",
	"interval_start",
	"interval_end",
	"container_name",
	"pod",
	"owner_name",
	"owner_kind",
	"workload",
	"workload_type",
	"namespace",
	"image_name",
	"node",
	"resource_id",
	"node_capacity_cpu_cores",
	"node_capacity_memory_bytes",
	"node_capacity_pods",
	"node_allocatable_cpu_cores",
	"node_allocatable_memory_bytes",
	"instance_type",
	"machineset_name",
	"cpu_request_container_avg",
	"cpu_request_container_sum",
	"cpu_limit_container_avg",
	"cpu_limit_container_sum",
	"cpu_usage_container_avg",
	"cpu_usage_container_min",
	"cpu_usage_container_max",
	"cpu_usage_container_sum",
	"cpu_throttle_container_avg",
	"cpu_throttle_container_max",
	"cpu_throttle_container_min",
	"cpu_throttle_container_sum",
	"memory_request_container_avg",
	"memory_request_container_sum",
	"memory_limit_container_avg",
	"memory_limit_container_sum",
	"memory_usage_container_avg",
	"memory_usage_container_min",
	"memory_usage_container_max",
	"memory_usage_container_sum",
	"memory_rss_usage_container_avg",
	"memory_rss_usage_container_min",
	"memory_rss_usage_container_max",
	"memory_rss_usage_container_sum",
	"oom_count",
	"workload_pod_count",
	"desired_replicas",
	"available_replicas",
	"accelerator_model_name",
	"accelerator_profile_name",
	"accelerator_frame_buffer_usage_min",
	"accelerator_frame_buffer_usage_max",
	"accelerator_frame_buffer_usage_avg",
	"tensor_pipe_active_min",
	"tensor_pipe_active_max",
	"tensor_pipe_active_avg",
	"dram_active_min",
	"dram_active_max",
	"dram_active_avg",
	"sm_active_min",
	"sm_active_max",
	"sm_active_avg",
}

// TestCSVContract_OperatorHeaderParseable verifies that the exact header
// produced by koku-metrics-operator can be parsed by buildColumnIndex without
// error and that all required columns are found at the expected positions.
//
// This is a cross-repository contract test. If it fails, either:
// 1. The operator changed its CSV header (update OperatorRosContainerCSVHeader), or
// 2. The ROS parser changed its required columns (coordinate with operator team).
func TestCSVContract_OperatorHeaderParseable(t *testing.T) {
	t.Parallel()

	idx, err := buildColumnIndex(OperatorRosContainerCSVHeader)
	require.NoError(t, err, "operator CSV header must be parseable by ROS buildColumnIndex")

	assert.GreaterOrEqual(t, idx.intervalStart, 0, "interval_start not found")
	assert.GreaterOrEqual(t, idx.intervalEnd, 0, "interval_end not found")
	assert.GreaterOrEqual(t, idx.namespace, 0, "namespace not found")
	assert.GreaterOrEqual(t, idx.workloadName, 0, "workload not found")
	assert.GreaterOrEqual(t, idx.workloadType, 0, "workload_type not found")
	assert.GreaterOrEqual(t, idx.containerName, 0, "container_name not found")
	assert.GreaterOrEqual(t, idx.pod, 0, "pod not found")
	assert.GreaterOrEqual(t, idx.cpuRequest, 0, "cpu_request_container_avg not found")
	assert.GreaterOrEqual(t, idx.cpuUsage, 0, "cpu_usage_container_avg not found")
	assert.GreaterOrEqual(t, idx.memRequest, 0, "memory_request_container_avg not found")
	assert.GreaterOrEqual(t, idx.memUsage, 0, "memory_usage_container_avg not found")

	assert.GreaterOrEqual(t, idx.node, 0, "node not found")
	assert.GreaterOrEqual(t, idx.nodeCapacityCPU, 0, "node_capacity_cpu_cores not found")
	assert.GreaterOrEqual(t, idx.nodeCapacityMem, 0, "node_capacity_memory_bytes not found")
	assert.GreaterOrEqual(t, idx.nodeAllocCPU, 0, "node_allocatable_cpu_cores not found")
	assert.GreaterOrEqual(t, idx.nodeAllocMem, 0, "node_allocatable_memory_bytes not found")
	assert.GreaterOrEqual(t, idx.instanceType, 0, "instance_type not found")
	assert.GreaterOrEqual(t, idx.cpuLimit, 0, "cpu_limit_container_avg not found")
	assert.GreaterOrEqual(t, idx.cpuThrottle, 0, "cpu_throttle_container_avg not found")
	assert.GreaterOrEqual(t, idx.memLimit, 0, "memory_limit_container_avg not found")
	assert.GreaterOrEqual(t, idx.memRSS, 0, "memory_rss_usage_container_avg not found")
	assert.GreaterOrEqual(t, idx.oomCount, 0, "oom_count not found")
	assert.GreaterOrEqual(t, idx.workloadPodCount, 0, "workload_pod_count not found")
	assert.GreaterOrEqual(t, idx.desiredReplicas, 0, "desired_replicas not found")
	assert.GreaterOrEqual(t, idx.availableReplicas, 0, "available_replicas not found")

	assert.GreaterOrEqual(t, idx.acceleratorModelName, 0, "accelerator_model_name not found")
	assert.GreaterOrEqual(t, idx.acceleratorProfileName, 0, "accelerator_profile_name not found")
	assert.GreaterOrEqual(t, idx.fbMin, 0, "accelerator_frame_buffer_usage_min not found")
	assert.GreaterOrEqual(t, idx.fbMax, 0, "accelerator_frame_buffer_usage_max not found")
	assert.GreaterOrEqual(t, idx.fbAvg, 0, "accelerator_frame_buffer_usage_avg not found")
	assert.GreaterOrEqual(t, idx.tensorMin, 0, "tensor_pipe_active_min not found")
	assert.GreaterOrEqual(t, idx.tensorMax, 0, "tensor_pipe_active_max not found")
	assert.GreaterOrEqual(t, idx.tensorAvg, 0, "tensor_pipe_active_avg not found")
	assert.GreaterOrEqual(t, idx.dramMin, 0, "dram_active_min not found")
	assert.GreaterOrEqual(t, idx.dramMax, 0, "dram_active_max not found")
	assert.GreaterOrEqual(t, idx.dramAvg, 0, "dram_active_avg not found")
	assert.GreaterOrEqual(t, idx.smMin, 0, "sm_active_min not found")
	assert.GreaterOrEqual(t, idx.smMax, 0, "sm_active_max not found")
	assert.GreaterOrEqual(t, idx.smAvg, 0, "sm_active_avg not found")
}

// TestCSVContract_OperatorRowParseable verifies a full synthetic row
// matching the operator's header can be parsed end-to-end by ParseRows.
func TestCSVContract_OperatorRowParseable(t *testing.T) {
	t.Parallel()

	values := make([]string, len(OperatorRosContainerCSVHeader))
	for i, col := range OperatorRosContainerCSVHeader {
		switch col {
		case "report_period_start", "interval_start":
			values[i] = "2026-05-01 00:00:00 +0000 UTC"
		case "report_period_end", "interval_end":
			values[i] = "2026-05-01 01:00:00 +0000 UTC"
		case "container_name":
			values[i] = "my-container"
		case "pod":
			values[i] = "my-pod-abc123"
		case "owner_name", "owner_kind", "image_name", "resource_id":
			values[i] = "test-value"
		case "workload":
			values[i] = "my-deployment"
		case "workload_type":
			values[i] = "Deployment"
		case "namespace":
			values[i] = "my-namespace"
		case "node":
			values[i] = "worker-0"
		case "instance_type":
			values[i] = "m5.xlarge"
		case "node_capacity_cpu_cores":
			values[i] = "8"
		case "node_capacity_memory_bytes":
			values[i] = "33554432000"
		case "node_allocatable_cpu_cores":
			values[i] = "7.5"
		case "node_allocatable_memory_bytes":
			values[i] = "32212254720"
		case "accelerator_model_name":
			values[i] = "NVIDIA A100"
		case "accelerator_profile_name":
			values[i] = ""
		default:
			values[i] = "0.5"
		}
	}

	body := strings.Join(OperatorRosContainerCSVHeader, ",") + "\n" + strings.Join(values, ",") + "\n"
	rows, skipped, err := ParseRows(strings.NewReader(body))
	require.NoError(t, err, "operator-format CSV row must be parseable")
	require.Zero(t, skipped)
	require.Len(t, rows, 1, "expected exactly 1 parsed row")

	row := rows[0]
	assert.Equal(t, "my-namespace", row.Namespace)
	assert.Equal(t, "my-deployment", row.WorkloadName)
	assert.Equal(t, "deployment", row.WorkloadType)
	assert.Equal(t, "my-container", row.ContainerName)
	assert.Equal(t, "my-pod-abc123", row.Pod)
	assert.Equal(t, "worker-0", row.Node)
	assert.Equal(t, "m5.xlarge", row.InstanceType)
	assert.Equal(t, int64(8000), row.NodeCapacityCPUMC)
	assert.Equal(t, int64(7500), row.NodeAllocatableCPUMC)
	assert.Equal(t, "NVIDIA A100", row.GPUModel)
	assert.Equal(t, int64(500), row.CPURequestMC, "0.5 cores = 500 millicores")
	assert.Equal(t, int64(500), row.CPUUsageMC)
}
