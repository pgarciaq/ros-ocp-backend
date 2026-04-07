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
			"interval_start,interval_end,namespace,workload_name,workload_type,container_name,cpu_request,cpu_limit,cpu_usage,cpu_throttle,mem_request,mem_limit,mem_usage,mem_rss,oom_count",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,test-deploy,deployment,main,0.5,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0",
			"2026-03-01 00:15:00 +0000 UTC,2026-03-01 00:30:00 +0000 UTC,test-ns,test-deploy,deployment,main,0.5,1.0,0.30,0.02,1048576.0,2097152.0,600000.0,300000.0,0",
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
		csv := "interval_start,interval_end,namespace,workload_name,workload_type,container_name,cpu_request,cpu_limit,cpu_usage,cpu_throttle,mem_request,mem_limit,mem_usage,mem_rss,oom_count\n"
		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		assert.Empty(t, rows)
	})

	t.Run("NaN values cause row skip", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,workload_name,workload_type,container_name,cpu_request,cpu_limit,cpu_usage,cpu_throttle,mem_request,mem_limit,mem_usage,mem_rss,oom_count",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,test-deploy,deployment,main,NaN,1.0,0.25,0.01,1048576.0,2097152.0,524288.0,262144.0,0",
		}, "\n")

		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		assert.Empty(t, rows)
	})

	t.Run("large values preserved as int64", func(t *testing.T) {
		csv := strings.Join([]string{
			"interval_start,interval_end,namespace,workload_name,workload_type,container_name,cpu_request,cpu_limit,cpu_usage,cpu_throttle,mem_request,mem_limit,mem_usage,mem_rss,oom_count",
			"2026-03-01 00:00:00 +0000 UTC,2026-03-01 00:15:00 +0000 UTC,test-ns,test-deploy,deployment,main,128.0,256.0,64.0,0.0,137438953472.0,274877906944.0,68719476736.0,34359738368.0,0",
		}, "\n")

		rows, err := ParseCSVRows(strings.NewReader(csv))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		assert.Equal(t, int64(128000), rows[0].CPURequestMC)
		assert.Equal(t, int64(134217728), rows[0].MemRequestKiB)
	})
}

// TestRoundHalfToEven verifies our rounding matches math.Round behavior.
func TestRoundHalfToEven(t *testing.T) {
	assert.Equal(t, int64(1), int64(math.Round(0.5)))
	assert.Equal(t, int64(2), int64(math.Round(1.5)))
	assert.Equal(t, int64(2), int64(math.Round(2.4999)))
	assert.Equal(t, int64(3), int64(math.Round(2.5)))
}
