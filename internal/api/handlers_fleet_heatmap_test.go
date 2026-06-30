package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUtilizationBand(t *testing.T) {
	tests := []struct {
		name      string
		utilP95   float32
		idleState string
		want      string
	}{
		{"idle state overrides", 0.50, "idle", "idle"},
		{"zombie state overrides", 0.80, "zombie", "idle"},
		{"very low utilization", 0.05, "active", "idle"},
		{"boundary at 0.10", 0.10, "active", "low"},
		{"low utilization", 0.20, "active", "low"},
		{"boundary at 0.30", 0.30, "active", "moderate"},
		{"moderate utilization", 0.50, "active", "moderate"},
		{"boundary at 0.65", 0.65, "active", "healthy"},
		{"healthy utilization", 0.75, "active", "healthy"},
		{"boundary at 0.85", 0.85, "active", "hot"},
		{"hot utilization", 0.95, "active", "hot"},
		{"zero utilization active", 0.0, "active", "idle"},
		{"full utilization", 1.0, "active", "hot"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UtilizationBand(tt.utilP95, tt.idleState)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDataWindowLabel(t *testing.T) {
	assert.Contains(t, dataWindowLabel("short"), "1 day")
	assert.Contains(t, dataWindowLabel("medium"), "7 days")
	assert.Contains(t, dataWindowLabel("long"), "15 days")
	assert.Contains(t, dataWindowLabel(""), "7 days")
}
