package ingestion

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoreToMillicores(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"exact integer core", "1.0", 1000, false},
		{"fractional core", "0.250", 250, false},
		{"large core count", "128.5", 128500, false},
		{"zero", "0.0", 0, false},
		{"sub-millicore rounds", "0.0001", 0, false},
		{"half millicore rounds", "0.0005", 1, false},
		{"2.5 cores", "2.5", 2500, false},
		{"small fraction", "0.001", 1, false},
		{"empty string", "", 0, true},
		{"non-numeric", "abc", 0, true},
		{"NaN", "NaN", 0, true},
		{"Inf", "Inf", 0, true},
		{"-Inf", "-Inf", 0, true},
		{"negative", "-0.5", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CoreToMillicores(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBytesToKiB(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"exact KiB boundary", "1024.0", 1, false},
		{"1 MiB", "1048576.0", 1024, false},
		{"1 GiB", "1073741824.0", 1048576, false},
		{"zero", "0.0", 0, false},
		{"512 bytes rounds to 1 KiB", "512.0", 1, false},
		{"511 bytes rounds to 0 KiB", "511.0", 0, false},
		{"sub-KiB rounds up", "768.0", 1, false},
		{"empty string", "", 0, true},
		{"NaN", "NaN", 0, true},
		{"Inf", "Inf", 0, true},
		{"negative", "-1024.0", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BytesToKiB(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateMetricRow(t *testing.T) {
	validRow := MetricRow{
		CPURequestMC:  100,
		CPUUsageMC:    50,
		MemRequestKiB: 1024,
		MemUsageKiB:   512,
	}

	t.Run("valid row passes", func(t *testing.T) {
		err := ValidateMetricRow(validRow)
		assert.NoError(t, err)
	})

	t.Run("zero values are valid", func(t *testing.T) {
		err := ValidateMetricRow(MetricRow{})
		assert.NoError(t, err)
	})

	t.Run("negative CPU request", func(t *testing.T) {
		row := validRow
		row.CPURequestMC = -1
		err := ValidateMetricRow(row)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CPURequestMC")
	})

	t.Run("negative memory usage", func(t *testing.T) {
		row := validRow
		row.MemUsageKiB = -100
		err := ValidateMetricRow(row)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "MemUsageKiB")
	})

	t.Run("negative OOM count", func(t *testing.T) {
		row := validRow
		row.OOMCount = -1
		err := ValidateMetricRow(row)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "OOMCount")
	})
}

func TestParseCSVRows(t *testing.T) {
	t.Run("valid CSV with header", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,pod-1,test-deploy,deployment,main,0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0",
			"2026-03-01 00:15:00 +0000 UTC,2026-03-01 00:30:00 +0000 UTC,test-ns,pod-2,test-deploy,deployment,main,0.5,1.0,0.30,0.02,1048576.0,2097152.0,600000.0,300000.0,0",
		}, "\n")

		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		assert.Len(t, rows, 2)
		assert.Equal(t, "test-ns", rows[0].Namespace)
		assert.Equal(t, int64(500), rows[0].CPURequestMC)
		assert.Equal(t, int64(250), rows[0].CPUUsageMC)
		assert.Equal(t, int64(1024), rows[0].MemRequestKiB)
	})

	t.Run("empty CSV returns empty slice", func(t *testing.T) {
		csv := "interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count\n"
		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		assert.Empty(t, rows)
	})

	t.Run("NaN values cause row skip", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,pod-nan,test-deploy,deployment,main,NaN,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0",
		}, "\n")

		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		assert.Empty(t, rows)
	})

	t.Run("large values preserved as int64", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,pod-big,test-deploy,deployment,main,128.0,256.0,64.0,0.0,137438953472.0,274877906944.0,68719476736.0,34359738368.0,0",
		}, "\n")

		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, int64(128000), rows[0].CPURequestMC)
		assert.Equal(t, int64(134217728), rows[0].MemRequestKiB)
	})
}

