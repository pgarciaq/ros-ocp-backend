package container

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
)

func TestReplicaCountForSavings_PrefersDesiredOverPodCount(t *testing.T) {
	tests := []struct {
		name            string
		desiredReplicas int64
		podCountAvg     int64
		want            float64
	}{
		{"desired > 0, uses desired", 5, 3, 5.0},
		{"desired == 0, falls back to pod count", 0, 3, 3.0},
		{"both zero", 0, 0, 0.0},
		{"desired == 1, pod count == 10", 1, 10, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &core.ContainerRec{
				DesiredReplicas: tt.desiredReplicas,
				PodCountAvg:     tt.podCountAvg,
			}
			got := replicaCountForSavings(rec)
			assert.Equal(t, tt.want, got)
		})
	}
}
