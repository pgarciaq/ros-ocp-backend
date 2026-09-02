package ingestion

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFlushGPUStreamGroupsOnSender_RequiresOrgID(t *testing.T) {
	t.Parallel()
	groups := map[gpuStreamKey]*gpuStreamAgg{
		{date: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), namespace: "ns", workload: "wl", container: "c"}: {
			workloadType: "deployment",
			modelName:    "A100",
			gpuUUIDs:     map[string]struct{}{"gpu-1": {}},
		},
	}
	err := flushGPUStreamGroupsOnSender(context.Background(), nil, groups, "", "aaaaaaaa-1111-2222-3333-444444444444", ScheduleTypeAllHours)
	if err == nil || !strings.Contains(err.Error(), "org_id") {
		t.Fatalf("got %v, want org_id error", err)
	}
}

func TestFlushGPUStreamGroupsOnSender_EmptyNoOp(t *testing.T) {
	t.Parallel()
	if err := flushGPUStreamGroupsOnSender(context.Background(), nil, nil, "", "", ScheduleTypeAllHours); err != nil {
		t.Fatalf("empty groups: %v", err)
	}
}
