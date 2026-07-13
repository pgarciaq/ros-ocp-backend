package gpu

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGpuMIGListSortColumn(t *testing.T) {
	tests := []struct {
		orderBy string
		want    string
	}{
		{"cluster_uuid", "m.cluster_uuid::text"},
		{"namespace", "m.namespace"},
		{"workload", "m.workload"},
		{"container", "m.container_name"},
		{"gpu_model", "m.gpu_model_name"},
		{"term", "m.term"},
		{"confidence", "m.confidence"},
		{"gpu_idle_state", "m.gpu_idle_state"},
		{"unknown", "m.cluster_uuid::text"},
	}
	for _, tt := range tests {
		t.Run(tt.orderBy, func(t *testing.T) {
			got := gpuMIGListSortColumn(tt.orderBy)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAppendGPUMIGFilters(t *testing.T) {
	t.Run("no filters", func(t *testing.T) {
		q, args, idx := appendGPUMIGFilters("SELECT 1", []any{"org1"}, 2, GPUMIGListFilters{})
		assert.Equal(t, "SELECT 1", q)
		assert.Len(t, args, 1)
		assert.Equal(t, 2, idx)
	})

	t.Run("all filters", func(t *testing.T) {
		f := GPUMIGListFilters{
			ClusterUUIDs:  []string{"c1", "c2"},
			Namespaces:    []string{"ns1"},
			Workloads:     []string{"wl1"},
			Term:          "short",
			GPUIdleStates: []string{"idle", "zombie"},
		}
		q, args, idx := appendGPUMIGFilters("SELECT 1", []any{"org1"}, 2, f)
		assert.Contains(t, q, "cluster_uuid")
		assert.Contains(t, q, "namespace")
		assert.Contains(t, q, "workload")
		assert.Contains(t, q, "term")
		assert.Contains(t, q, "gpu_idle_state")
		assert.Len(t, args, 6)
		assert.Equal(t, 7, idx)
	})
}