func TestParseCSVRows_WorkloadPodCount(t *testing.T) {
	t.Run("with workload_pod_count and pod columns", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count,workload_pod_count",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,pod-abc-123,test-deploy,deployment,main,0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0,3.0",
		}, "\n")

		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, "pod-abc-123", rows[0].Pod)
		assert.Equal(t, int64(3), rows[0].WorkloadPodCount)
	})

	t.Run("without workload_pod_count (old operator)", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,pod-xyz-456,test-deploy,deployment,main,0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0",
		}, "\n")

		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, "pod-xyz-456", rows[0].Pod)
		assert.Equal(t, int64(0), rows[0].WorkloadPodCount)
	})

	t.Run("workload_pod_count rounds float to int", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count,workload_pod_count",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,pod-a,test-deploy,deployment,main,0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0,2.7",
		}, "\n")

		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, int64(3), rows[0].WorkloadPodCount)
	})
}

func TestParseCSVRows_ReplicaColumns(t *testing.T) {
	t.Run("with desired_replicas and available_replicas", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name," +
				"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
				"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg," +
				"oom_count,workload_pod_count,desired_replicas,available_replicas",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,prod,pod-a,web,deployment,app," +
				"0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0,3.0,5.0,4.0",
		}, "\n")

		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, int64(3), rows[0].WorkloadPodCount)
		assert.Equal(t, int64(5), rows[0].DesiredReplicas)
		assert.Equal(t, int64(4), rows[0].AvailableReplicas)
	})

	t.Run("without replica columns (old operator)", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name," +
				"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
				"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg," +
				"oom_count,workload_pod_count",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,prod,pod-a,web,deployment,app," +
				"0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0,3.0",
		}, "\n")

		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, int64(3), rows[0].WorkloadPodCount)
		assert.Equal(t, int64(0), rows[0].DesiredReplicas)
		assert.Equal(t, int64(0), rows[0].AvailableReplicas)
	})

	t.Run("replica columns with float values are rounded", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name," +
				"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
				"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg," +
				"oom_count,workload_pod_count,desired_replicas,available_replicas",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,prod,pod-a,web,deployment,app," +
				"0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0,3.0,3.7,2.4",
		}, "\n")

		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, int64(4), rows[0].DesiredReplicas)
		assert.Equal(t, int64(2), rows[0].AvailableReplicas)
	})

	t.Run("empty replica columns treated as zero", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name," +
				"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
				"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg," +
				"oom_count,workload_pod_count,desired_replicas,available_replicas",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,prod,pod-a,web,deployment,app," +
				"0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0,3.0,,",
		}, "\n")

		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, int64(3), rows[0].WorkloadPodCount)
		assert.Equal(t, int64(0), rows[0].DesiredReplicas)
		assert.Equal(t, int64(0), rows[0].AvailableReplicas)
	})
}

// TestRoundHalfToEven verifies our rounding matches math.Round behavior.
func TestMetricRow_HasGPU(t *testing.T) {
	assert.False(t, (&MetricRow{}).HasGPU())
	row := MetricRow{GPUModel: "NVIDIA A100"}
	assert.True(t, row.HasGPU())
}

func TestParseCSVRows_GPUMetrics(t *testing.T) {
	t.Run("parses GPU columns when present", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name," +
				"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
				"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count," +
				"accelerator_core_usage_percentage_min,accelerator_model_name,accelerator_profile_name," +
				"accelerator_frame_buffer_usage_min,accelerator_frame_buffer_usage_max,accelerator_frame_buffer_usage_avg," +
				"tensor_pipe_active_min,tensor_pipe_active_max,tensor_pipe_active_avg," +
				"dram_active_min,dram_active_max,dram_active_avg," +
				"sm_active_min,sm_active_max,sm_active_avg",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,pod-gpu,train,deployment,app," +
				"0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0," +
				",NVIDIA-A100-SXM4-80GB,3g.40gb," +
				"1024.5,2048.0,1536.25," +
				"0.1,0.2,0.15," +
				"0.3,0.4,0.35," +
				"0.5,0.6,0.55",
		}, "\n")

		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.True(t, rows[0].HasGPU())
		assert.Equal(t, "NVIDIA-A100-SXM4-80GB", rows[0].GPUModel)
		assert.Equal(t, "3g.40gb", rows[0].GPUProfile)
		assert.InDelta(t, 1024.5, rows[0].FBUsageMinMiB, 1e-9)
		assert.InDelta(t, 0.55, rows[0].SMActiveAvg, 1e-9)
	})

	t.Run("missing GPU columns leave zero defaults", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name," +
				"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
				"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,pod-only,svc,deployment,svc,0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0",
		}, "\n")

		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.False(t, rows[0].HasGPU())
		assert.Empty(t, rows[0].GPUProfile)
		assert.Zero(t, rows[0].TensorPipeAvg)
	})

	t.Run("subset of GPU columns with blanks", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name," +
				"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
				"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count," +
				"accelerator_model_name,accelerator_profile_name,accelerator_frame_buffer_usage_min," +
				"tensor_pipe_active_avg,dram_active_min,sm_active_max",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,gpu-ns,pod-half,wl,deployment,c1," +
				"0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0," +
				"Tesla-T4,,,0.42,,",
		}, "\n")

		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.True(t, rows[0].HasGPU())
		assert.Empty(t, rows[0].GPUProfile)
		assert.InDelta(t, 0.42, rows[0].TensorPipeAvg, 1e-9)
		assert.Zero(t, rows[0].FBUsageMinMiB)
	})
}

