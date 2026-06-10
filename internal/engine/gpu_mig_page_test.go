package engine

import "testing"

func TestGPUMIGOrderColumnSupportsPagination(t *testing.T) {
	t.Parallel()
	for _, col := range []string{"cluster_uuid", "namespace", "workload", "container", "gpu_model"} {
		if !GPUMIGOrderColumnSupportsPagination(col) {
			t.Errorf("expected %q to support SQL pagination", col)
		}
	}
	for _, col := range []string{"term", "confidence", "invalid"} {
		if GPUMIGOrderColumnSupportsPagination(col) {
			t.Errorf("expected %q to not support SQL pagination", col)
		}
	}
}
