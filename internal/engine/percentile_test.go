package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSelectCPUUsagePercentile(t *testing.T) {
	row := DigestRow{
		CPUUsageP50MC: 100,
		CPUUsageP60MC: 120,
		CPUUsageP95MC: 190,
		CPUUsageP98MC: 196,
		CPUUsageP99MC: 198,
		CPUUsageMaxMC: 200,
	}

	tests := []struct {
		name string
		pct  float64
		want int64
	}{
		{"p50", 0.50, 100},
		{"p60", 0.60, 120},
		{"p95", 0.95, 190},
		{"p98", 0.98, 196},
		{"p99", 0.99, 198},
		{"p100 (max)", 1.0, 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectCPUUsagePercentile(row, tt.pct)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSelectMemUsagePercentile(t *testing.T) {
	row := DigestRow{
		MemUsageP50KiB: 1024,
		MemUsageP95KiB: 4096,
		MemUsageMaxKiB: 8192,
	}

	tests := []struct {
		name string
		pct  float64
		want int64
	}{
		{"p50", 0.50, 1024},
		{"p95", 0.95, 4096},
		{"p100 (max)", 1.0, 8192},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectMemUsagePercentile(row, tt.pct)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSelectCPUUsagePercentile_CustomP50(t *testing.T) {
	row := DigestRow{CPUUsageP50MC: 500}
	got := SelectCPUUsagePercentile(row, 0.50)
	assert.Equal(t, int64(500), got)
}