func TestParseCSVRows_NodeColumn(t *testing.T) {
	t.Run("node column present", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name,node," +
				"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
				"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,pod-1,test-deploy,deployment,main,gpu-worker-1," +
				"0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0",
		}, "\n")
		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, "gpu-worker-1", rows[0].Node)
	})

	t.Run("node column missing", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name," +
				"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
				"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,pod-1,test-deploy,deployment,main," +
				"0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0",
		}, "\n")
		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, "", rows[0].Node)
	})
}

func TestParseCSVRows_InstanceTypeColumn(t *testing.T) {
	t.Run("instance_type column present", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name,node,instance_type," +
				"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
				"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,pod-1,test-deploy,deployment,main,worker-1,m5.xlarge," +
				"0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0",
		}, "\n")
		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, "m5.xlarge", rows[0].InstanceType)
	})

	t.Run("instance_type column missing (old operator)", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,pod,workload,workload_type,container_name,node," +
				"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
				"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,pod-1,test-deploy,deployment,main,worker-1," +
				"0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0",
		}, "\n")
		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, "", rows[0].InstanceType)
	})
}

func TestRoundHalfToEven(t *testing.T) {
	assert.Equal(t, int64(1), int64(math.Round(0.5)))
	assert.Equal(t, int64(2), int64(math.Round(1.5)))
	assert.Equal(t, int64(2), int64(math.Round(2.4999)))
	assert.Equal(t, int64(3), int64(math.Round(2.5)))
}

// --- Fuzz tests: ensure ParseCSVRows never panics on arbitrary input ---

func FuzzCoreToMillicores(f *testing.F) {
	f.Add("1.0")
	f.Add("0.250")
	f.Add("")
	f.Add("NaN")
	f.Add("Inf")
	f.Add("-1.5")
	f.Add("abc")
	f.Add("99999999999999999999999.9")
	f.Fuzz(func(t *testing.T, s string) {
		CoreToMillicores(s) //nolint:errcheck
	})
}

func FuzzBytesToKiB(f *testing.F) {
	f.Add("1048576.0")
	f.Add("0.0")
	f.Add("")
	f.Add("NaN")
	f.Add("-500")
	f.Add("Inf")
	f.Fuzz(func(t *testing.T, s string) {
		BytesToKiB(s) //nolint:errcheck
	})
}

func FuzzParseCSVRows(f *testing.F) {
	validCSV := "interval_start,interval_end,namespace,pod,workload,workload_type,container_name," +
		"cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg," +
		"memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count\n" +
		"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,pod-1,test-deploy,deployment,main," +
		"0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0\n"
	f.Add(validCSV)
	f.Add("")
	f.Add("just,a,header\n")
	f.Add("no_newline_header")
	f.Add(strings.Repeat("a,", 100) + "b\n" + strings.Repeat("x,", 100) + "y\n")

	f.Fuzz(func(t *testing.T, data string) {
		ParseCSVRows(strings.NewReader(data)) //nolint:errcheck
	})
}
